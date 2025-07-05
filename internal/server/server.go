package server

import (
	"fmt"
	"log"
	"net/http"
	"html/template"
	"strings"
	"time"

	"nntp-web/internal/config"
	"nntp-web/internal/database"
	"nntp-web/internal/nntpclient"
	"nntp-web/internal/models"
	"nntp-web/internal/ratelimit" // For BotProtectionMiddleware
	"nntp-web/web/templates"
	"os"
	"errors"

	"github.com/gatherstars-com/jwz"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp" // For OTel HTTP server instrumentation
	"go.opentelemetry.io/otel"                                      // For global OTel providers
	"github.com/prometheus/client_golang/prometheus/promhttp"       // For Prometheus HTTP handler
)

var webLogger *log.Logger

func init() {
	webLogger = log.New(os.Stdout, "", 0)
}

type responseWriterInterceptor struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (w *responseWriterInterceptor) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriterInterceptor) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += n
	return n, err
}

func newResponseWriterInterceptor(w http.ResponseWriter) *responseWriterInterceptor {
	return &responseWriterInterceptor{ResponseWriter: w, statusCode: http.StatusOK}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		interceptor := newResponseWriterInterceptor(w)
		next.ServeHTTP(interceptor, r)
		duration := time.Since(startTime)
		remoteUser := "-"
		if r.URL.User != nil && r.URL.User.Username() != "" {
			remoteUser = r.URL.User.Username()
		}
		referer := r.Referer()
		if referer == "" {
			referer = "-"
		}
		userAgent := r.UserAgent()
		if userAgent == "" {
			userAgent = "-"
		}
		logTime := startTime.Format("02/Jan/2006:15:04:05 -0700")
		clientIP := r.RemoteAddr
		if colonPos := strings.LastIndex(clientIP, ":"); colonPos != -1 {
			clientIP = clientIP[:colonPos]
		}
		webLogger.Printf("%s - %s [%s] \"%s %s %s\" %d %d \"%s\" \"%s\" %dms",
			clientIP, remoteUser, logTime, r.Method, r.RequestURI, r.Proto,
			interceptor.statusCode, interceptor.bytesWritten, referer, userAgent, duration.Milliseconds(),
		)
	})
}

type jwzArticleAdapter struct {
	Article    *models.Article
	ParsedDate time.Time
	nextSibling  jwz.Threadable
	firstChild jwz.Threadable
	parent     jwz.Threadable
	isDummy    bool
	dummyID    string
}

func (a *jwzArticleAdapter) MessageThreadID() string { if a.isDummy { return a.dummyID }; if a.Article == nil { return "" }; return a.Article.MessageID }
func (a *jwzArticleAdapter) MessageThreadReferences() []string { if a.isDummy || a.Article == nil { return nil }; return a.Article.References }
func (a *jwzArticleAdapter) Subject() string { if a.isDummy || a.Article == nil { return "" }; return a.Article.Subject }
var subjectPrefixes = []string{"re:", "fw:", "fwd:", "aw:"}
func (a *jwzArticleAdapter) SimplifiedSubject() string {
	if a.isDummy || a.Article == nil || a.Article.Subject == "" { return "" }
	subj := strings.ToLower(a.Article.Subject)
	for { changed := false; for _, prefix := range subjectPrefixes { if strings.HasPrefix(subj, prefix) { subj = strings.TrimSpace(subj[len(prefix):]); changed = true }}; if !changed { break }}
	return strings.Join(strings.Fields(subj), " ")
}
func (a *jwzArticleAdapter) SubjectIsReply() bool {
	if a.isDummy || a.Article == nil || a.Article.Subject == "" { return false }
	subj := strings.ToLower(a.Article.Subject); for _, prefix := range subjectPrefixes { if strings.HasPrefix(subj, prefix) { return true }}; return false
}
func (a *jwzArticleAdapter) SetNext(next jwz.Threadable) { a.nextSibling = next }
func (a *jwzArticleAdapter) GetNext() jwz.Threadable { return a.nextSibling }
func (a *jwzArticleAdapter) SetChild(kid jwz.Threadable) { a.firstChild = kid }
func (a *jwzArticleAdapter) GetChild() jwz.Threadable { return a.firstChild }
func (a *jwzArticleAdapter) SetParent(p jwz.Threadable) { a.parent = p }
func (a *jwzArticleAdapter) GetParent() jwz.Threadable { return a.parent }
func (a *jwzArticleAdapter) GetDate() time.Time { return a.ParsedDate }
func (a *jwzArticleAdapter) MakeDummy(forID string) jwz.Threadable { return &jwzArticleAdapter{ isDummy: true, dummyID: forID } }
func (a *jwzArticleAdapter) IsDummy() bool { return a.isDummy }

type Server struct {
	config      *config.Config
	db          *database.DB
	nntpClient  nntpclient.NNTPClientInterface
	router      *http.ServeMux
	templates   *template.Template
	rateLimiter *ratelimit.RateLimiter // For bot protection
}

func NewServer(
	cfg *config.Config,
	db *database.DB,
	nntpCli nntpclient.NNTPClientInterface,
	rl *ratelimit.RateLimiter, // Added for bot protection
) (*Server, error) {
	t := template.New("base").Funcs(template.FuncMap{
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 { return nil, errors.New("dict expects an even number of arguments for key-value pairs") }
			m := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok { return nil, errors.New("dict keys must be strings") }
				m[key] = values[i+1]
			}
			return m, nil
		},
	})
	parsedTemplates, err := t.ParseFS(templates.Files, "*.html.tmpl")
	if err != nil { return nil, fmt.Errorf("failed to parse templates: %w", err) }

	srv := &Server{
		config:      cfg,
		db:          db,
		nntpClient:  nntpCli,
		router:      http.NewServeMux(),
		templates:   parsedTemplates,
		rateLimiter: rl,
	}
	srv.setupRoutes()
	return srv, nil
}

func (s *Server) setupRoutes() {
	s.router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" { http.Redirect(w, r, "/group/", http.StatusFound)
		} else { http.NotFound(w, r) }
	})
	s.router.HandleFunc("/group/", s.routeGroupRequests)
	s.router.HandleFunc("/robots.txt", s.handleRobotsTXT())

	if s.config.OTelMetricsEnabled { // Check if OTel metrics are enabled in config
		s.router.Handle("/metrics", promhttp.Handler())
		log.Println("Metrics endpoint /metrics enabled")
	}
}

func (s *Server) routeGroupRequests(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	trimmedPath := strings.TrimPrefix(path, "/group/")
	parts := strings.Split(trimmedPath, "/")
	if path == "/group/" { s.handleListGroups()(w, r); return }
	if r.URL.Query().Get("msgid") != "" { log.Printf("routeGroupRequests: Routing to handleShowArticle for 'msgid' query parameter. Path: %s", r.URL.RequestURI()); s.handleShowArticle()(w, r); return }
	if len(parts) >= 2 && strings.HasPrefix(parts[len(parts)-1], ";.msgid=") { log.Printf("routeGroupRequests: Routing to handleShowArticle for path-based ';.msgid='. Path: %s", path); s.handleShowArticle()(w, r); return }
	if len(parts) == 4 && parts[0] != "" && strings.HasPrefix(parts[3], "msg") && strings.HasSuffix(parts[3], ".html") { log.Printf("routeGroupRequests: Routing to handleShowArticle for canonical article path: %s", path); s.handleShowArticle()(w, r); return }
	if len(parts) == 3 && parts[0] != "" && strings.HasSuffix(parts[2], ".html") { s.handleListMessages()(w, r); return }
	if len(parts) >= 2 && parts[0] != "" && parts[1] == "" { s.handleListMessages()(w, r); return }
	log.Printf("Unhandled /group/ path structure: %s with parts %v", path, parts); http.NotFound(w, r)
}

func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf(":%d", s.config.ServerPort)
	log.Printf("Server listening on http://localhost%s", addr)

	var handler http.Handler = s.router

	// Apply BotProtectionMiddleware. It checks s.config.BotProtectionEnabled internally.
	handler = s.BotProtectionMiddleware(handler)

	// Apply loggingMiddleware
	handler = loggingMiddleware(handler)

	// Apply OTel HTTP middleware if tracing is configured
	// if s.config.OTelExporterOTLPTracesEndpoint != "" {
	// 	handler = otelhttp.NewHandler(handler, "http.server",
	// 		otelhttp.WithMeterProvider(otel.GetGlobalMeterProvider()),
	// 		otelhttp.WithTracerProvider(otel.GetGlobalTracerProvider()),
	// 	)
	// 	log.Println("OTel HTTP server instrumentation enabled.")
	// } else {
	// 	log.Println("OTel HTTP server instrumentation disabled (no OTLP traces endpoint configured).")
	// }
	log.Println("OTel HTTP server auto-instrumentation temporarily disabled due to build issues.")

	return http.ListenAndServe(addr, handler)
}

func (s *Server) renderTemplate(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	err := s.templates.ExecuteTemplate(w, name, data)
	if err != nil {
		log.Printf("Error executing template %s: %v", name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

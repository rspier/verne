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
	"nntp-web/internal/nntpclient" // Package for concrete type and interface
	"nntp-web/internal/models"
	"github.com/gatherstars-com/jwz"
	"nntp-web/web/templates"
	"os"     // For os.Stdout for webLogger
	"errors" // Required for the new dict function
)

var webLogger *log.Logger // For Apache-style logs

func init() {
	// Initialize webLogger to write to STDOUT without any prefix or flags from the standard log package.
	// We want raw output for Apache-style logs.
	webLogger = log.New(os.Stdout, "", 0)
}

// responseWriterInterceptor is a wrapper around http.ResponseWriter to capture status code and bytes written.
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

// newResponseWriterInterceptor creates a new responseWriterInterceptor.
// It's important to initialize statusCode to http.StatusOK, as WriteHeader might not be called explicitly by all handlers
// (e.g. if Write is called directly, or if an error occurs before WriteHeader).
func newResponseWriterInterceptor(w http.ResponseWriter) *responseWriterInterceptor {
	return &responseWriterInterceptor{ResponseWriter: w, statusCode: http.StatusOK}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		interceptor := newResponseWriterInterceptor(w)

		next.ServeHTTP(interceptor, r) // Call the next handler in the chain

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

		// Apache log time format: 02/Jan/2006:15:04:05 -0700
		logTime := startTime.Format("02/Jan/2006:15:04:05 -0700")

		// Use RemoteAddr, but consider X-Forwarded-For if behind a proxy.
		// For simplicity, using RemoteAddr directly here.
		clientIP := r.RemoteAddr
		if colonPos := strings.LastIndex(clientIP, ":"); colonPos != -1 {
			clientIP = clientIP[:colonPos] // Strip port if present (common for RemoteAddr)
		}


		webLogger.Printf("%s - %s [%s] \"%s %s %s\" %d %d \"%s\" \"%s\" %dms",
			clientIP,
			remoteUser,
			logTime,
			r.Method,
			r.RequestURI,
			r.Proto,
			interceptor.statusCode,
			interceptor.bytesWritten,
			referer,
			userAgent,
			duration.Milliseconds(),
		)
	})
}


// jwzArticleAdapter adapts models.Article to jwz.Threadable interface
type jwzArticleAdapter struct {
	Article    *models.Article
	ParsedDate time.Time

	nextSibling  jwz.Threadable
	firstChild jwz.Threadable
	parent     jwz.Threadable
	isDummy    bool
	dummyID    string
}

func (a *jwzArticleAdapter) MessageThreadID() string {
	if a.isDummy {
		return a.dummyID
	}
	if a.Article == nil {
		return ""
	}
	return a.Article.MessageID
}

func (a *jwzArticleAdapter) MessageThreadReferences() []string {
	if a.isDummy || a.Article == nil {
		return nil
	}
	return a.Article.References
}

func (a *jwzArticleAdapter) Subject() string {
	if a.isDummy || a.Article == nil {
		return ""
	}
	return a.Article.Subject
}

var subjectPrefixes = []string{"re:", "fw:", "fwd:", "aw:"}

func (a *jwzArticleAdapter) SimplifiedSubject() string {
	if a.isDummy || a.Article == nil || a.Article.Subject == "" {
		return ""
	}
	subj := strings.ToLower(a.Article.Subject)
	for {
		changed := false
		for _, prefix := range subjectPrefixes {
			if strings.HasPrefix(subj, prefix) {
				subj = strings.TrimSpace(subj[len(prefix):])
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return strings.Join(strings.Fields(subj), " ")
}

func (a *jwzArticleAdapter) SubjectIsReply() bool {
	if a.isDummy || a.Article == nil || a.Article.Subject == "" {
		return false
	}
	subj := strings.ToLower(a.Article.Subject)
	for _, prefix := range subjectPrefixes {
		if strings.HasPrefix(subj, prefix) {
			return true
		}
	}
	return false
}

func (a *jwzArticleAdapter) SetNext(next jwz.Threadable) {
	a.nextSibling = next
}

func (a *jwzArticleAdapter) GetNext() jwz.Threadable {
	return a.nextSibling
}

func (a *jwzArticleAdapter) SetChild(kid jwz.Threadable) {
	a.firstChild = kid
}

func (a *jwzArticleAdapter) GetChild() jwz.Threadable {
	return a.firstChild
}

func (a *jwzArticleAdapter) SetParent(p jwz.Threadable) {
	a.parent = p
}

func (a *jwzArticleAdapter) GetParent() jwz.Threadable {
	return a.parent
}

func (a *jwzArticleAdapter) GetDate() time.Time {
	return a.ParsedDate
}

func (a *jwzArticleAdapter) MakeDummy(forID string) jwz.Threadable {
	return &jwzArticleAdapter{
		isDummy: true,
		dummyID: forID,
	}
}

func (a *jwzArticleAdapter) IsDummy() bool {
	return a.isDummy
}

// Server holds the dependencies for the HTTP server.
type Server struct {
	config     *config.Config
	db         *database.DB
	nntpClient nntpclient.NNTPClientInterface // Use the interface
	router     *http.ServeMux
	templates  *template.Template
}

// NewServer creates and configures a new server instance.
func NewServer(cfg *config.Config, db *database.DB, nntpCli nntpclient.NNTPClientInterface) (*Server, error) {
	t := template.New("base").Funcs(template.FuncMap{
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, errors.New("dict expects an even number of arguments for key-value pairs")
			}
			m := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, errors.New("dict keys must be strings")
				}
				m[key] = values[i+1]
			}
			return m, nil
		},
	})
	parsedTemplates, err := t.ParseFS(templates.Files, "*.html.tmpl")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	srv := &Server{
		config:     cfg,
		db:         db,
		nntpClient: nntpCli,
		router:     http.NewServeMux(),
		templates:  parsedTemplates,
	}

	srv.setupRoutes()
	return srv, nil
}

func (s *Server) setupRoutes() {
	s.router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// This handler is for the root path.
		// http.ServeMux matches longer patterns first, so if r.URL.Path is not "/",
		// it means no other more specific handler (like "/group/") matched it.
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/group/", http.StatusFound) // Or http.StatusMovedPermanently
		} else {
			// For any other path that falls through to this root handler, serve a 404.
			http.NotFound(w, r)
		}
	})
	s.router.HandleFunc("/group/", s.routeGroupRequests) // This will handle all /group/* requests
}

func (s *Server) routeGroupRequests(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	trimmedPath := strings.TrimPrefix(path, "/group/")
	parts := strings.Split(trimmedPath, "/")

	if path == "/group/" {
		s.handleListGroups()(w, r)
		return
	}

	// msgid lookup: Check for query parameter "msgid"
	if r.URL.Query().Get("msgid") != "" {
		log.Printf("routeGroupRequests: Routing to handleShowArticle for 'msgid' query parameter. Path: %s", r.URL.RequestURI())
		s.handleShowArticle()(w, r)
		return
	}
	// Path-based ;.msgid= (less preferred, but for compatibility if needed)
	if len(parts) >= 2 && strings.HasPrefix(parts[len(parts)-1], ";.msgid=") {
		log.Printf("routeGroupRequests: Routing to handleShowArticle for path-based ';.msgid='. Path: %s", path)
		s.handleShowArticle()(w, r)
		return
	}

	// Canonical article: /group/{groupname}/{year}/{month}/msg{id}.html
	if len(parts) == 4 && parts[0] != "" && strings.HasPrefix(parts[3], "msg") && strings.HasSuffix(parts[3], ".html") {
		log.Printf("routeGroupRequests: Routing to handleShowArticle for canonical article path: %s", path)
		s.handleShowArticle()(w, r)
		return
	}

	// Message list for specific month: /group/{groupname}/{year}/{month}.html
	if len(parts) == 3 && parts[0] != "" && strings.HasSuffix(parts[2], ".html") {
		s.handleListMessages()(w, r)
		return
	}

	// Message list for group (latest month): /group/{groupname}/
	if len(parts) >= 2 && parts[0] != "" && parts[1] == "" { // Must have trailing slash
		s.handleListMessages()(w, r)
		return
	}

	log.Printf("Unhandled /group/ path structure: %s with parts %v", path, parts)
	http.NotFound(w, r)
}

func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf(":%d", s.config.ServerPort)
	// Standard logger (STDERR) for this startup message
	log.Printf("Server listening on http://localhost%s", addr)

	// Wrap the main router with the logging middleware
	loggedRouter := loggingMiddleware(s.router)

	return http.ListenAndServe(addr, loggedRouter) // Use the wrapped router
}

func (s *Server) renderTemplate(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	err := s.templates.ExecuteTemplate(w, name, data)
	if err != nil {
		log.Printf("Error executing template %s: %v", name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

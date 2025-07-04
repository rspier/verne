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
)

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
	t := template.New("base")
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
	s.router.HandleFunc("/group/", s.routeGroupRequests)
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
	log.Printf("Server listening on http://localhost%s", addr)
	return http.ListenAndServe(addr, s.router)
}

func (s *Server) renderTemplate(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	err := s.templates.ExecuteTemplate(w, name, data)
	if err != nil {
		log.Printf("Error executing template %s: %v", name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

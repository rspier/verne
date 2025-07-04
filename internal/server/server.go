package server

import (
	"fmt"
	"log"
	"net/http"

	"nntp-web/internal/config"
	"nntp-web/internal/database"
	"html/template"                // Changed from safehtml/template
	"nntp-web/internal/nntpclient"
	"nntp-web/internal/models"     // Added for models.Article
	"nntp-web/internal/utils"      // Added for ObfuscateEmailInFromHeader
	"github.com/gatherstars-com/jwz" // Corrected JWZ threading import
	"nntp-web/web/templates"       // Import for embedded templates
	"strings"                      // Added
	"time"                         // Added for parsing dates
)

// jwzArticleAdapter adapts models.Article to jwz.Message interface
type jwzArticleAdapter struct {
	Article      *models.Article
	ParsedDate   time.Time
	ParsedParent string
	ParentMsg    jwz.Message // For Parent() method
}

func (a *jwzArticleAdapter) MessageId() string {
	return a.Article.MessageID
}

func (a *jwzArticleAdapter) References() []string {
	// models.Article.References is already []string, parsed from RawReferences
	return a.Article.References
}

func (a *jwzArticleAdapter) Date() time.Time {
	return a.ParsedDate
}

func (a *jwzArticleAdapter) Parent() jwz.Message {
	// This is tricky. JWZ expects to find the parent in the list of messages
	// provided to Thread(). We don't need to implement complex lookup here.
	// If ParentMsg is set during preprocessing, return it. Otherwise, nil.
	return a.ParentMsg
}


// Server holds the dependencies for the HTTP server.
type Server struct {
	config     *config.Config
	db         *database.DB
	nntpClient *nntpclient.Client
	router     *http.ServeMux
	templates  *template.Template // Now html/template.Template
}

// NewServer creates and configures a new server instance.
func NewServer(cfg *config.Config, db *database.DB, nntpCli *nntpclient.Client) (*Server, error) {
	// Parse templates using html/template
	t := template.New("base")
	// Add Funcs here if needed: t = t.Funcs(template.FuncMap{...})
	parsedTemplates, err := t.ParseFS(templates.Files, "*.html.tmpl")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	srv := &Server{
		config:     cfg,
		db:         db,
		nntpClient: nntpCli,
		router:     http.NewServeMux(),
		templates:  parsedTemplates, // This is now *html.template.Template
	}

	srv.setupRoutes()

	return srv, nil
}

func (s *Server) setupRoutes() {
	// Centralized routing for /group/ paths
	s.router.HandleFunc("/group/", s.routeGroupRequests)
	// Add other top-level routes here, e.g. s.router.HandleFunc("/static/", s.handleStaticFiles())
	// s.router.HandleFunc("/article/", s.routeArticleRequests) // Example for future
}

// routeGroupRequests is the main dispatcher for all paths starting with /group/
func (s *Server) routeGroupRequests(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	trimmedPath := strings.TrimPrefix(path, "/group/")
	parts := strings.Split(trimmedPath, "/")

	// Case 1: /group/ (list all groups)
	if path == "/group/" {
		s.handleListGroups()(w, r)
		return
	}

	// parts[0] will be groupname or empty if path was just "/group/" (already handled)
	// If path is /group/comp.lang.c/ or /group/comp.lang.c/;.msgid=...
	// parts would be ["comp.lang.c", ""] or ["comp.lang.c", ";.msgid=..."] (if query param is part of path by mistake)
	// We should rely on query parameters for ;.msgid, not path parsing.

	// Check for Message-ID lookup: /group/{groupname}/;.msgid={messageid}
	// The actual path for this is /group/{groupname}/ and ;.msgid is a query parameter.
	rawQuery := r.URL.RawQuery // Keep for logging if needed elsewhere
	log.Printf("routeGroupRequests: Path=[%s], RawQuery=[%s]", path, rawQuery)

	// Case 1: /group/ (list all groups) - Handled at the top by 'if path == "/group/"'

	// parts[0] is groupName, parts[1] could be year or ";.msgid=..." or empty (for trailing slash)

	// Case 2: /group/{groupname}/;.msgid={id} (article by message ID)
	// path: /group/foo/;.msgid=bar -> parts: ["foo", ";.msgid=bar"]
	if len(parts) == 2 && parts[0] != "" && strings.HasPrefix(parts[1], ";.msgid=") {
		log.Printf("routeGroupRequests: Matched ;.msgid path pattern, routing to handleShowArticle for path %s", path)
		s.handleShowArticle()(w, r)
		return
	}

	// Case 3: /group/{groupname}/{year}/{month}/msg{id}.html (specific article by details)
	// path: /group/foo/2024/01/msg123.html -> parts: ["foo", "2024", "01", "msg123.html"]
	if len(parts) == 4 && parts[0] != "" && strings.HasPrefix(parts[3], "msg") && strings.HasSuffix(parts[3], ".html") {
		log.Printf("routeGroupRequests: Matched canonical article path pattern, routing to handleShowArticle for path %s", path)
		s.handleShowArticle()(w, r)
		return
	}

	// Case 4: /group/{groupname}/{year}/{month}.html (specific month message list)
	// e.g., /group/foo/2024/01.html -> parts: ["foo", "2024", "01.html"]
	if len(parts) == 3 && strings.HasSuffix(parts[2], ".html") {
		s.handleListMessages()(w, r)
		return
	}

	// Case 4: /group/{groupname}/ (current month message list)
	// e.g., /group/foo/ -> parts: ["foo", ""]
	if len(parts) == 2 && parts[0] != "" && parts[1] == "" { // Must have trailing slash
		s.handleListMessages()(w, r)
		return
	}

	// If none of the above patterns match specifically
	log.Printf("Unhandled /group/ path structure: %s", path)
	http.NotFound(w, r)
}


// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf(":%d", s.config.ServerPort)
	log.Printf("Server listening on http://localhost%s", addr)
	return http.ListenAndServe(addr, s.router)
}

// Helper to render templates
func (s *Server) renderTemplate(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	err := s.templates.ExecuteTemplate(w, name, data)
	if err != nil {
		// Log the error and send a generic error response
		log.Printf("Error executing template %s: %v", name, err)
		// Consider having a dedicated error template
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

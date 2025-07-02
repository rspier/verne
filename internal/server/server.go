package server

import (
	"fmt
	"log"
	"net/http"

	"nntp-web/internal/config"
	"nntp-web/internal/database"
	"nntp-web/web/templates" // Import for embedded templates

	"github.com/google/safehtml/template"
)

// Server holds the dependencies for the HTTP server.
type Server struct {
	config     *config.Config
	db         *database.DB
	nntpClient *nntpclient.Client // Added NNTP client
	router     *http.ServeMux
	templates  *template.Template
}

// NewServer creates and configures a new server instance.
func NewServer(cfg *config.Config, db *database.DB, nntpCli *nntpclient.Client) (*Server, error) {
	// Parse templates
	// Using template.Must to panic if parsing fails, as it's a startup error.
	parsedTemplates, err := template.New("").
		Funcs(template.FuncMap{
			// Add any custom template functions here if needed in the future
		}).
		ParseFS(templates.Get(), "*.html.tmpl")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	srv := &Server{
		config:     cfg,
		db:         db,
		nntpClient: nntpCli, // Assign passed NNTP client
		router:     http.NewServeMux(),
		templates:  parsedTemplates,
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
	if queryMsgID := r.URL.Query().Get(";.msgid"); queryMsgID != "" {
		// Path should be /group/{groupname}/ for this to be valid.
		// Example: /group/perl.test/;.msgid=foo@bar
		// parts here for "/group/perl.test/" would be ["perl.test", ""]
		if len(parts) > 0 && parts[0] != "" { // parts[0] is groupname
			// If path is just /group/groupname/ (len(parts)==2, parts[1]=="")
			// or /group/groupname (len(parts)==1)
			// This is valid for ;.msgid lookup.
			s.handleShowArticle()(w, r)
			return
		}
	}

	// At this point, parts[0] is the groupName.
	// parts: [groupName, yearOrPossibleEmpty, monthOrMsg, possibleMsgID]

	// Case 2: /group/{groupname}/{year}/{month}/msg{id}.html (specific article)
	// e.g., /group/foo/2024/01/msg123.html -> parts: ["foo", "2024", "01", "msg123.html"]
	if len(parts) == 4 && strings.HasPrefix(parts[3], "msg") && strings.HasSuffix(parts[3], ".html") {
		s.handleShowArticle()(w, r)
		return
	}

	// Case 3: /group/{groupname}/{year}/{month}.html (specific month message list)
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

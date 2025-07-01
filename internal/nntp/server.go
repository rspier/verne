package nntp

import (
	"fmt"
	"log"
	"net"
	// "time" // Not directly used here anymore, but session uses it.

	"github.com/user/colobus/internal/config"
	"github.com/user/colobus/internal/database"
)

// Server holds the NNTP server state.
type Server struct {
	config         *config.Config
	db             *database.DB
	listener       net.Listener
	groupNameToNum map[string]int // Effective mapping of group name to numeric ID
	groupNumToName map[int]string   // Effective mapping of group numeric ID to name
}

// NewServer creates a new NNTP server instance.
func NewServer(
	cfg *config.Config,
	db *database.DB,
	groupNameToNum map[string]int,
	groupNumToName map[int]string,
) (*Server, error) {
	return &Server{
		config:         cfg,
		db:             db,
		groupNameToNum: groupNameToNum,
		groupNumToName: groupNumToName,
	}, nil
}

// ListenAndServe starts the NNTP server and listens for incoming connections.
func (s *Server) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = listener
	defer s.listener.Close() // Ensure listener is closed when function exits

	log.Printf("NNTP server listening on %s", addr)

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Check if the error is due to the listener being closed (e.g., by Shutdown).
			if opError, ok := err.(*net.OpError); ok && opError.Err.Error() == "use of closed network connection" {
				log.Println("Listener closed, server loop terminating.")
				return nil // Normal shutdown
			}
			// For other errors, log and continue if possible, or return if fatal.
			// Depending on the error, continuing might not be feasible.
			log.Printf("Error accepting connection: %v. Server loop might terminate.", err)
			return err // Treat other accept errors as fatal for the server loop
		}
		// Handle connection in a new goroutine.
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	remoteAddr := conn.RemoteAddr().String()
	// The Perl script sets $0 = "colobus: $REMOTE"; this could be a session ID for logging.
	log.Printf("Accepted connection from %s", remoteAddr)

	session := NewSession(conn, s.config, s.db, s.groupNameToNum, s.groupNumToName, remoteAddr)
	defer session.Close() // Ensure connection is closed when session ends

	session.Serve() // Blocking call that runs the session loop
	log.Printf("Session ended for %s", remoteAddr)
}

// Shutdown gracefully shuts down the server by closing the listener.
// Active connections will continue until they complete or timeout.
func (s *Server) Shutdown() {
	log.Println("Shutting down NNTP server...")
	if s.listener != nil {
		err := s.listener.Close() // This will cause ListenAndServe's Accept to return an error
		if err != nil {
			log.Printf("Error closing listener: %v", err)
		}
	}
	// TODO: Implement more sophisticated shutdown:
	// 1. Stop accepting new connections (done by closing listener).
	// 2. Wait for active sessions to finish (e.g., using a WaitGroup).
	// 3. Or, after a timeout, forcibly close active sessions.
}

package nntpclient

import (
	"fmt"
	"log"
	"nntp-web/internal/config"
	// "github.com/russross/nntp" // Example of a real NNTP client library
)

import (
	"bufio"
	"fmt"
	"log"
	"net/textproto"
	"strings"

	"nntp-web/internal/config"
)

// Client for basic NNTP operations.
type Client struct {
	serverAddr string
	conn       *textproto.Conn
	reader     *bufio.Reader // For reading multi-line responses easily
}

// NewClient creates and connects to the NNTP server.
func NewClient(cfg *config.Config) (*Client, error) {
	if cfg.NNTPServer == "" {
		return nil, fmt.Errorf("nntp server address is not configured")
	}

	conn, err := textproto.Dial("tcp", cfg.NNTPServer)
	if err != nil {
		return nil, fmt.Errorf("nntp: failed to dial %s: %w", cfg.NNTPServer, err)
	}
	log.Printf("NNTP Client connected to server: %s", cfg.NNTPServer)

	// Read initial greeting from server (usually 200 or 201)
	// The reader for textproto.Conn is conn.Reader directly.
	// We'll wrap it for easier line-by-line or multi-line reading.
	reader := bufio.NewReader(conn.R)
	greeting, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("nntp: failed to read greeting: %w", err)
	}
	log.Printf("NNTP Server greeting: %s", strings.TrimSpace(greeting))
	if !strings.HasPrefix(greeting, "200") && !strings.HasPrefix(greeting, "201") {
		conn.Close()
		return nil, fmt.Errorf("nntp: unexpected server greeting: %s", greeting)
	}

	// Set client to read-only mode if supported by server (MODE READER)
	// This is good practice.
	if err := conn.Cmd("MODE READER"); err != nil {
		log.Printf("NNTP: MODE READER command failed (server might not support it or already in reader mode): %v", err)
		// Not a fatal error, proceed. Some servers might not require/support it.
		// Read response for MODE READER
		modeReaderResp, errModeReader := reader.ReadString('\n')
		if errModeReader != nil {
			log.Printf("NNTP: Error reading response for MODE READER: %v", errModeReader)
		} else {
			log.Printf("NNTP MODE READER response: %s", strings.TrimSpace(modeReaderResp))
			if !strings.HasPrefix(modeReaderResp, "200") && !strings.HasPrefix(modeReaderResp, "201") && !strings.HasPrefix(modeReaderResp, "480") { // 480 Auth required, some servers give this if already reader.
				// Potentially problematic, but continue.
			}
		}
	}


	return &Client{serverAddr: cfg.NNTPServer, conn: conn, reader: reader}, nil
}

// FetchArticleBody fetches the body of an article using its Message-ID.
// Note: groupName is used to select the group first, as some ARTICLE commands might need it.
func (c *Client) FetchArticleBody(messageID string, groupName string) (string, error) {
	if c.conn == nil {
		return "", fmt.Errorf("nntp: not connected")
	}

	// 1. Select the group
	// The messageID should be wrapped in <>, but check if it already is.
	// The h_messageid from DB includes them.
	cleanMessageID := strings.Trim(messageID, "<>")


	id, err := c.conn.Cmd("GROUP %s", groupName)
	if err != nil {
		return "", fmt.Errorf("nntp: GROUP %s command failed: %w", groupName, err)
	}
	c.conn.StartResponse(id)
	groupResp, err := c.reader.ReadString('\n')
	c.conn.EndResponse(id)
	if err != nil {
		return "", fmt.Errorf("nntp: failed to read response for GROUP %s: %w", groupName, err)
	}
	// Expected: "211 count low high groupname"
	if !strings.HasPrefix(groupResp, "211") {
		return "", fmt.Errorf("nntp: failed to select group %s: %s", groupName, strings.TrimSpace(groupResp))
	}
	log.Printf("NNTP: Selected group %s: %s", groupName, strings.TrimSpace(groupResp))

	// 2. Fetch article body by Message-ID
	// Use ARTICLE <message-id>
	// Some servers might not support looking up by Message-ID if it's not the current article.
	// The command `ARTICLE <message-id>` is standard.
	id, err = c.conn.Cmd("ARTICLE <%s>", cleanMessageID)
	if err != nil {
		return "", fmt.Errorf("nntp: ARTICLE <%s> command failed: %w", cleanMessageID, err)
	}
	c.conn.StartResponse(id)
	defer c.conn.EndResponse(id) // Ensure EndResponse is called

	// Read status line for ARTICLE command
	// Expected: "220 <articleNumber> <messageID> article follows"
	// Or: "430 No such article found"
	statusLine, err := c.reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("nntp: failed to read status for ARTICLE <%s>: %w", cleanMessageID, err)
	}

	if strings.HasPrefix(statusLine, "430") {
		return "", fmt.Errorf("nntp: article not found with Message-ID <%s> (server response: %s)", cleanMessageID, strings.TrimSpace(statusLine))
	}
	if !strings.HasPrefix(statusLine, "220") { // 220 is for article, 221 for head, 222 for body
		return "", fmt.Errorf("nntp: unexpected response for ARTICLE <%s>: %s", cleanMessageID, strings.TrimSpace(statusLine))
	}

	// Read the multi-line body
	var bodyLines []string
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			// This could be EOF if connection drops, or other read errors
			return "", fmt.Errorf("nntp: error reading article body line: %w", err)
		}
		// Dot on a line by itself signifies end of data
		if line == ".\r\n" || line == ".\n" {
			break
		}
		// Unescape leading dots (some servers might dot-stuff lines starting with a dot)
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		bodyLines = append(bodyLines, line)
	}

	return strings.Join(bodyLines, ""), nil
}

// Close disconnects the NNTP client.
func (c *Client) Close() error {
	if c.conn != nil {
		log.Println("NNTP Client closing connection.")
		err := c.conn.Cmd("QUIT")
		// Read QUIT response
		if err == nil {
			quitResp, _ := c.reader.ReadString('\n')
			log.Printf("NNTP QUIT response: %s", strings.TrimSpace(quitResp))
		}
		return c.conn.Close()
	}
	return nil
}

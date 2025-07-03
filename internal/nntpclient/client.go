package nntpclient

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
	cfg    *config.Config // Store config for reconnects
	conn   *textproto.Conn
	reader *bufio.Reader
}

// connect establishes a new connection to the NNTP server and performs handshake.
func (c *Client) connect() error {
	if c.cfg.NNTPServer == "" { // Should have been checked by NewClient, but good for internal method too
		return fmt.Errorf("nntp: server address is not configured for connect")
	}

	conn, err := textproto.Dial("tcp", c.cfg.NNTPServer)
	if err != nil {
		return fmt.Errorf("nntp: failed to dial %s: %w", c.cfg.NNTPServer, err)
	}
	log.Printf("NNTP Client connected to server: %s", c.cfg.NNTPServer)

	reader := bufio.NewReader(conn.R)
	greeting, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return fmt.Errorf("nntp: failed to read greeting: %w", err)
	}
	log.Printf("NNTP Server greeting: %s", strings.TrimSpace(greeting))
	if !strings.HasPrefix(greeting, "200") && !strings.HasPrefix(greeting, "201") {
		conn.Close()
		return fmt.Errorf("nntp: unexpected server greeting: %s", greeting)
	}

	modeCmdID, err := conn.Cmd("MODE READER")
	if err != nil {
		log.Printf("NNTP: Sending MODE READER command failed: %v", err)
	} else {
		conn.StartResponse(modeCmdID)
		modeReaderResp, errModeReader := reader.ReadString('\n')
		conn.EndResponse(modeCmdID)
		if errModeReader != nil {
			log.Printf("NNTP: Error reading response for MODE READER: %v", errModeReader)
		} else {
			log.Printf("NNTP MODE READER response: %s", strings.TrimSpace(modeReaderResp))
			if !strings.HasPrefix(modeReaderResp, "200") && !strings.HasPrefix(modeReaderResp, "201") && !strings.HasPrefix(modeReaderResp, "480") {
				// Potentially problematic
			}
		}
	}

	c.conn = conn
	c.reader = reader
	return nil
}

// NewClient creates a new NNTP client and establishes the initial connection.
func NewClient(cfg *config.Config) (*Client, error) {
	if cfg.NNTPServer == "" {
		return nil, fmt.Errorf("nntp server address is not configured")
	}
	client := &Client{cfg: cfg}
	if err := client.connect(); err != nil {
		return nil, err // connect() already logs and wraps errors
	}
	return client, nil
}

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	// Check for common network error types or substrings
	// This list can be expanded. textproto.Error might wrap net.OpError.
	// io.EOF can also indicate a closed connection.
	s := err.Error()
	if strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "connection reset by peer") ||
		strings.Contains(s, "network is unreachable") ||
		strings.Contains(s, "no such host") ||
		strings.Contains(s, "connection refused") ||
		err == bufio.ErrBufferFull || // Could happen if reader buffer is an issue with dead conn
		err.Error() == "EOF" { // For io.EOF
		return true
	}
	// TODO: Could also use errors.As to check for net.Error and check Temporary() or Timeout()
	return false
}


// FetchRawArticle fetches the raw article content (headers + body) using its Message-ID.
// It will attempt to reconnect and retry once if a network error is detected.
func (c *Client) FetchRawArticle(messageID string, groupName string) (string, error) {
	var rawArticle string
	var err error

	for i := 0; i < 2; i++ { // Allow one retry (total 2 attempts)
		if c.conn == nil {
			if i > 0 {
				return "", fmt.Errorf("nntp: not connected after retry attempt")
			}
			log.Println("NNTP: Connection is nil, attempting to connect...")
			err = c.connect()
			if err != nil {
				return "", fmt.Errorf("nntp: initial connect failed: %w", err)
			}
		}

		rawArticle, err = c.fetchRawArticleAttempt(messageID, groupName)
		if err == nil {
			return rawArticle, nil // Success
		}

		log.Printf("NNTP: FetchRawArticle attempt %d failed: %v", i+1, err)

		if isNetworkError(err) {
			log.Println("NNTP: Detected network error, attempting to reconnect...")
			c.Close()
			if connectErr := c.connect(); connectErr != nil {
				log.Printf("NNTP: Reconnect failed: %v", connectErr)
				return "", fmt.Errorf("nntp: reconnect failed after network error: %w (original error: %v)", connectErr, err)
			}
			log.Println("NNTP: Reconnect successful, retrying command.")
		} else {
			return "", err
		}
	}
	return "", fmt.Errorf("nntp: failed to fetch raw article after retry: %w", err)
}


// fetchRawArticleAttempt contains the actual logic for fetching a raw article (headers + body).
func (c *Client) fetchRawArticleAttempt(messageID string, groupName string) (string, error) {
	// This function assumes c.conn is not nil.
	cleanMessageID := strings.Trim(messageID, "<>")

	// 1. Select the group
	cmdID, err := c.conn.Cmd("GROUP %s", groupName)
	if err != nil {
		return "", fmt.Errorf("nntp: GROUP %s command failed: %w", groupName, err)
	}
	c.conn.StartResponse(cmdID)
	groupResp, err := c.reader.ReadString('\n')
	c.conn.EndResponse(cmdID)
	if err != nil {
		return "", fmt.Errorf("nntp: failed to read response for GROUP %s: %w", groupName, err)
	}
	if !strings.HasPrefix(groupResp, "211") {
		return "", fmt.Errorf("nntp: failed to select group %s: %s", groupName, strings.TrimSpace(groupResp))
	}

	// 2. Fetch raw article by Message-ID using ARTICLE command
	cmdID, err = c.conn.Cmd("ARTICLE <%s>", cleanMessageID)
	if err != nil {
		return "", fmt.Errorf("nntp: ARTICLE <%s> command failed: %w", cleanMessageID, err)
	}
	c.conn.StartResponse(cmdID)
	defer c.conn.EndResponse(cmdID)

	statusLine, err := c.reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("nntp: failed to read status for ARTICLE <%s>: %w", cleanMessageID, err)
	}

	if strings.HasPrefix(statusLine, "430") { // Article not found
		return "", fmt.Errorf("nntp: article not found with Message-ID <%s> (server response: %s)", cleanMessageID, strings.TrimSpace(statusLine))
	}
	if !strings.HasPrefix(statusLine, "220") { // 220 Article follows (headers and body)
		return "", fmt.Errorf("nntp: unexpected response for ARTICLE <%s>: %s", cleanMessageID, strings.TrimSpace(statusLine))
	}

	// Read the rest of the article (headers and body) until the terminating dot.
	// Use c.conn.Reader (the textproto.Reader part of Conn) for ReadDotBytes.
	rawArticleBytes, err := c.conn.Reader.ReadDotBytes()
	if err != nil {
		return "", fmt.Errorf("nntp: error reading raw article content: %w", err)
	}

	// The statusLine is part of the server's response but not part of the raw article itself.
	// The rawArticleBytes contains headers and body.
	return string(rawArticleBytes), nil
}


// Close disconnects the NNTP client.
func (c *Client) Close() error {
	if c.conn != nil {
		log.Println("NNTP Client closing connection.")
		// Capture both id and err from conn.Cmd
		quitCmdID, err := c.conn.Cmd("QUIT")
		if err == nil {
			c.conn.StartResponse(quitCmdID)
			quitResp, _ := c.reader.ReadString('\n')
			c.conn.EndResponse(quitCmdID)
			log.Printf("NNTP QUIT response: %s", strings.TrimSpace(quitResp))
		} else {
			log.Printf("NNTP: Sending QUIT command failed: %v", err)
		}
		return c.conn.Close()
	}
	return nil
}

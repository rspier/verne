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
	if c.cfg.NNTPServer == "" {
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
		return nil, err
	}
	return client, nil
}

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	if strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "connection reset by peer") ||
		strings.Contains(s, "network is unreachable") ||
		strings.Contains(s, "no such host") ||
		strings.Contains(s, "connection refused") ||
		err.Error() == "EOF" {
		return true
	}
	return false
}

// FetchRawArticle fetches the raw article content (headers + body) using its article number within a group.
// It will attempt to reconnect and retry once if a network error is detected.
// Returns raw article content as []byte.
func (c *Client) FetchRawArticle(groupName string, articleNum uint32) ([]byte, error) {
	var rawArticleBytes []byte
	var err error

	for i := 0; i < 2; i++ { // Allow one retry (total 2 attempts)
		if c.conn == nil {
			if i > 0 {
				log.Println("NNTP: Still not connected after retry attempt.")
				return nil, fmt.Errorf("nntp: not connected after retry attempt")
			}
			log.Println("NNTP: Connection is nil, attempting to connect...")
			err = c.connect()
			if err != nil {
				return nil, fmt.Errorf("nntp: initial connect call failed: %w", err)
			}
		}

		rawArticleBytes, err = c.fetchRawArticleAttempt(groupName, articleNum)
		if err == nil {
			return rawArticleBytes, nil // Success
		}

		log.Printf("NNTP: FetchRawArticle attempt %d for group %s, article %d failed: %v", i+1, groupName, articleNum, err)

		if isNetworkError(err) {
			log.Println("NNTP: Detected network error, attempting to reconnect...")
			c.Close()
			if connectErr := c.connect(); connectErr != nil {
				log.Printf("NNTP: Reconnect failed: %v", connectErr)
				return nil, fmt.Errorf("nntp: reconnect failed after network error: %w (original error: %v)", connectErr, err)
			}
			log.Println("NNTP: Reconnect successful, retrying command.")
		} else {
			return nil, err
		}
	}
	return nil, fmt.Errorf("nntp: failed to fetch raw article for group %s, article %d after retry: %w", groupName, articleNum, err)
}

// fetchRawArticleAttempt contains the actual logic for fetching a raw article (headers + body) by article number.
func (c *Client) fetchRawArticleAttempt(groupName string, articleNum uint32) ([]byte, error) {
	if c.conn == nil { // Should not happen if FetchRawArticle calls it correctly
		return nil, fmt.Errorf("nntp: fetchRawArticleAttempt called with nil connection")
	}

	// 1. Select the group
	cmdID, err := c.conn.Cmd("GROUP %s", groupName)
	if err != nil {
		return nil, fmt.Errorf("nntp: GROUP %s command failed: %w", groupName, err)
	}
	c.conn.StartResponse(cmdID)
	groupResp, err := c.reader.ReadString('\n')
	c.conn.EndResponse(cmdID)
	if err != nil {
		return nil, fmt.Errorf("nntp: failed to read response for GROUP %s: %w", groupName, err)
	}
	if !strings.HasPrefix(groupResp, "211") { // 211 group selected
		return nil, fmt.Errorf("nntp: failed to select group %s: %s", groupName, strings.TrimSpace(groupResp))
	}

	// 2. Fetch raw article by article number using ARTICLE command
	cmdID, err = c.conn.Cmd("ARTICLE %d", articleNum)
	if err != nil {
		return nil, fmt.Errorf("nntp: ARTICLE %d command failed: %w", articleNum, err)
	}
	c.conn.StartResponse(cmdID)
	defer c.conn.EndResponse(cmdID)

	statusLine, err := c.reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("nntp: failed to read status for ARTICLE %d: %w", articleNum, err)
	}

	if strings.HasPrefix(statusLine, "430") { // 430 No such article found
		return nil, fmt.Errorf("nntp: article %d not found in group %s (server response: %s)", articleNum, groupName, strings.TrimSpace(statusLine))
	}
	if !strings.HasPrefix(statusLine, "220") { // 220 Article retrieved (head and body follow)
		return nil, fmt.Errorf("nntp: unexpected response for ARTICLE %d in group %s: %s", articleNum, groupName, strings.TrimSpace(statusLine))
	}

	rawArticleData, err := c.conn.Reader.ReadDotBytes()
	if err != nil {
		return nil, fmt.Errorf("nntp: error reading raw article content for article %d in group %s: %w", articleNum, groupName, err)
	}

	return rawArticleData, nil
}

// Close disconnects the NNTP client.
func (c *Client) Close() error {
	if c.conn != nil {
		log.Println("NNTP Client closing connection.")
		quitCmdID, err := c.conn.Cmd("QUIT")
		if err == nil {
			c.conn.StartResponse(quitCmdID)
			quitResp, _ := c.reader.ReadString('\n') // Error reading QUIT response is logged but not fatal to Close()
			c.conn.EndResponse(quitCmdID)
			log.Printf("NNTP QUIT response: %s", strings.TrimSpace(quitResp))
		} else {
			log.Printf("NNTP: Sending QUIT command failed: %v", err)
		}

		errClose := c.conn.Close()
		c.conn = nil // Ensure connection is marked as closed
		c.reader = nil
		return errClose
	}
	return nil
}

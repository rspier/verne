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
	// Capture both id and err from conn.Cmd
	modeCmdID, err := conn.Cmd("MODE READER")
	if err != nil {
		// This error is for sending the command, not the server's response to it.
		log.Printf("NNTP: Sending MODE READER command failed: %v", err)
		// Not necessarily fatal, some servers might not support it or be in this mode.
		// Try to read response anyway or just proceed.
	} else {
		// If cmd send was ok, handle response
		conn.StartResponse(modeCmdID)
		modeReaderResp, errModeReader := reader.ReadString('\n')
		conn.EndResponse(modeCmdID) // End response after reading
		if errModeReader != nil {
			log.Printf("NNTP: Error reading response for MODE READER: %v", errModeReader)
		} else {
			log.Printf("NNTP MODE READER response: %s", strings.TrimSpace(modeReaderResp))
			if !strings.HasPrefix(modeReaderResp, "200") && !strings.HasPrefix(modeReaderResp, "201") && !strings.HasPrefix(modeReaderResp, "480") {
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
	log.Printf("NNTP: Selected group %s: %s", groupName, strings.TrimSpace(groupResp))

	// 2. Fetch article body by Message-ID
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

	if strings.HasPrefix(statusLine, "430") {
		return "", fmt.Errorf("nntp: article not found with Message-ID <%s> (server response: %s)", cleanMessageID, strings.TrimSpace(statusLine))
	}
	if !strings.HasPrefix(statusLine, "220") {
		return "", fmt.Errorf("nntp: unexpected response for ARTICLE <%s>: %s", cleanMessageID, strings.TrimSpace(statusLine))
	}

	var bodyLines []string
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("nntp: error reading article body line: %w", err)
		}
		if line == ".\r\n" || line == ".\n" {
			break
		}
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

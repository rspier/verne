package nntpclient

import (
	"bufio"
	"fmt"
	"log"
	"net/textproto"
	"strings"
	"context" // Added for OTel

	"nntp-web/internal/config"
	"go.opentelemetry.io/otel"                        // Added for OTel
	"go.opentelemetry.io/otel/attribute"              // Added for OTel
	"go.opentelemetry.io/otel/codes"                  // Added for OTel
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0" // Added for OTel
	"go.opentelemetry.io/otel/trace"                  // Added for OTel
)

var tracer = otel.Tracer("nntp-web/internal/nntpclient") // OTel tracer

// Client for basic NNTP operations.
type Client struct {
	cfg    *config.Config // Store config for reconnects
	conn   *textproto.Conn
	reader *bufio.Reader
}

// connect establishes a new connection to the NNTP server and performs handshake.
// It now accepts a context for tracing.
func (c *Client) connect(ctx context.Context) error {
	_, span := tracer.Start(ctx, "NNTPClient.connect",
		trace.WithAttributes(
			semconv.NetPeerNameKey.String(c.cfg.NNTPServer), // Assuming NNTPServer is "host:port"
		),
	)
	defer span.End()

	if c.cfg.NNTPServer == "" {
		err := fmt.Errorf("nntp: server address is not configured for connect")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
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
		err = fmt.Errorf("nntp: failed to read greeting: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	log.Printf("NNTP Server greeting: %s", strings.TrimSpace(greeting))
	span.SetAttributes(attribute.String("nntp.server_greeting", strings.TrimSpace(greeting)))
	if !strings.HasPrefix(greeting, "200") && !strings.HasPrefix(greeting, "201") {
		conn.Close()
		err = fmt.Errorf("nntp: unexpected server greeting: %s", greeting)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
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
// It now accepts a context for tracing the initial connection.
func NewClient(ctx context.Context, cfg *config.Config) (*Client, error) {
	if cfg.NNTPServer == "" {
		return nil, fmt.Errorf("nntp server address is not configured")
	}
	client := &Client{cfg: cfg}
	if err := client.connect(ctx); err != nil { // Pass context to connect
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
// Returns raw article content as []byte. It now accepts a context for tracing.
func (c *Client) FetchRawArticle(ctx context.Context, groupName string, articleNum uint32) ([]byte, error) {
	ctx, span := tracer.Start(ctx, "NNTPClient.FetchRawArticle",
		trace.WithAttributes(
			attribute.String("nntp.group", groupName),
			attribute.Int64("nntp.article_num", int64(articleNum)),
			semconv.NetPeerNameKey.String(c.cfg.NNTPServer),
		),
	)
	defer span.End()

	var rawArticleBytes []byte
	var err error

	for i := 0; i < 2; i++ { // Allow one retry (total 2 attempts)
		span.SetAttributes(attribute.Int("nntp.fetch_attempt", i+1))
		if c.conn == nil {
			if i > 0 {
				log.Println("NNTP: Still not connected after retry attempt.")
				err = fmt.Errorf("nntp: not connected after retry attempt")
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return nil, err
			}
			log.Println("NNTP: Connection is nil, attempting to connect...")
			// Use the passed-in context for the connect call
			err = c.connect(ctx)
			if err != nil {
				err = fmt.Errorf("nntp: initial connect call failed: %w", err)
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return nil, err
			}
		}

		// Pass the context to the attempt function
		rawArticleBytes, err = c.fetchRawArticleAttempt(ctx, groupName, articleNum, i+1)
		if err == nil {
			span.SetStatus(codes.Ok, "Article fetched successfully")
			return rawArticleBytes, nil // Success
		}

		log.Printf("NNTP: FetchRawArticle main loop, attempt %d for group %s, article %d failed: %v", i+1, groupName, articleNum, err)
		span.AddEvent("Fetch attempt failed", trace.WithAttributes(attribute.String("error.message", err.Error())))


		if isNetworkError(err) {
			log.Println("NNTP: Detected network error, attempting to reconnect...")
			span.AddEvent("Network error detected, attempting reconnect")
			c.Close() // Close does not take context yet, will instrument it separately
			if connectErr := c.connect(ctx); connectErr != nil { // Use context for reconnect
				log.Printf("NNTP: Reconnect failed: %v", connectErr)
				err = fmt.Errorf("nntp: reconnect failed after network error: %w (original error: %v)", connectErr, err)
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return nil, err
			}
			log.Println("NNTP: Reconnect successful, retrying command.")
			span.AddEvent("Reconnect successful")
		} else {
			// Non-network error, fail immediately
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
	}
	// If loop finishes, it means all retries failed
	err = fmt.Errorf("nntp: failed to fetch raw article for group %s, article %d after retry: %w", groupName, articleNum, err)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return nil, err
}

// fetchRawArticleAttempt contains the actual logic for fetching a raw article (headers + body) by article number.
// It now accepts a context for tracing. attemptNum is for logging/tracing.
func (c *Client) fetchRawArticleAttempt(ctx context.Context, groupName string, articleNum uint32, attemptNum int) ([]byte, error) {
	ctx, span := tracer.Start(ctx, "NNTPClient.fetchRawArticleAttempt",
		trace.WithAttributes(
			attribute.String("nntp.group", groupName),
			attribute.Int64("nntp.article_num", int64(articleNum)),
			attribute.Int("nntp.attempt_num", attemptNum),
		),
	)
	defer span.End()

	if c.conn == nil {
		err := fmt.Errorf("nntp: fetchRawArticleAttempt called with nil connection")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// 1. Select the group
	cmdID, err := c.conn.Cmd("GROUP %s", groupName)
	if err != nil {
		err = fmt.Errorf("nntp: GROUP %s command failed: %w", groupName, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	c.conn.StartResponse(cmdID)
	groupResp, err := c.reader.ReadString('\n')
	c.conn.EndResponse(cmdID)
	if err != nil {
		err = fmt.Errorf("nntp: failed to read response for GROUP %s: %w", groupName, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.String("nntp.group_response", strings.TrimSpace(groupResp)))
	if !strings.HasPrefix(groupResp, "211") { // 211 group selected
		err = fmt.Errorf("nntp: failed to select group %s: %s", groupName, strings.TrimSpace(groupResp))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// 2. Fetch raw article by article number using ARTICLE command
	cmdID, err = c.conn.Cmd("ARTICLE %d", articleNum)
	if err != nil {
		err = fmt.Errorf("nntp: ARTICLE %d command failed: %w", articleNum, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	c.conn.StartResponse(cmdID)
	defer c.conn.EndResponse(cmdID) // This defer should be after StartResponse and before any early returns related to this command

	statusLine, err := c.reader.ReadString('\n')
	if err != nil {
		err = fmt.Errorf("nntp: failed to read status for ARTICLE %d: %w", articleNum, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.String("nntp.article_response", strings.TrimSpace(statusLine)))

	if strings.HasPrefix(statusLine, "430") { // 430 No such article found
		err = fmt.Errorf("nntp: article %d not found in group %s (server response: %s)", articleNum, groupName, strings.TrimSpace(statusLine))
		// This is a "not found" error, which might be common.
		// OTel spec suggests not setting Error status for "not found" unless it's unexpected.
		// For now, we'll record it as an error to be visible.
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error()) // Or codes.Unset if treating "not found" differently
		return nil, err
	}
	if !strings.HasPrefix(statusLine, "220") { // 220 Article retrieved (head and body follow)
		err = fmt.Errorf("nntp: unexpected response for ARTICLE %d in group %s: %s", articleNum, groupName, strings.TrimSpace(statusLine))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	rawArticleData, err := c.conn.Reader.ReadDotBytes()
	if err != nil {
		err = fmt.Errorf("nntp: error reading raw article content for article %d in group %s: %w", articleNum, groupName, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetStatus(codes.Ok, "Article attempt successful")
	return rawArticleData, nil
}

// Close disconnects the NNTP client.
func (c *Client) Close() error {
	// Using context.Background() as Close is often called in defers or shutdown
	// where a request-specific context might not be available or relevant.
	_, span := tracer.Start(context.Background(), "NNTPClient.Close",
		trace.WithAttributes(semconv.NetPeerNameKey.String(c.cfg.NNTPServer)),
	)
	defer span.End()

	if c.conn != nil {
		log.Println("NNTP Client closing connection.")
		var quitErr error
		quitCmdID, cmdErr := c.conn.Cmd("QUIT")
		if cmdErr == nil {
			c.conn.StartResponse(quitCmdID)
			quitResp, readErr := c.reader.ReadString('\n') // Error reading QUIT response is logged but not fatal to Close()
			c.conn.EndResponse(quitCmdID)
			log.Printf("NNTP QUIT response: %s", strings.TrimSpace(quitResp))
			if readErr != nil {
				quitErr = fmt.Errorf("nntp: error reading QUIT response: %w", readErr)
				span.AddEvent("QUIT response read error", trace.WithAttributes(attribute.String("error.message", quitErr.Error())))
			}
			span.SetAttributes(attribute.String("nntp.quit_response", strings.TrimSpace(quitResp)))
		} else {
			quitErr = fmt.Errorf("nntp: sending QUIT command failed: %w", cmdErr)
			log.Printf("%v", quitErr) // Log the error
			span.RecordError(quitErr) // Record it on the span
		}

		errClose := c.conn.Close()
		c.conn = nil // Ensure connection is marked as closed
		c.reader = nil

		if errClose != nil {
			span.RecordError(errClose)
			span.SetStatus(codes.Error, "Failed to close connection")
			// If QUIT also failed, wrap the errors
			if quitErr != nil {
				return fmt.Errorf("nntp: error during close (QUIT command: %v): %w", quitErr, errClose)
			}
			return errClose
		}
		if quitErr != nil {
			// Connection closed successfully, but QUIT command had an issue
			span.SetStatus(codes.Error, "QUIT command failed during close")
			return quitErr
		}
		span.SetStatus(codes.Ok, "Connection closed successfully")
		return nil
	}
	span.SetStatus(codes.Ok, "Connection already nil")
	return nil
}

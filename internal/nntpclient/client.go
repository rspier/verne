package nntpclient

import (
	"bufio"
	"fmt"
	"log"
	"net/textproto"
	"strings"
	"context" // Added for OpenTelemetry

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace" // Added for oteltrace.WithAttributes
	semconv "go.opentelemetry.io/otel/semconv/v1.25.0" // Trying semconv v1.25.0

	"nntp-web/internal/config"
)

const nntpTracerName = "nntp-client" // OpenTelemetry tracer name

// Client for basic NNTP operations.
type Client struct {
	cfg    *config.Config // Store config for reconnects
	conn   *textproto.Conn
	reader *bufio.Reader
}

// connect establishes a new connection to the NNTP server and performs handshake.
func (c *Client) connect() error {
	// Using context.Background() as context is not passed down.
	// In a real-world scenario, context should be propagated.
	ctx, span := otel.Tracer(nntpTracerName).Start(context.Background(), "NNTP.connect",
		oteltrace.WithAttributes( // Corrected: Use oteltrace.WithAttributes
			semconv.NetPeerNameKey.String(strings.Split(c.cfg.NNTPServer, ":")[0]), // Corrected: Use Key.String()
			// semconv.NetPeerPortKey.Int(port), // Example if port was parsed
		),
	)
	defer span.End()

	if c.cfg.NNTPServer == "" {
		err := fmt.Errorf("nntp: server address is not configured for connect")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetAttributes(attribute.String("nntp.server.address", c.cfg.NNTPServer))

	conn, err := textproto.Dial("tcp", c.cfg.NNTPServer)
	if err != nil {
		err = fmt.Errorf("nntp: failed to dial %s: %w", c.cfg.NNTPServer, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	log.Printf("NNTP Client connected to server: %s", c.cfg.NNTPServer)
	span.AddEvent("NNTP dial successful")

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
	span.SetAttributes(attribute.String("nntp.server.greeting", strings.TrimSpace(greeting)))

	if !strings.HasPrefix(greeting, "200") && !strings.HasPrefix(greeting, "201") {
		conn.Close()
		err = fmt.Errorf("nntp: unexpected server greeting: %s", greeting)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Instrumenting MODE READER command
	_, modeCmdSpan := otel.Tracer(nntpTracerName).Start(ctx, "NNTP.MODE_READER") // Use parent ctx, assign child to _
	modeCmdID, err := conn.Cmd("MODE READER")
	if err != nil {
		log.Printf("NNTP: Sending MODE READER command failed: %v", err)
		modeCmdSpan.RecordError(err)
		modeCmdSpan.SetStatus(codes.Error, "send MODE READER failed")
		// Not returning error here as original code doesn't treat this as fatal for connect
	} else {
		conn.StartResponse(modeCmdID)
		modeReaderResp, errModeReader := reader.ReadString('\n')
		conn.EndResponse(modeCmdID)
		if errModeReader != nil {
			log.Printf("NNTP: Error reading response for MODE READER: %v", errModeReader)
			modeCmdSpan.RecordError(errModeReader)
			modeCmdSpan.SetStatus(codes.Error, "read MODE READER response failed")
		} else {
			log.Printf("NNTP MODE READER response: %s", strings.TrimSpace(modeReaderResp))
			modeCmdSpan.SetAttributes(attribute.String("nntp.command.response", strings.TrimSpace(modeReaderResp)))
			if !strings.HasPrefix(modeReaderResp, "200") && !strings.HasPrefix(modeReaderResp, "201") && !strings.HasPrefix(modeReaderResp, "480") {
				// Potentially problematic, but original code doesn't fail connection
				modeCmdSpan.SetStatus(codes.Unset, "MODE READER response not strictly success/auth_required but connection proceeds")
			} else {
				modeCmdSpan.SetStatus(codes.Ok, "MODE READER successful or auth required")
			}
		}
	}
	modeCmdSpan.End()

	c.conn = conn
	c.reader = reader
	span.SetStatus(codes.Ok, "NNTP connect successful")
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
	// Using context.Background() for now.
	ctx, span := otel.Tracer(nntpTracerName).Start(context.Background(), "NNTP.fetchRawArticleAttempt",
		oteltrace.WithAttributes( // Corrected
			attribute.String("nntp.group", groupName),
			attribute.Int64("nntp.article_number", int64(articleNum)),
			semconv.NetPeerNameKey.String(strings.Split(c.cfg.NNTPServer, ":")[0]), // Corrected
		),
	)
	defer span.End()

	if c.conn == nil { // Should not happen if FetchRawArticle calls it correctly
		err := fmt.Errorf("nntp: fetchRawArticleAttempt called with nil connection")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// 1. Select the group
	groupCmdCtx, groupCmdSpan := otel.Tracer(nntpTracerName).Start(ctx, "NNTP.GROUP")
	groupCmdSpan.SetAttributes(attribute.String("nntp.command.group_name", groupName))
	cmdID, err := c.conn.Cmd("GROUP %s", groupName)
	if err != nil {
		err = fmt.Errorf("nntp: GROUP %s command failed: %w", groupName, err)
		groupCmdSpan.RecordError(err)
		groupCmdSpan.SetStatus(codes.Error, err.Error())
		groupCmdSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, "GROUP command failed")
		return nil, err
	}
	c.conn.StartResponse(cmdID)
	groupResp, err := c.reader.ReadString('\n')
	c.conn.EndResponse(cmdID)
	groupCmdSpan.SetAttributes(attribute.String("nntp.command.response", strings.TrimSpace(groupResp)))
	if err != nil {
		err = fmt.Errorf("nntp: failed to read response for GROUP %s: %w", groupName, err)
		groupCmdSpan.RecordError(err)
		groupCmdSpan.SetStatus(codes.Error, err.Error())
		groupCmdSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, "GROUP response read failed")
		return nil, err
	}
	if !strings.HasPrefix(groupResp, "211") { // 211 group selected
		err = fmt.Errorf("nntp: failed to select group %s: %s", groupName, strings.TrimSpace(groupResp))
		groupCmdSpan.RecordError(err)
		groupCmdSpan.SetStatus(codes.Error, err.Error())
		groupCmdSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, "GROUP selection failed")
		return nil, err
	}
	groupCmdSpan.SetStatus(codes.Ok, "GROUP successful")
	groupCmdSpan.End()
	span.AddEvent("NNTP GROUP command successful")

	// 2. Fetch raw article by article number using ARTICLE command
	_, articleCmdSpan := otel.Tracer(nntpTracerName).Start(groupCmdCtx, "NNTP.ARTICLE") // Child of GROUP span's context, assign child ctx to _
	articleCmdSpan.SetAttributes(attribute.Int64("nntp.command.article_number", int64(articleNum)))
	cmdID, err = c.conn.Cmd("ARTICLE %d", articleNum)
	if err != nil {
		err = fmt.Errorf("nntp: ARTICLE %d command failed: %w", articleNum, err)
		articleCmdSpan.RecordError(err)
		articleCmdSpan.SetStatus(codes.Error, err.Error())
		articleCmdSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, "ARTICLE command failed")
		return nil, err
	}
	c.conn.StartResponse(cmdID)
	defer c.conn.EndResponse(cmdID) // This defer should be inside this function, not FetchRawArticle

	statusLine, err := c.reader.ReadString('\n')
	articleCmdSpan.SetAttributes(attribute.String("nntp.command.response", strings.TrimSpace(statusLine)))
	if err != nil {
		err = fmt.Errorf("nntp: failed to read status for ARTICLE %d: %w", articleNum, err)
		articleCmdSpan.RecordError(err)
		articleCmdSpan.SetStatus(codes.Error, err.Error())
		articleCmdSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, "ARTICLE response read failed")
		return nil, err
	}

	if strings.HasPrefix(statusLine, "430") { // 430 No such article found
		err = fmt.Errorf("nntp: article %d not found in group %s (server response: %s)", articleNum, groupName, strings.TrimSpace(statusLine))
		articleCmdSpan.RecordError(err) // Record as error, but it's a valid server response for "not found"
		articleCmdSpan.SetStatus(codes.Error, "article not found (430)") // Or codes.Unset if 430 is not an "error" for the command itself
		articleCmdSpan.End()
		span.SetAttributes(attribute.Bool("nntp.article_found", false))
		// Not necessarily an error for the parent span if 430 is an expected outcome.
		// For now, let's propagate it as an error.
		span.RecordError(err)
		span.SetStatus(codes.Error, "article not found (430)")
		return nil, err
	}
	if !strings.HasPrefix(statusLine, "220") { // 220 Article retrieved (head and body follow)
		err = fmt.Errorf("nntp: unexpected response for ARTICLE %d in group %s: %s", articleNum, groupName, strings.TrimSpace(statusLine))
		articleCmdSpan.RecordError(err)
		articleCmdSpan.SetStatus(codes.Error, err.Error())
		articleCmdSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, "ARTICLE unexpected response")
		return nil, err
	}
	articleCmdSpan.SetStatus(codes.Ok, "ARTICLE successful")
	articleCmdSpan.End()
	span.AddEvent("NNTP ARTICLE command successful")
	span.SetAttributes(attribute.Bool("nntp.article_found", true))

	rawArticleData, err := c.conn.Reader.ReadDotBytes()
	if err != nil {
		err = fmt.Errorf("nntp: error reading raw article content for article %d in group %s: %w", articleNum, groupName, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "ReadDotBytes failed")
		return nil, err
	}

	span.SetAttributes(attribute.Int("nntp.article_size", len(rawArticleData)))
	span.SetStatus(codes.Ok, "Article fetched successfully")
	return rawArticleData, nil
}

// Close disconnects the NNTP client.
func (c *Client) Close() error {
	// Using context.Background() for now.
	var peerName string
	if c.cfg != nil && c.cfg.NNTPServer != "" {
		peerName = strings.Split(c.cfg.NNTPServer, ":")[0]
	}
	ctx, span := otel.Tracer(nntpTracerName).Start(context.Background(), "NNTP.close", // Added ctx to use for child span
		oteltrace.WithAttributes( // Corrected
			semconv.NetPeerNameKey.String(peerName), // Corrected
		),
	)
	defer span.End()

	if c.conn != nil {
		log.Println("NNTP Client closing connection.")
		_, quitCmdSpan := otel.Tracer(nntpTracerName).Start(ctx, "NNTP.QUIT") // Used parent ctx, assigned child ctx to _
		quitCmdID, err := c.conn.Cmd("QUIT")
		if err == nil {
			c.conn.StartResponse(quitCmdID)
			quitResp, readErr := c.reader.ReadString('\n') // Error reading QUIT response is logged but not fatal to Close()
			c.conn.EndResponse(quitCmdID)
			log.Printf("NNTP QUIT response: %s", strings.TrimSpace(quitResp))
			quitCmdSpan.SetAttributes(attribute.String("nntp.command.response", strings.TrimSpace(quitResp)))
			if readErr != nil {
				quitCmdSpan.RecordError(readErr)
				quitCmdSpan.SetStatus(codes.Error, "read QUIT response failed")
			} else {
				quitCmdSpan.SetStatus(codes.Ok, "QUIT successful")
			}
		} else {
			log.Printf("NNTP: Sending QUIT command failed: %v", err)
			quitCmdSpan.RecordError(err)
			quitCmdSpan.SetStatus(codes.Error, "send QUIT command failed")
		}
		quitCmdSpan.End()

		errClose := c.conn.Close()
		if errClose != nil {
			span.RecordError(errClose)
			span.SetStatus(codes.Error, "textproto.Conn.Close failed")
		} else {
			span.SetStatus(codes.Ok, "Connection closed successfully")
		}
		c.conn = nil // Ensure connection is marked as closed
		c.reader = nil
		return errClose
	}
	span.SetStatus(codes.Ok, "Connection was already nil")
	return nil
}

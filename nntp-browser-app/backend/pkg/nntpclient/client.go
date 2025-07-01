package nntpclient

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net/textproto"
	"nntp-browser-app/backend/pkg/config"
	"nntp-browser-app/backend/pkg/models"
	"strconv"
	"strings"
	"time"

	"github.com/willglynn/nntp"
)

// NNTPClient wraps the willglynn/nntp.Conn.
type NNTPClient struct {
	Conn   NNTPConnection
	Config config.AppConfig
}

// New creates a new NNTPClient and connects to the server using willglynn/nntp.
func New(cfg config.AppConfig) (*NNTPClient, error) {
	if cfg.NNTPServer == "" {
		return nil, fmt.Errorf("NNTP server address is not configured")
	}

	log.Printf("Attempting to connect to NNTP server: %s (using willglynn/nntp)", cfg.NNTPServer)
	rawConn, err := nntp.Dial("tcp", cfg.NNTPServer)
	if err != nil {
		log.Printf("Failed to connect to NNTP server %s: %v", cfg.NNTPServer, err)
		return nil, fmt.Errorf("failed to connect to NNTP server %s: %w", cfg.NNTPServer, err)
	}
	log.Printf("Successfully connected to NNTP server: %s", cfg.NNTPServer)

	/* TODO: Add NNTPUser, NNTPPass to config.AppConfig and implement authentication.
	if cfg.NNTPUser != "" {
		log.Printf("Attempting authentication for user %s...", cfg.NNTPUser)
		err = rawConn.Authenticate(cfg.NNTPUser, cfg.NNTPPass)
		if err != nil {
			rawConn.Quit()
			log.Printf("NNTP authentication failed for user %s: %v", cfg.NNTPUser, err)
			return nil, fmt.Errorf("NNTP authentication failed for user %s: %w", cfg.NNTPUser, err)
		}
		log.Println("NNTP authentication successful.")
	}
	*/

	adaptedConn := NewNNTPConnAdapter(rawConn)
	return &NNTPClient{Conn: adaptedConn, Config: cfg}, nil
}

// GetGroups fetches the list of newsgroups.
func (c *NNTPClient) GetGroups() ([]models.Group, error) {
	if c.Conn == nil { return nil, errors.New("NNTP connection not established") }
	nntpGroups, err := c.Conn.List()
	if err != nil {
		log.Printf("Failed to list groups: %v", err)
		return nil, fmt.Errorf("failed to list groups: %w", err)
	}
	var groups []models.Group
	for _, ng := range nntpGroups {
		if ng == nil { continue }
		groups = append(groups, models.Group{
			Name:        ng.Name,
			Description: "",
			Count:       ng.Count,
			High:        ng.High,
			Low:         ng.Low,
		})
	}
	log.Printf("Fetched %d groups", len(groups))
	return groups, nil
}

// SelectGroup selects a newsgroup.
func (c *NNTPClient) SelectGroup(groupName string) (count int, low int, high int, err error) {
	if c.Conn == nil { return 0,0,0, errors.New("NNTP connection not established") }
	log.Printf("Selecting group: %s", groupName)
	g, err := c.Conn.Group(groupName)
	if err != nil {
		log.Printf("Failed to select group %s: %v", groupName, err)
		return 0, 0, 0, fmt.Errorf("failed to select group %s: %w", groupName, err)
	}
	if g == nil {
		log.Printf("Group %s selection returned nil group info", groupName)
		return 0,0,0, fmt.Errorf("group %s selection returned nil info", groupName)
	}
	log.Printf("Group %s selected. Count: %d, Low: %d, High: %d, Status: %s", g.Name, g.Count, g.Low, g.High, g.Status)
	return int(g.Count), int(g.Low), int(g.High), nil
}

// GetArticleOverviews fetches []nntp.MessageOverview using the Overview method.
func (c *NNTPClient) GetArticleOverviews(first, last int64) ([]nntp.MessageOverview, error) {
	if c.Conn == nil { return nil, errors.New("NNTP connection not established") }
	log.Printf("Fetching article overviews (Overview) for range: %d-%d", first, last)
	overviews, err := c.Conn.Overview(first, last)
	if err != nil {
		log.Printf("Overview command failed for range %d-%d: %v", first, last, err)
		return nil, fmt.Errorf("Overview command failed for range %d-%d: %w", first, last, err)
	}
	for i := range overviews {
		overviews[i].MessageId = strings.Trim(overviews[i].MessageId, "<>")
		for j, ref := range overviews[i].References {
			overviews[i].References[j] = strings.Trim(ref, "<>")
		}
	}
	log.Printf("Fetched %d article overviews for range %d-%d", len(overviews), first, last)
	return overviews, nil
}

// GetArticleHeaders uses GetArticleOverviews and maps to models.MessageOverview.
func (c *NNTPClient) GetArticleHeaders(first, last int) ([]models.MessageOverview, error) {
	if c.Conn == nil { return nil, errors.New("NNTP connection not established") }
	nntpOverviews, err := c.GetArticleOverviews(int64(first), int64(last))
	if err != nil {
		return nil, fmt.Errorf("failed to get article overviews: %w", err)
	}
	var overviews []models.MessageOverview
	for _, entry := range nntpOverviews {
		overview := models.MessageOverview{
			ID:         entry.MessageId,
			Number:     entry.MessageNumber,
			Subject:    entry.Subject,
			From:       entry.From,
			Date:       entry.Date,
		}
		overviews = append(overviews, overview)
	}
	log.Printf("Processed %d article overviews into models.MessageOverview", len(overviews))
	return overviews, nil
}

// GetArticleBody fetches and parses a specific article using willglynn/nntp.
func (c *NNTPClient) GetArticleBody(specifier string) (*models.Message, error) {
	if c.Conn == nil { return nil, errors.New("NNTP connection not established") }

	log.Printf("Fetching article for specifier: %s", specifier)
	article, err := c.Conn.Article(specifier) // article is *nntp.Article from willglynn/nntp
	if err != nil {
		log.Printf("Article command failed for specifier %s: %v", specifier, err)
		return nil, fmt.Errorf("failed to get article %s: %w", specifier, err)
	}
	if article == nil {
		return nil, fmt.Errorf("article %s not found or empty response from library", specifier)
	}

	// Read the body content from article.Body (io.Reader)
	bodyBytes, err := io.ReadAll(article.Body)
	if err != nil {
		log.Printf("Failed to read article body content for %s: %v", specifier, err)
		return nil, fmt.Errorf("failed to read article body content for %s: %w", specifier, err)
	}
	bodyString := string(bodyBytes)

	// Helper to get first value from header map (article.Header is map[string][]string)
	// Uses textproto.CanonicalMIMEHeaderKey for case-insensitive lookup.
	getHeader := func(h map[string][]string, key string) string {
		canonicalKey := textproto.CanonicalMIMEHeaderKey(key)
		if vals, ok := h[canonicalKey]; ok && len(vals) > 0 {
			return vals[0]
		}
		// Fallback for keys that might not be canonicalized in the map by the library
		if vals, ok := h[key]; ok && len(vals) > 0 {
			return vals[0]
		}
		return ""
	}

	subject := getHeader(article.Header, "Subject")
	from := getHeader(article.Header, "From")
	dateStr := getHeader(article.Header, "Date")
	actualMessageID := strings.Trim(getHeader(article.Header, "Message-ID"), "<>")
	refsStr := getHeader(article.Header, "References")

    if actualMessageID == "" {
        if strings.HasPrefix(specifier, "<") && strings.HasSuffix(specifier, ">") {
            actualMessageID = strings.Trim(specifier, "<>")
        } else {
            actualMessageID = specifier
        }
	}

	parsedDate, _ := ParseNNTPDate(dateStr)

	var references []string
	if refsStr != "" {
		rawRefs := strings.Fields(refsStr)
		for _, r := range rawRefs {
			references = append(references, strings.Trim(r, "<>"))
		}
	}

	message := &models.Message{
		ID:         actualMessageID,
		Subject:    subject,
		From:       from,
		Date:       parsedDate,
		References: references,
		Body:       bodyString, // Use the correctly read body string
	}

	log.Printf("Successfully fetched and parsed article: %s", message.ID)
	return message, nil
}

// Close terminates the connection to the NNTP server.
func (c *NNTPClient) Close() {
	if c.Conn != nil {
		log.Println("Closing NNTP connection (willglynn/nntp).")
		err := c.Conn.Quit()
		if err != nil {
			log.Printf("Error sending QUIT to NNTP server: %v", err)
		}
		c.Conn = nil
	}
}

// ParseNNTPDate tries to parse date strings from NNTP headers or OverItem.Date.
func ParseNNTPDate(dateStr string) (time.Time, error) {
	layouts := []string{
		time.RFC1123Z,
		time.RFC1123,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, _2 Jan 2006 15:04:05 -0700",
		"2 Jan 2006 15:04:05 -0700",
		"_2 Jan 2006 15:04:05 -0700",
		time.RFC822Z,
		time.RFC822,
	}
	for _, layout := range layouts {
		t, err := time.Parse(layout, dateStr)
		if err == nil {
			return t, nil
		}
	}
    parts := strings.Fields(dateStr)
    if len(parts) > 0 {
        lastPart := parts[len(parts)-1]
        if (len(lastPart) == 3 || len(lastPart) == 4) && !strings.Contains(dateStr, "+") && !strings.Contains(dateStr, "-") {
        }
    }
	log.Printf("Warning: Could not parse date string '%s' with known layouts.", dateStr)
	return time.Time{}, fmt.Errorf("could not parse date: %s", dateStr)
}

var _ = bufio.NewReader(nil)
var _ = textproto.NewReader(nil) // textproto.CanonicalMIMEHeaderKey is used
var _ = strconv.Itoa
var _ = io.EOF

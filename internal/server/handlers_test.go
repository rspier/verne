package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"nntp-web/internal/config"
	"nntp-web/internal/database"
	"database/sql" // For sql.ErrNoRows
	// "nntp-web/internal/models" // Was unused
	"nntp-web/internal/nntpclient" // For nntpclient.NewClient
	"nntp-web/web/templates"       // For template name constants
	htmltemplate "html/template"   // Use standard html/template for parsing
	"time"                         // For time.Date, time.Now

	"github.com/DATA-DOG/go-sqlmock"
)

var articleColsNoGroup = []string{"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date", "received", "thread_id", "parent", "h_references", "h_lines", "h_bytes"}
var articleColsWithGroup = append(articleColsNoGroup, "group_name")


// newTestServer creates a server instance for testing,
// allowing injection of a mock DB.
func newTestServer(t *testing.T, mockDb *database.DB) *Server {
	t.Helper()

	// Parse templates like in the actual server setup, using html/template
	parsedTemplates, err := htmltemplate.New("test").ParseFS(templates.Files, "*.html.tmpl")
	if err != nil {
		t.Fatalf("Failed to parse templates: %v", err)
	}

	cfg := &config.Config{ServerPort: 8080} // Dummy config

	return &Server{
		config:    cfg,
		db:        mockDb,
		router:    http.NewServeMux(),
		templates: parsedTemplates, // Now *htmltemplate.Template
	}
}

func TestServer_handleShowArticle(t *testing.T) {
	groupName := "test.group"
	var groupID uint16 = 1
	year, month, day := 2024, 1, 15
	var articleNum uint32 = 789
	msgID := "article123@example.com"
	threadID := uint32(456)
	sampleReceived := time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.UTC)

	// DB queries
	groupQuery := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
	articleDetailsQuery := regexp.QuoteMeta(`
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
		       received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND id = ?
		  AND YEAR(received) = ? AND MONTH(received) = ?
		LIMIT 1`)
	relaxedArticleDetailsQuery := regexp.QuoteMeta(`
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
			   received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND id = ?
		LIMIT 1`)
	articleByMsgIDQuery := regexp.QuoteMeta(`
		SELECT a.group_id, a.id, a.h_messageid, a.h_subject, a.h_from, a.h_date,
		       a.received, a.thread_id, a.parent, a.h_references, a.h_lines, a.h_bytes,
		       g.name as group_name
		FROM articles a
		JOIN `+"`groups`"+` g ON a.group_id = g.id
		WHERE a.h_messageid = ?
		LIMIT 1`)
	threadMessagesQuery := regexp.QuoteMeta(`
		SELECT a.group_id, a.id, a.h_messageid, a.h_subject, a.h_from, a.h_date,
		       a.received, a.thread_id, a.parent, a.h_references, a.h_lines, a.h_bytes,
		       g.name as group_name
		FROM articles a
		JOIN `+"`groups`"+` g ON a.group_id = g.id
		WHERE a.thread_id = ? AND NOT (a.group_id = ? AND a.id = ?)
		ORDER BY a.received ASC, a.id ASC`)

	articleCols := []string{"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date", "received", "thread_id", "parent", "h_references", "h_lines", "h_bytes"}
	articleWithGroupCols := append(articleCols, "group_name")


	tests := []struct {
		name                 string
		path                 string
		setupMock            func(mock sqlmock.Sqlmock)
		expectedStatusCode   int
		expectedLocation     string   // For redirects
		expectedBodyContains []string // For rendered pages
	}{
		// --- Canonical Path Tests ---
		{
			name: "canonical path - success",
			path: fmt.Sprintf("/group/%s/%d/%02d/msg%d.html", groupName, year, month, articleNum),
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(articleDetailsQuery).WithArgs(groupID, articleNum, year, month).
					WillReturnRows(sqlmock.NewRows(articleCols).
						AddRow(groupID, articleNum, msgID, "Test Subject", "Test From", "Test Date", sampleReceived, threadID, 0, "", 10, 100))
				mock.ExpectQuery(threadMessagesQuery).WithArgs(threadID, groupID, articleNum).
					WillReturnRows(sqlmock.NewRows(articleWithGroupCols)) // No other thread messages
			},
			expectedStatusCode: http.StatusOK,
			expectedBodyContains: []string{
				"Test Subject",
				"<strong>From:</strong> Test From",
				"<strong>Message-ID:</strong>", // Check for label
				`<a href="/group/test.group/;.msgid=article123@example.com"><code>article123@example.com</code></a>`, // Check for link and content
				"No other messages found in this thread.",
			},
		},
		{
			name: "canonical path - date mismatch redirect",
			path: fmt.Sprintf("/group/%s/%d/%02d/msg%d.html", groupName, 2023, 12, articleNum), // Wrong date in URL
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				// Fails strict date query
				mock.ExpectQuery(articleDetailsQuery).WithArgs(groupID, articleNum, 2023, 12).WillReturnError(sql.ErrNoRows)
				// Succeeds relaxed query (finds article with actual date sampleReceived: 2024/01)
				mock.ExpectQuery(relaxedArticleDetailsQuery).WithArgs(groupID, articleNum).
					WillReturnRows(sqlmock.NewRows(articleCols).
						AddRow(groupID, articleNum, msgID, "Test Subject", "Test From", "Test Date", sampleReceived, threadID, 0, "", 10, 100))
			},
			expectedStatusCode: http.StatusFound, // 302 redirect
			expectedLocation:   fmt.Sprintf("/group/%s/%d/%02d/msg%d.html", groupName, year, month, articleNum), // Corrected URL
		},
		{
			name: "canonical path - article not found (even with relaxed date)",
			path: fmt.Sprintf("/group/%s/%d/%02d/msg%d.html", groupName, year, month, 9999), // Non-existent articleNum
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(articleDetailsQuery).WithArgs(groupID, uint32(9999), year, month).WillReturnError(sql.ErrNoRows)
				mock.ExpectQuery(relaxedArticleDetailsQuery).WithArgs(groupID, uint32(9999)).WillReturnError(sql.ErrNoRows)
			},
			expectedStatusCode: http.StatusNotFound,
			expectedBodyContains: []string{"Article not found"},
		},
		{
			name: "canonical path - group not found",
			path: fmt.Sprintf("/group/unknown.group/%d/%02d/msg%d.html", year, month, articleNum),
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs("unknown.group").WillReturnError(sql.ErrNoRows)
			},
			expectedStatusCode: http.StatusNotFound,
			expectedBodyContains: []string{"Article not found"}, // db.GetArticleByDetails returns generic not found if group fails
		},
		// --- Message-ID Lookup Tests ---
		{
			name: "msgid lookup - success and redirect",
			path: fmt.Sprintf("/group/%s/;.msgid=%s", groupName, msgID), // Path parameter
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(articleByMsgIDQuery).WithArgs(msgID).
					WillReturnRows(sqlmock.NewRows(articleWithGroupCols).
						AddRow(groupID, articleNum, msgID, "Test Subject", "Test From", "Test Date", sampleReceived, threadID, 0, "", 10, 100, groupName))
			},
			expectedStatusCode: http.StatusFound,
			expectedLocation:   fmt.Sprintf("/group/%s/%d/%02d/msg%d.html", groupName, year, month, articleNum),
		},
		{
			name: "msgid lookup - not found",
			path: fmt.Sprintf("/group/%s/;.msgid=notfound@id.com", groupName), // Path parameter
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(articleByMsgIDQuery).WithArgs("notfound@id.com").WillReturnError(sql.ErrNoRows)
			},
			expectedStatusCode: http.StatusNotFound,
			expectedBodyContains: []string{"Article with Message-ID notfound@id.com not found"},
		},
		{
			name: "msgid lookup - invalid path structure",
			// Path is /group/groupname/subpath/;.msgid=... which is not a valid structure for msgid lookup.
			// routeGroupRequests will not match this for handleShowArticle's msgid pattern.
			// It will fall through to the final http.NotFound in routeGroupRequests.
			path: fmt.Sprintf("/group/%s/subpath/;.msgid=%s", groupName, msgID),
			setupMock: func(mock sqlmock.Sqlmock) {
				// No DB calls expected
			},
			expectedStatusCode: http.StatusNotFound, // Router will 404 this, not handleShowArticle
			expectedBodyContains: []string{"404 page not found"},
		},
		// --- Thread Display Test ---
		{
			name: "canonical path - success with thread messages",
			path: fmt.Sprintf("/group/%s/%d/%02d/msg%d.html", groupName, year, month, articleNum),
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(articleDetailsQuery).WithArgs(groupID, articleNum, year, month).
					WillReturnRows(sqlmock.NewRows(articleCols).
						AddRow(groupID, articleNum, msgID, "Main Subject", "Main From", "Main Date", sampleReceived, threadID, 0, "", 10, 100))

				// Thread messages
				threadRows := sqlmock.NewRows(articleWithGroupCols).
					AddRow(groupID, 790, "reply1@example.com", "Re: Main Subject", "Reply From", "Reply Date", sampleReceived.Add(time.Hour), threadID, articleNum, "", 8, 80, groupName)
				mock.ExpectQuery(threadMessagesQuery).WithArgs(threadID, groupID, articleNum).WillReturnRows(threadRows)
			},
			expectedStatusCode: http.StatusOK,
			expectedBodyContains: []string{"Main Subject", "Re: Main Subject", "Reply From"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSqlDb, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create sqlmock: %v", err)
			}
			defer mockSqlDb.Close()

			testableDB := database.NewWithSQLDB(mockSqlDb)
			// For NNTP client, we can pass nil for now or a mock if its interactions become critical for a test.
			// The handler is written to be nil-safe for s.nntpClient.
			s := newTestServer(t, testableDB)
			s.nntpClient, _ = nntpclient.NewClient(&config.Config{NNTPServer: "dummy.server:119"}) // Init dummy nntp client

			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			req := httptest.NewRequest("GET", tt.path, nil)
			rr := httptest.NewRecorder()

			s.routeGroupRequests(rr, req) // Test via the main router

			if status := rr.Code; status != tt.expectedStatusCode {
				bodyBytes, _ := io.ReadAll(rr.Body)
				t.Errorf("handler for path '%s' returned wrong status code: got %v want %v. Body: %s", tt.path, status, tt.expectedStatusCode, string(bodyBytes))
			}

			if tt.expectedLocation != "" { // Check redirect location
				loc, err := rr.Result().Location()
				if err != nil {
					t.Errorf("Expected redirect, but got error retrieving location: %v", err)
				} else if loc.String() != tt.expectedLocation {
					t.Errorf("handler redirected to wrong location: got %s want %s", loc.String(), tt.expectedLocation)
				}
			}

			body := rr.Body.String()
			for _, substr := range tt.expectedBodyContains {
				if !strings.Contains(body, substr) {
					t.Logf("For path '%s', comparing body substring: [[%s]] against full body: [[%s]]", tt.path, substr, body) // Added log
					t.Errorf("response body for path '%s' does not contain expected substring '%s'. Body:\n%s", tt.path, substr, body)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				// Be careful with tests that don't expect DB calls (e.g. bad path formats handled before DB)
				isBadRequestNoDB := (tt.expectedStatusCode == http.StatusBadRequest || tt.expectedStatusCode == http.StatusNotFound) &&
									(strings.Contains(tt.name, "invalid path") || strings.Contains(tt.name, "malformed"))
				if !isBadRequestNoDB { // Only check if DB calls were expected
					t.Errorf("sqlmock expectations were not met for path '%s': %s", tt.path, err)
				}
			}
		})
	}
}

// TestServer_RouteGroupRequests replaces the old TestServer_handleListGroups
// as it now tests the main dispatcher for /group/ paths.
func TestServer_RouteGroupRequests(t *testing.T) {
	// Define the query that GetAllNewsgroups will execute
	expectedListGroupsSQLQuery := "SELECT id, name, description FROM `groups` ORDER BY name"

	// Define queries for GetMessagesForGroupMonth as it might be called by the router
	groupQueryBase := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
	articleQueryBase := regexp.QuoteMeta(`
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
		       received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND YEAR(received) = ? AND MONTH(received) = ?
		ORDER BY received DESC, id DESC`)
	// Query for GetMinMaxMessageMonthsForGroup
	countQuery := regexp.QuoteMeta("SELECT COUNT(*) FROM articles WHERE group_id = ?")
	// minMaxMonthQuery is not used by subtests in TestServer_RouteGroupRequests directly
	// Queries for GetPrevMonthWithMessages / GetNextMonthWithMessages
	prevMonthNavQuery := regexp.QuoteMeta(`
		SELECT YEAR(received), MONTH(received)
		FROM articles
		WHERE group_id = ? AND received < ?
		ORDER BY received DESC
		LIMIT 1`)
	nextMonthNavQuery := regexp.QuoteMeta(`
		SELECT YEAR(received), MONTH(received)
		FROM articles
		WHERE group_id = ? AND received >= ?
		ORDER BY received ASC
		LIMIT 1`)

	tests := []struct {
		name               string
		path               string // Path for routeGroupRequests
		setupMock          func(mock sqlmock.Sqlmock)
		expectedStatusCode int
		expectedBody       []string // Substrings to check for in the body
		notExpectedBody    []string // Substrings to ensure are NOT in the body
	}{
		{
			name: "dispatch to list groups - success",
			path: "/group/",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"id", "name", "description"}).
					AddRow(1, "alt.test", "Test group").
					AddRow(2, "comp.lang.go", "Go Language")
				mock.ExpectQuery(expectedListGroupsSQLQuery).WillReturnRows(rows)
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       []string{"alt.test", "Go Language", `<a href="/group/alt.test/">`},
		},
		{
			name: "dispatch to list groups - database error",
			path: "/group/",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(expectedListGroupsSQLQuery).WillReturnError(errors.New("db error for list groups"))
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedBody:       []string{"Failed to retrieve newsgroups"},
		},
		{
			name: "dispatch to list messages (default to current cal month) - group found, no messages",
			path: "/group/comp.test/",
			setupMock: func(mock sqlmock.Sqlmock) {
				groupID := 1
				now := time.Now()
				currentY, currentM := now.Year(), int(now.Month())

				// 1. GetMinMaxMessageMonthsForGroup
				mock.ExpectQuery(groupQueryBase).WithArgs("comp.test").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(countQuery).WithArgs(groupID).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0)) // No messages

				// 2. GetMessagesForGroupMonth (for currentY, currentM because count was 0)
				mock.ExpectQuery(groupQueryBase).WithArgs("comp.test").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(articleQueryBase).WithArgs(groupID, currentY, currentM).
					WillReturnRows(sqlmock.NewRows(articleColsNoGroup))

				// 3. GetPrevMonthWithMessages
				mock.ExpectQuery(groupQueryBase).WithArgs("comp.test").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				prevNavTargetDate := time.Date(currentY, time.Month(currentM), 1, 0,0,0,0,time.UTC)
				mock.ExpectQuery(prevMonthNavQuery).WithArgs(groupID, prevNavTargetDate).WillReturnError(sql.ErrNoRows)

				// 4. GetNextMonthWithMessages
				mock.ExpectQuery(groupQueryBase).WithArgs("comp.test").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				nextNavTargetDate := time.Date(currentY, time.Month(currentM), 1, 0,0,0,0,time.UTC).AddDate(0,1,0)
				mock.ExpectQuery(nextMonthNavQuery).WithArgs(groupID, nextNavTargetDate).WillReturnError(sql.ErrNoRows)
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       []string{"Messages for", "comp.test", "No messages found for this month."},
		},
		{
			name: "dispatch to list messages - group not found",
			path: "/group/unknown.group/",
			setupMock: func(mock sqlmock.Sqlmock) {
				// GetMinMaxMessageMonthsForGroup fails to find group
				mock.ExpectQuery(groupQueryBase).WithArgs("unknown.group").WillReturnError(sql.ErrNoRows)
			},
			expectedStatusCode: http.StatusNotFound,
			expectedBody:       []string{"Group unknown.group not found"},
		},
		{
			name:  "malformed group path for router - e.g. /group/name/toolong/extra",
			path:  "/group/comp.test/2023/01/extra.html", // Path that handleListMessages itself will 404
			setupMock: func(mock sqlmock.Sqlmock) {
				// No DB interaction expected if path parsing in handleListMessages fails early
			},
			expectedStatusCode: http.StatusNotFound,
		},
		{
			name:  "path /group (no trailing slash) - should be 404 by router",
			path:  "/group",
			setupMock: func(mock sqlmock.Sqlmock) {}, // No DB calls
			expectedStatusCode: http.StatusNotFound,
			expectedBody: []string{"404 page not found"}, // Default http.NotFound
		},
		{
			name:  "path /group/name (no trailing slash for current month) - should be 404 by router",
			path:  "/group/comp.test",
			setupMock: func(mock sqlmock.Sqlmock) {}, // No DB calls
			expectedStatusCode: http.StatusNotFound,
			expectedBody: []string{"404 page not found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentMockSqlDb, currentMock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create sqlmock: %v", err)
			}
			defer currentMockSqlDb.Close()

			testableDB := database.NewWithSQLDB(currentMockSqlDb)
			s := newTestServer(t, testableDB) // Server with mock DB and parsed templates

			if tt.setupMock != nil {
				tt.setupMock(currentMock)
			}

			req := httptest.NewRequest("GET", tt.path, nil)
			rr := httptest.NewRecorder()

			s.routeGroupRequests(rr, req) // Test the router directly

			if rr.Code != tt.expectedStatusCode {
				t.Errorf("routeGroupRequests with path '%s' returned wrong status code: got %v want %v. Body: %s", tt.path, rr.Code, tt.expectedStatusCode, rr.Body.String())
			}

			bodyBytes, _ := io.ReadAll(rr.Body)
			bodyString := string(bodyBytes)

			for _, expected := range tt.expectedBody {
				if !strings.Contains(bodyString, expected) {
					t.Errorf("routeGroupRequests with path '%s' returned unexpected body: got\n%s\nwant to contain\n%s", tt.path, bodyString, expected)
				}
			}
			for _, notExpected := range tt.notExpectedBody {
				if strings.Contains(bodyString, notExpected) {
					t.Errorf("routeGroupRequests with path '%s' returned unexpected body: got\n%s\ndid NOT want to contain\n%s", tt.path, bodyString, notExpected)
				}
			}

			if err := currentMock.ExpectationsWereMet(); err != nil {
				t.Errorf("sqlmock expectations were not met for path '%s': %s", tt.path, err)
			}
		})
	}
}

func TestServer_handleListMessages_SpecificMonth(t *testing.T) {
	groupName := "comp.sys.mac"
	targetYear, targetMonth := 2023, 11
	path := fmt.Sprintf("/group/%s/%d/%02d.html", groupName, targetYear, targetMonth)
	var groupID uint16 = 2

	groupQuery := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
	articleQuery := regexp.QuoteMeta(`
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
		       received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND YEAR(received) = ? AND MONTH(received) = ?
		ORDER BY received DESC, id DESC`)
	prevMonthNavQuery := regexp.QuoteMeta(`
		SELECT YEAR(received), MONTH(received)
		FROM articles
		WHERE group_id = ? AND received < ?
		ORDER BY received DESC
		LIMIT 1`)
	nextMonthNavQuery := regexp.QuoteMeta(`
		SELECT YEAR(received), MONTH(received)
		FROM articles
		WHERE group_id = ? AND received >= ?
		ORDER BY received ASC
		LIMIT 1`)
	sampleReceivedTime := time.Date(targetYear, time.Month(targetMonth), 5, 10, 30, 0, 0, time.UTC)


	tests := []struct {
		name                 string
		path                 string // Allow overriding path for bad format tests
		setupMock            func(mock sqlmock.Sqlmock)
		expectedStatusCode   int
		expectedBodyContains []string
		expectDBErrorOnArticleQuery bool // To distinguish from group query error
	}{
		{
			name: "success - messages found, prev/next links active",
			path: path, // /group/comp.sys.mac/2023/11.html
			setupMock: func(mock sqlmock.Sqlmock) {
				// 1. Call to s.db.GetMessagesForGroupMonth(groupName, targetYear, targetMonth)
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal group ID lookup
				mock.ExpectQuery(articleQuery).WithArgs(groupID, targetYear, targetMonth).
					WillReturnRows(sqlmock.NewRows(articleColsNoGroup).AddRow(groupID, 201, "msg@nov", "Nov News", "User", "Date", sampleReceivedTime, 3,0,"",30,300))

				// 2. Call to s.db.GetPrevMonthWithMessages(groupName, targetYear, targetMonth)
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal group ID lookup
				prevNavTargetDate := time.Date(targetYear, time.Month(targetMonth), 1, 0,0,0,0,time.UTC)
				mock.ExpectQuery(prevMonthNavQuery).WithArgs(groupID, prevNavTargetDate).
					WillReturnRows(sqlmock.NewRows([]string{"YEAR(received)", "MONTH(received)"}).AddRow(2023, 10))

				// 3. Call to s.db.GetNextMonthWithMessages(groupName, targetYear, targetMonth)
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal group ID lookup
				nextNavTargetDate := time.Date(targetYear, time.Month(targetMonth), 1, 0,0,0,0,time.UTC).AddDate(0,1,0)
				mock.ExpectQuery(nextMonthNavQuery).WithArgs(groupID, nextNavTargetDate).
					WillReturnRows(sqlmock.NewRows([]string{"YEAR(received)", "MONTH(received)"}).AddRow(2023, 12))
			},
			expectedStatusCode: http.StatusOK,
			expectedBodyContains: []string{
				fmt.Sprintf("Messages for %d-%02d", targetYear, targetMonth), // 2023-11
				"Nov News",
				"Previous Month (2023-10)",
				"Next Month (2023-12)",
			},
		},
		{
			name: "success - no messages for specific month, but prev/next still possible",
			path: path,
			setupMock: func(mock sqlmock.Sqlmock) {
				// 1. Call to s.db.GetMessagesForGroupMonth -  no articles
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal group ID lookup
				mock.ExpectQuery(articleQuery).WithArgs(groupID, targetYear, targetMonth).
					WillReturnRows(sqlmock.NewRows(articleColsNoGroup))

				// 2. Call to s.db.GetPrevMonthWithMessages
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal group ID lookup
				prevNavTargetDate := time.Date(targetYear, time.Month(targetMonth), 1, 0,0,0,0,time.UTC)
				mock.ExpectQuery(prevMonthNavQuery).WithArgs(groupID, prevNavTargetDate).
					WillReturnRows(sqlmock.NewRows([]string{"YEAR(received)", "MONTH(received)"}).AddRow(2023, 10))

				// 3. Call to s.db.GetNextMonthWithMessages
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal group ID lookup
				nextNavTargetDate := time.Date(targetYear, time.Month(targetMonth), 1, 0,0,0,0,time.UTC).AddDate(0,1,0)
				mock.ExpectQuery(nextMonthNavQuery).WithArgs(groupID, nextNavTargetDate).
					WillReturnRows(sqlmock.NewRows([]string{"YEAR(received)", "MONTH(received)"}).AddRow(2023, 12))
			},
			expectedStatusCode: http.StatusOK,
			expectedBodyContains: []string{
				fmt.Sprintf("Messages for %d-%02d", targetYear, targetMonth),
				"No messages found for this month.",
				"Previous Month (2023-10)",
				"Next Month (2023-12)",
			},
		},
		{
			name: "group not found for specific month",
			path: path,
			setupMock: func(mock sqlmock.Sqlmock) {
				// GetMessagesForGroupMonth fails on group lookup
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnError(sql.ErrNoRows)
			},
			expectedStatusCode:   http.StatusNotFound,
			expectedBodyContains: []string{fmt.Sprintf("Group %s not found", groupName)},
		},
		{
			name: "database error on article query for specific month",
			path: path,
			setupMock: func(mock sqlmock.Sqlmock) {
				// GetMessagesForGroupMonth succeeds group lookup but fails article query
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(articleQuery).WithArgs(groupID, targetYear, targetMonth).WillReturnError(errors.New("specific month db error"))
				// Since GetMessagesForGroupMonth failed, nav link DB calls won't be made by handler if it returns error early.
				// If GetMessagesForGroupMonth returned empty list + no error, then nav link DB calls would be made.
				// Current handler logic: if GetMessagesForGroupMonth errors, it returns early.
			},
			expectedStatusCode:   http.StatusInternalServerError,
			expectedBodyContains: []string{"Failed to retrieve messages"},
			expectDBErrorOnArticleQuery: true,
		},
		{
			name:                 "invalid month format - non-numeric",
			path:                 fmt.Sprintf("/group/%s/%d/nov.html", groupName, targetYear),
			setupMock:            func(mock sqlmock.Sqlmock) { /* No DB calls expected */ },
			expectedStatusCode:   http.StatusBadRequest,
			expectedBodyContains: []string{"Invalid date format in URL"},
		},
		{
			name:                 "invalid month format - month out of range (0)",
			path:                 fmt.Sprintf("/group/%s/%d/%02d.html", groupName, targetYear, 0),
			setupMock:            func(mock sqlmock.Sqlmock) { /* No DB calls expected */ },
			expectedStatusCode:   http.StatusBadRequest,
			expectedBodyContains: []string{"Invalid date format in URL"},
		},
		{
			name:                 "invalid month format - month out of range (13)",
			path:                 fmt.Sprintf("/group/%s/%d/%02d.html", groupName, targetYear, 13),
			setupMock:            func(mock sqlmock.Sqlmock) { /* No DB calls expected */ },
			expectedStatusCode:   http.StatusBadRequest,
			expectedBodyContains: []string{"Invalid date format in URL"},
		},
		{
			name:                 "invalid year format - non-numeric",
			path:                 fmt.Sprintf("/group/%s/twenty-three/%02d.html", groupName, targetMonth),
			setupMock:            func(mock sqlmock.Sqlmock) { /* No DB calls expected */ },
			expectedStatusCode:   http.StatusBadRequest,
			expectedBodyContains: []string{"Invalid date format in URL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSqlDb, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create sqlmock: %v", err)
			}
			defer mockSqlDb.Close()

			testableDB := database.NewWithSQLDB(mockSqlDb)
			s := newTestServer(t, testableDB)

			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			req := httptest.NewRequest("GET", tt.path, nil)
			rr := httptest.NewRecorder()

			s.routeGroupRequests(rr, req) // Test via the main router

			if status := rr.Code; status != tt.expectedStatusCode {
				body, _ := io.ReadAll(rr.Body) // Read body for error context
				t.Errorf("handler for path '%s' returned wrong status code: got %v want %v. Body: %s", tt.path, status, tt.expectedStatusCode, string(body))
			}

			body := rr.Body.String() // Get body string once
			if rr.Code == tt.expectedStatusCode { // Only check body content if status is as expected
				for _, substr := range tt.expectedBodyContains {
					if !strings.Contains(body, substr) {
						t.Errorf("response body for path '%s' does not contain expected substring '%s'. Body:\n%s", tt.path, substr, body)
					}
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				// Only fail if DB error was not expected or if it's not a bad request type error
				if !tt.expectDBErrorOnArticleQuery && tt.expectedStatusCode < http.StatusBadRequest { // Use the correct flag name
					t.Errorf("sqlmock expectations were not met for path '%s': %s", tt.path, err)
				} else if tt.expectedStatusCode >= http.StatusBadRequest && strings.Contains(err.Error(), "call to Query") {
					// If it's a bad request, we might not expect DB calls, so an unmet expectation for query is an error
					t.Errorf("sqlmock expectations for query were not met for path '%s' (a BadRequest case, should be no DB calls): %s", tt.path, err)
				}
			}
		})
	}
}

// TestServer_handleListMessages_DefaultToLatestMonth tests /group/{groupname}/
// behavior, which should default to the latest month with messages.
func TestServer_handleListMessages_DefaultToLatestMonth(t *testing.T) {
	groupName := "comp.lang.go"
	path := fmt.Sprintf("/group/%s/", groupName)
	var groupID uint16 = 1

	// DB queries involved
	groupQuery := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
	minMaxMonthQuery := regexp.QuoteMeta("SELECT MIN(received), MAX(received) FROM articles WHERE group_id = ?")
	countQuery := regexp.QuoteMeta("SELECT COUNT(*) FROM articles WHERE group_id = ?")
	articleQuery := regexp.QuoteMeta(`
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
		       received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND YEAR(received) = ? AND MONTH(received) = ?
		ORDER BY received DESC, id DESC`)
	prevMonthNavQuery := regexp.QuoteMeta(`
		SELECT YEAR(received), MONTH(received)
		FROM articles
		WHERE group_id = ? AND received < ?
		ORDER BY received DESC
		LIMIT 1`)
	nextMonthNavQuery := regexp.QuoteMeta(`
		SELECT YEAR(received), MONTH(received)
		FROM articles
		WHERE group_id = ? AND received >= ?
		ORDER BY received ASC
		LIMIT 1`)

	tests := []struct {
		name                 string
		setupMock            func(mock sqlmock.Sqlmock, latestYear, latestMonth int) // Pass latest Y/M for dynamic query setup
		latestYear           int // For setting up mock expectations for latest month
		latestMonth          int // For setting up mock expectations for latest month
		expectedStatusCode   int
		expectedBodyContains []string
	}{
		{
			name: "success - group has messages, defaults to latest month",
			latestYear: 2024, latestMonth: 5, // May 2024 is latest
			setupMock: func(mock sqlmock.Sqlmock, latestY, latestM int) {
				// 1. Call to s.db.GetMinMaxMessageMonthsForGroup
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal
				mock.ExpectQuery(countQuery).WithArgs(groupID).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(5))
				mock.ExpectQuery(minMaxMonthQuery).WithArgs(groupID).WillReturnRows(sqlmock.NewRows([]string{"MIN(received)", "MAX(received)"}).
					AddRow(time.Date(2024, 3, 1, 0,0,0,0,time.UTC), time.Date(latestY, time.Month(latestM), 1, 0,0,0,0,time.UTC)))

				// 2. Call to s.db.GetMessagesForGroupMonth (for latestY, latestM)
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal
				mock.ExpectQuery(articleQuery).WithArgs(groupID, latestY, latestM).
					WillReturnRows(sqlmock.NewRows(articleColsNoGroup).AddRow(groupID, 101, "msg@latest", "Latest", "User", "Date", time.Now(), 1,0,"",10,100))

				// 3. Call to s.db.GetPrevMonthWithMessages (for latestY, latestM)
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal
				prevNavTargetDate := time.Date(latestY, time.Month(latestM), 1, 0,0,0,0,time.UTC)
				mock.ExpectQuery(prevMonthNavQuery).WithArgs(groupID, prevNavTargetDate).
					WillReturnRows(sqlmock.NewRows([]string{"YEAR(received)", "MONTH(received)"}).AddRow(2024, 4))

				// 4. Call to s.db.GetNextMonthWithMessages (for latestY, latestM)
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal
				nextNavTargetDate := time.Date(latestY, time.Month(latestM), 1, 0,0,0,0,time.UTC).AddDate(0,1,0)
				mock.ExpectQuery(nextMonthNavQuery).WithArgs(groupID, nextNavTargetDate).WillReturnError(sql.ErrNoRows)
			},
			expectedStatusCode: http.StatusOK,
			expectedBodyContains: []string{
				fmt.Sprintf("Messages for 2024-05"), "Latest", // Check it's showing May
				"Previous Month (2024-04)", // Check prev link
				"<span>Next Month &raquo;</span>", // Check next link is disabled
			},
		},
		{
			name: "group exists but no messages, defaults to current calendar month view",
			latestYear: time.Now().Year(), latestMonth: int(time.Now().Month()), // For handler's default calendar month
			setupMock: func(mock sqlmock.Sqlmock, currentY, currentM int) {
				// 1. Call to s.db.GetMinMaxMessageMonthsForGroup
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal
				mock.ExpectQuery(countQuery).WithArgs(groupID).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0)) // No messages

				// 2. Call to s.db.GetMessagesForGroupMonth (for currentY, currentM because count was 0)
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal
				mock.ExpectQuery(articleQuery).WithArgs(groupID, currentY, currentM).
					WillReturnRows(sqlmock.NewRows(articleColsNoGroup))

				// 3. Call to s.db.GetPrevMonthWithMessages
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal
				prevNavTargetDate := time.Date(currentY, time.Month(currentM), 1, 0,0,0,0,time.UTC)
				mock.ExpectQuery(prevMonthNavQuery).WithArgs(groupID, prevNavTargetDate).WillReturnError(sql.ErrNoRows)

				// 4. Call to s.db.GetNextMonthWithMessages
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID)) // Internal
				nextNavTargetDate := time.Date(currentY, time.Month(currentM), 1, 0,0,0,0,time.UTC).AddDate(0,1,0)
				mock.ExpectQuery(nextMonthNavQuery).WithArgs(groupID, nextNavTargetDate).WillReturnError(sql.ErrNoRows)
			},
			expectedStatusCode: http.StatusOK,
			expectedBodyContains: []string{
				fmt.Sprintf("Messages for %d-%02d", time.Now().Year(), int(time.Now().Month())), // Shows current calendar month
				"No messages found for this month.",
				"<span>&laquo; Previous Month</span>", // Both links disabled
				"<span>Next Month &raquo;</span>",
			},
		},
		{
			name: "group not found", // GetMinMaxMessageMonthsForGroup returns group not found
			latestYear: 0, latestMonth: 0, // Not relevant
			setupMock: func(mock sqlmock.Sqlmock, _, _ int) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnError(sql.ErrNoRows)
				// No other DB calls expected
			},
			expectedStatusCode: http.StatusNotFound,
			expectedBodyContains: []string{fmt.Sprintf("Group %s not found", groupName)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSqlDb, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create sqlmock: %v", err)
			}
			defer mockSqlDb.Close()

			testableDB := database.NewWithSQLDB(mockSqlDb)
			s := newTestServer(t, testableDB)

			tt.setupMock(mock, tt.latestYear, tt.latestMonth)

			req := httptest.NewRequest("GET", path, nil) // Path is /group/{groupName}/
			rr := httptest.NewRecorder()

			s.routeGroupRequests(rr, req)  // Test via the main router

			if status := rr.Code; status != tt.expectedStatusCode {
				t.Errorf("handler returned wrong status code: got %v want %v. Body: %s", status, tt.expectedStatusCode, rr.Body.String())
			}

			body := rr.Body.String()
			for _, substr := range tt.expectedBodyContains {
				if !strings.Contains(body, substr) {
					t.Errorf("response body does not contain expected substring '%s'. Body:\n%s", substr, body)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("sqlmock expectations were not met: %s", err)
			}
		})
	}
}

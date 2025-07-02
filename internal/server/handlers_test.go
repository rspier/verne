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
			expectedBodyContains: []string{"Test Subject", "<strong>From:</strong> Test From", "<strong>Message-ID:</strong> <code>&lt;article123@example.com&gt;</code>", "No other messages found in this thread."},
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
	groupQueryBase := "SELECT id FROM `groups` WHERE name = ?"
	articleQueryBase := `
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
		       received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND YEAR(received) = ? AND MONTH(received) = ?
		ORDER BY received DESC, id DESC`


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
				mock.ExpectQuery(regexp.QuoteMeta(expectedListGroupsSQLQuery)).WillReturnRows(rows)
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       []string{"alt.test", "Go Language", `<a href="/group/alt.test/">`},
		},
		{
			name: "dispatch to list groups - database error",
			path: "/group/",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(expectedListGroupsSQLQuery)).WillReturnError(errors.New("db error for list groups"))
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedBody:       []string{"Failed to retrieve newsgroups"},
		},
		{
			name: "dispatch to list messages (current month) - group found, no messages",
			path: "/group/comp.test/", // Trailing slash indicates current month for this group
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(groupQueryBase)).WithArgs("comp.test").
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1)) // group_id = 1
				mock.ExpectQuery(regexp.QuoteMeta(articleQueryBase)).
					WithArgs(1, time.Now().Year(), int(time.Now().Month())).
					WillReturnRows(sqlmock.NewRows([]string{"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date", "received", "thread_id", "parent", "h_references", "h_lines", "h_bytes"}))
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       []string{"Messages for", "comp.test", "No messages found for this month."},
		},
		{
			name: "dispatch to list messages - group not found",
			path: "/group/unknown.group/",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(groupQueryBase)).WithArgs("unknown.group").
					WillReturnError(sql.ErrNoRows)
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
	sampleReceivedTime := time.Date(targetYear, time.Month(targetMonth), 5, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name                 string
		path                 string // Allow overriding path for bad format tests
		setupMock            func(mock sqlmock.Sqlmock)
		expectedStatusCode   int
		expectedBodyContains []string
		expectDBError        bool
	}{
		{
			name: "success - messages found for specific month",
			path: path,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				articleRows := sqlmock.NewRows([]string{
					"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date",
					"received", "thread_id", "parent", "h_references", "h_lines", "h_bytes",
				}).AddRow(groupID, 201, "msgABC@example.com", "Old News", "Old Timer", "Yesterday", sampleReceivedTime, 3, 0, "", 30, 300)
				mock.ExpectQuery(articleQuery).WithArgs(groupID, targetYear, targetMonth).WillReturnRows(articleRows)
			},
			expectedStatusCode: http.StatusOK,
			expectedBodyContains: []string{
				fmt.Sprintf("Messages for %d-%02d", targetYear, targetMonth),
				"Old News", "Old Timer",
				fmt.Sprintf("/group/%s/%d/%02d/msg201.html", groupName, targetYear, targetMonth),
				fmt.Sprintf("/group/%s/%d/%02d.html", groupName, 2023, 10), // Prev month link (Oct)
				fmt.Sprintf("/group/%s/%d/%02d.html", groupName, 2023, 12), // Next month link (Dec)
			},
		},
		{
			name: "success - no messages for specific month",
			path: path,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				emptyArticleRows := sqlmock.NewRows([]string{
					"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date",
					"received", "thread_id", "parent", "h_references", "h_lines", "h_bytes",
				})
				mock.ExpectQuery(articleQuery).WithArgs(groupID, targetYear, targetMonth).WillReturnRows(emptyArticleRows)
			},
			expectedStatusCode: http.StatusOK,
			expectedBodyContains: []string{
				fmt.Sprintf("Messages for %d-%02d", targetYear, targetMonth),
				"No messages found for this month.",
			},
		},
		{
			name: "group not found for specific month",
			path: path,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnError(sql.ErrNoRows)
			},
			expectedStatusCode:   http.StatusNotFound,
			expectedBodyContains: []string{fmt.Sprintf("Group %s not found", groupName)},
		},
		{
			name: "database error on article query for specific month",
			path: path,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(articleQuery).WithArgs(groupID, targetYear, targetMonth).WillReturnError(errors.New("specific month db error"))
			},
			expectedStatusCode:   http.StatusInternalServerError,
			expectedBodyContains: []string{"Failed to retrieve messages"},
			expectDBError:        true,
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
				if !tt.expectDBError && tt.expectedStatusCode < http.StatusBadRequest {
					t.Errorf("sqlmock expectations were not met for path '%s': %s", tt.path, err)
				} else if tt.expectedStatusCode >= http.StatusBadRequest && strings.Contains(err.Error(), "call to Query") {
					// If it's a bad request, we might not expect DB calls, so an unmet expectation for query is an error
					t.Errorf("sqlmock expectations for query were not met for path '%s' (a BadRequest case, should be no DB calls): %s", tt.path, err)
				}
			}
		})
	}
}

// TestServer_handleListMessages_CurrentMonth specifically tests the current month logic
// for /group/{groupname}/
func TestServer_handleListMessages_CurrentMonth(t *testing.T) {
	groupName := "comp.lang.go"
	path := fmt.Sprintf("/group/%s/", groupName) // Path for routeGroupRequests to dispatch to handleListMessages
	now := time.Now()
	currentYear, currentMonth := now.Year(), int(now.Month())
	var groupID uint16 = 1

	groupQuery := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
	articleQuery := regexp.QuoteMeta(`
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
		       received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND YEAR(received) = ? AND MONTH(received) = ?
		ORDER BY received DESC, id DESC`)
	sampleReceivedTime := time.Date(currentYear, time.Month(currentMonth), 1, 12, 0, 0, 0, time.UTC)


	tests := []struct {
		name               string
		setupMock          func(mock sqlmock.Sqlmock)
		expectedStatusCode int
		expectedBodyContains []string
	}{
		{
			name: "success - messages found for current month",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				articleRows := sqlmock.NewRows([]string{
					"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date",
					"received", "thread_id", "parent", "h_references", "h_lines", "h_bytes",
				}).AddRow(groupID, 101, "msg1@example.com", "Hello Go", "Go Gopher", "Today", sampleReceivedTime, 1, 0, "", 20, 200)
				mock.ExpectQuery(articleQuery).WithArgs(groupID, currentYear, currentMonth).WillReturnRows(articleRows)
			},
			expectedStatusCode: http.StatusOK,
			expectedBodyContains: []string{
				fmt.Sprintf("Messages for %d-%02d", currentYear, currentMonth),
				"Hello Go", "Go Gopher",
				// Link uses current year/month from data passed to template
				fmt.Sprintf("/group/%s/%d/%02d/msg101.html", groupName, currentYear, currentMonth),
			},
		},
		{
			name: "success - no messages for current month",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				emptyArticleRows := sqlmock.NewRows([]string{
					"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date",
					"received", "thread_id", "parent", "h_references", "h_lines", "h_bytes",
				})
				mock.ExpectQuery(articleQuery).WithArgs(groupID, currentYear, currentMonth).WillReturnRows(emptyArticleRows)
			},
			expectedStatusCode: http.StatusOK,
			expectedBodyContains: []string{
				fmt.Sprintf("Messages for %d-%02d", currentYear, currentMonth),
				"No messages found for this month.",
			},
		},
		{
			name: "group not found for current month list",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnError(sql.ErrNoRows)
			},
			expectedStatusCode: http.StatusNotFound,
			expectedBodyContains: []string{fmt.Sprintf("Group %s not found", groupName)},
		},
		{
			 name: "database error on article query for current month",
			 setupMock: func(mock sqlmock.Sqlmock) {
				 mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				 mock.ExpectQuery(articleQuery).WithArgs(groupID, currentYear, currentMonth).WillReturnError(errors.New("article db error"))
			 },
			 expectedStatusCode: http.StatusInternalServerError,
			 expectedBodyContains: []string{"Failed to retrieve messages"},
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

			tt.setupMock(mock)

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

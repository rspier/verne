package server

import (
	"database/sql" // For sql.ErrNoRows
	"errors"       // For dict func in newTestServer & other error handling
	"fmt"
	htmltemplate "html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	// Project specific imports
	"nntp-web/internal/config"
	"nntp-web/internal/database"
	"nntp-web/internal/nntpclient"
	"nntp-web/web/templates"

	// External test libraries
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockNNTPClient is a mock implementation of nntpclient.NNTPClientInterface
type MockNNTPClient struct {
	mock.Mock
}

func (m *MockNNTPClient) FetchRawArticle(groupName string, articleNum uint32) ([]byte, error) {
	args := m.Called(groupName, articleNum)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockNNTPClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

var articleColsNoGroup = []string{"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date", "received", "thread_id", "parent", "h_references", "h_lines", "h_bytes"}
var articleColsWithGroup = append(articleColsNoGroup, "group_name")

func newTestServer(t *testing.T, mockDb *database.DB, mockNntp nntpclient.NNTPClientInterface) *Server {
	t.Helper()
	templateFuncs := htmltemplate.FuncMap{
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, errors.New("dict expects an even number of arguments for key-value pairs")
			}
			m := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, errors.New("dict keys must be strings")
				}
				m[key] = values[i+1]
			}
			return m, nil
		},
	}
	parsedTemplates, err := htmltemplate.New("test").Funcs(templateFuncs).ParseFS(templates.Files, "*.html.tmpl")
	if err != nil {
		t.Fatalf("Failed to parse templates: %v", err)
	}
	cfg := &config.Config{ServerPort: 8080, CacheTTLSeconds: 1} // Example config
	return &Server{
		config:     cfg,
		db:         mockDb,
		nntpClient: mockNntp,
		router:     http.NewServeMux(),
		templates:  parsedTemplates,
	}
}

func TestServer_handleShowArticle(t *testing.T) {
	groupName := "test.group"
	var groupID uint16 = 1
	year, month, day := 2024, 1, 15
	var articleNum uint32 = 789
	msgID := fmt.Sprintf("article%d@example.com", articleNum)
	threadID := uint32(456)
	sampleReceived := time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.UTC)
	mockArticleDateStr := sampleReceived.Format(time.RFC1123Z)


	groupQueryRgx := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
	articleDetailsQueryRgx := regexp.QuoteMeta("SELECT group_id, id, h_messageid, h_subject, h_from, h_date, received, thread_id, parent, h_references, h_lines, h_bytes FROM articles WHERE group_id = ? AND id = ? AND YEAR(received) = ? AND MONTH(received) = ? LIMIT 1")
	relaxedArticleDetailsQueryRgx := regexp.QuoteMeta("SELECT group_id, id, h_messageid, h_subject, h_from, h_date, received, thread_id, parent, h_references, h_lines, h_bytes FROM articles WHERE group_id = ? AND id = ? LIMIT 1")
	articleByMsgIDQueryRgx := regexp.QuoteMeta("SELECT a.group_id, a.id, a.h_messageid, a.h_subject, a.h_from, a.h_date, a.received, a.thread_id, a.parent, a.h_references, a.h_lines, a.h_bytes, g.name as group_name FROM articles a JOIN `groups` g ON a.group_id = g.id WHERE a.h_messageid = ? LIMIT 1")
	// Updated threadMessagesQueryRgx to also filter by group_id
	threadMessagesQueryRgx := regexp.QuoteMeta("SELECT a.group_id, a.id, a.h_messageid, a.h_subject, a.h_from, a.h_date, a.received, a.thread_id, a.parent, a.h_references, a.h_lines, a.h_bytes, g.name as group_name FROM articles a JOIN `groups` g ON a.group_id = g.id WHERE a.thread_id = ? AND a.group_id = ? ORDER BY a.received ASC, a.id ASC")

	tests := []struct {
		name                 string
		path                 string
		setupDbMockFn        func(dbMock sqlmock.Sqlmock)
		setupNntpMockFn      func(nntpMock *MockNNTPClient)
		expectedStatusCode   int
		expectedLocation     string
		expectedBodyContains []string
	}{
		{
			name: "canonical path - success",
			path: fmt.Sprintf("/group/%s/%d/%02d/msg%d.html", groupName, year, month, articleNum),
			setupDbMockFn: func(dbMock sqlmock.Sqlmock) {
				dbMock.ExpectQuery(groupQueryRgx).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				dbMock.ExpectQuery(articleDetailsQueryRgx).WithArgs(groupID, articleNum, year, month).
					WillReturnRows(sqlmock.NewRows(articleColsNoGroup).
						AddRow(groupID, articleNum, msgID, "Test Subject", "Test From", mockArticleDateStr, sampleReceived, threadID, 0, "", 10, 100))
				// Expect query for thread messages to use threadID and groupID
				dbMock.ExpectQuery(threadMessagesQueryRgx).WithArgs(threadID, groupID).
					WillReturnRows(sqlmock.NewRows(articleColsWithGroup)) // Mock returns no actual "other" thread messages for this test.
			},
			setupNntpMockFn: func(nntpMock *MockNNTPClient) {
				nntpMock.On("FetchRawArticle", groupName, articleNum).Return([]byte("Article: Test Subject\n\nBody."), nil).Once()
			},
			expectedStatusCode: http.StatusOK,
			expectedBodyContains: []string{"Test Subject", "<strong>From:</strong> Test From"},
		},
		{
			name: "canonical path - date mismatch redirect",
			path: fmt.Sprintf("/group/%s/%d/%02d/msg%d.html", groupName, 2023, 12, articleNum),
			setupDbMockFn: func(dbMock sqlmock.Sqlmock) {
				dbMock.ExpectQuery(groupQueryRgx).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				dbMock.ExpectQuery(articleDetailsQueryRgx).WithArgs(groupID, articleNum, 2023, 12).WillReturnError(sql.ErrNoRows)
				dbMock.ExpectQuery(relaxedArticleDetailsQueryRgx).WithArgs(groupID, articleNum).
					WillReturnRows(sqlmock.NewRows(articleColsNoGroup).
						AddRow(groupID, articleNum, msgID, "Test Subject", "Test From", mockArticleDateStr, sampleReceived, threadID, 0, "", 10, 100))
			},
			setupNntpMockFn:      func(nntpMock *MockNNTPClient) { /* No NNTP call expected */ },
			expectedStatusCode:   http.StatusFound,
			expectedLocation:     fmt.Sprintf("/group/%s/%d/%02d/msg%d.html", groupName, year, month, articleNum),
		},
		{
			name: "msgid lookup - success and redirect",
			path: fmt.Sprintf("/group/%s/?msgid=%s", groupName, msgID), // msgID is raw here
			setupDbMockFn: func(dbMock sqlmock.Sqlmock) {
				// Expect GetArticleByMessageID to be called with the Message-ID wrapped in angle brackets
				expectedMsgIDWithBrackets := fmt.Sprintf("<%s>", msgID)
				dbMock.ExpectQuery(articleByMsgIDQueryRgx).WithArgs(expectedMsgIDWithBrackets).
					WillReturnRows(sqlmock.NewRows(articleColsWithGroup).
						// The h_messageid column in DB stores it with brackets
						AddRow(groupID, articleNum, expectedMsgIDWithBrackets, "Test Subject", "Test From", mockArticleDateStr, sampleReceived, threadID, 0, "", 10, 100, groupName))
			},
			setupNntpMockFn:      func(nntpMock *MockNNTPClient) { /* No NNTP call */ },
			expectedStatusCode:   http.StatusFound,
			expectedLocation:     fmt.Sprintf("/group/%s/%d/%02d/msg%d.html", groupName, year, month, articleNum),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSqlDb, dbMock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create sqlmock: %v", err)
			}
			defer mockSqlDb.Close()

			// Pass nil for config to NewWithSQLDB
			testableDB := database.NewWithSQLDB(mockSqlDb, nil)
			nntpMock := new(MockNNTPClient)

			s := newTestServer(t, testableDB, nntpMock)

			if tt.setupDbMockFn != nil {
				tt.setupDbMockFn(dbMock)
			}
			if tt.setupNntpMockFn != nil {
				tt.setupNntpMockFn(nntpMock)
			}

			req := httptest.NewRequest("GET", tt.path, nil)
			rr := httptest.NewRecorder()
			s.routeGroupRequests(rr, req)

			if status := rr.Code; status != tt.expectedStatusCode {
				bodyBytes, _ := io.ReadAll(rr.Body)
				t.Errorf("handler for path '%s' returned wrong status code: got %v want %v. Body: %s", tt.path, status, tt.expectedStatusCode, string(bodyBytes))
			}

			if tt.expectedLocation != "" {
				loc, errLoc := rr.Result().Location()
				if errLoc != nil {
					t.Errorf("Expected redirect for path '%s', but got error retrieving location: %v", tt.path, errLoc)
				} else if loc.String() != tt.expectedLocation {
					t.Errorf("handler for path '%s' redirected to wrong location: got %s want %s", tt.path, loc.String(), tt.expectedLocation)
				}
			}

			body := rr.Body.String()
			for _, substr := range tt.expectedBodyContains {
				if !strings.Contains(body, substr) {
					t.Errorf("response body for path '%s' does not contain expected substring '%s'. Body:\n%s", tt.path, substr, body)
				}
			}

			assert.NoError(t, dbMock.ExpectationsWereMet(), "DB expectations not met for path %s", tt.path)
			nntpMock.AssertExpectations(t)
		})
	}
}

func TestServer_RouteGroupRequests(t *testing.T) {
	// Updated query to match the one in db.GetAllNewsgroups
	expectedListGroupsSQLQuery := regexp.QuoteMeta(`
		SELECT
			g.id, g.name, g.description,
			last_post.max_received_date
		FROM
			` + "`groups`" + ` g
		LEFT JOIN
			(SELECT group_id, MAX(received) as max_received_date FROM articles GROUP BY group_id) last_post
		ON
			g.id = last_post.group_id
		ORDER BY
			g.name`)

	tests := []struct {
		name               string
		path               string
		setupDbMockFn      func(dbMock sqlmock.Sqlmock)
		expectedStatusCode int
		expectedBody       []string
		notExpectedBody    []string
	}{
		{
			name: "dispatch to list groups - success",
			path: "/group/",
			setupDbMockFn: func(dbMock sqlmock.Sqlmock) {
				// Rows now need to include max_received_date (can be nil for sql.NullTime)
				rows := sqlmock.NewRows([]string{"id", "name", "description", "max_received_date"}).
					AddRow(1, "alt.test", "Test group", nil). // Example with nil last post date
					AddRow(2, "comp.lang.go", "Go Language", time.Now()) // Example with a last post date
				dbMock.ExpectQuery(expectedListGroupsSQLQuery).WillReturnRows(rows)
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       []string{"alt.test", "Go Language"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSqlDb, dbMock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create sqlmock: %v", err)
			}
			defer mockSqlDb.Close()

			testableDB := database.NewWithSQLDB(mockSqlDb, nil) // Pass nil for config
			nntpMock := new(MockNNTPClient)
			s := newTestServer(t, testableDB, nntpMock)

			if tt.setupDbMockFn != nil {
				tt.setupDbMockFn(dbMock)
			}

			req := httptest.NewRequest("GET", tt.path, nil)
			rr := httptest.NewRecorder()
			s.routeGroupRequests(rr, req)

			if rr.Code != tt.expectedStatusCode {
				t.Errorf("routeGroupRequests with path '%s' returned wrong status code: got %v want %v. Body: %s", tt.path, rr.Code, tt.expectedStatusCode, rr.Body.String())
			}
			bodyString := rr.Body.String()
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
			assert.NoError(t, dbMock.ExpectationsWereMet(), "DB expectations not met for path %s", tt.path)
			nntpMock.AssertExpectations(t)
		})
	}
}


func TestServer_handleListMessages_SpecificMonth(t *testing.T) {
	groupName := "comp.sys.mac"
	targetYear, targetMonth := 2023, 11

	tests := []struct {
		name                 string
		path                 string
		setupDbMockFn        func(dbMock sqlmock.Sqlmock)
		expectedStatusCode   int
		expectedBodyContains []string
	}{
		{
			name: "success - messages found",
			path: fmt.Sprintf("/group/%s/%d/%02d.html", groupName, targetYear, targetMonth),
			setupDbMockFn: func(dbMock sqlmock.Sqlmock) {
				var groupID uint16 = 1
				groupQueryRgx := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
				articleQueryRgx := regexp.QuoteMeta("SELECT group_id, id, h_messageid, h_subject, h_from, h_date, received, thread_id, parent, h_references, h_lines, h_bytes FROM articles WHERE group_id = ? AND YEAR(received) = ? AND MONTH(received) = ? ORDER BY received DESC, id DESC")
				prevMonthNavQueryRgx := regexp.QuoteMeta("SELECT YEAR(received), MONTH(received) FROM articles WHERE group_id = ? AND received < ? ORDER BY received DESC LIMIT 1")
				nextMonthNavQueryRgx := regexp.QuoteMeta("SELECT YEAR(received), MONTH(received) FROM articles WHERE group_id = ? AND received >= ? ORDER BY received ASC LIMIT 1")

				dbMock.ExpectQuery(groupQueryRgx).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mockArticleDate := time.Now().Format(time.RFC1123Z)
				dbMock.ExpectQuery(articleQueryRgx).WithArgs(groupID, targetYear, targetMonth).
					WillReturnRows(sqlmock.NewRows(articleColsNoGroup).AddRow(groupID, 201, "msg@nov", "Nov News", "User", mockArticleDate, time.Now(), 3,0,"",30,300))
				dbMock.ExpectQuery(groupQueryRgx).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				dbMock.ExpectQuery(prevMonthNavQueryRgx).WithArgs(groupID, sqlmock.AnyArg()).WillReturnError(sql.ErrNoRows)
				dbMock.ExpectQuery(groupQueryRgx).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				dbMock.ExpectQuery(nextMonthNavQueryRgx).WithArgs(groupID, sqlmock.AnyArg()).WillReturnError(sql.ErrNoRows)
			},
			expectedStatusCode: http.StatusOK,
			// Be more specific with the "From" check to include the span, as that's what's rendered.
			expectedBodyContains: []string{">Nov News</a>", `From: <span class="message-from">User</span>`, "Thread: 1 message"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSqlDb, dbMock, err := sqlmock.New()
			if err != nil { t.Fatalf("Failed to create sqlmock: %v", err) }
			defer mockSqlDb.Close()
			testableDB := database.NewWithSQLDB(mockSqlDb, nil) // Pass nil for config
			nntpMock := new(MockNNTPClient)
			s := newTestServer(t, testableDB, nntpMock)
			if tt.setupDbMockFn != nil { tt.setupDbMockFn(dbMock) }
			req := httptest.NewRequest("GET", tt.path, nil)
			rr := httptest.NewRecorder()
			s.routeGroupRequests(rr, req)
			assert.Equal(t, tt.expectedStatusCode, rr.Code)
			for _, sub := range tt.expectedBodyContains { assert.Contains(t, rr.Body.String(), sub) }
			assert.NoError(t, dbMock.ExpectationsWereMet())
			nntpMock.AssertExpectations(t)
		})
	}
}

func TestServer_handleListMessages_JWZThreading(t *testing.T) {
	groupName := "jwz.test.group"
	targetYear, targetMonth := 2024, 4
	var groupID uint16 = 10
	path := fmt.Sprintf("/group/%s/%d/%02d.html", groupName, targetYear, targetMonth)
	time1 := time.Date(targetYear, time.Month(targetMonth), 1, 10, 0, 0, 0, time.UTC)
	// Simplified mock data for this specific test
	mockArticlesData := []struct { id uint32; msgID, subject, hDate, references string; received time.Time }{
		{1, "msg1@host.jwz", "Root Message 1", time1.Format(time.RFC1123Z), "", time1},
		{2, "msg2@host.jwz", "Re: Root Message 1", time1.Add(1*time.Hour).Format(time.RFC1123Z), "<msg1@host.jwz>", time1.Add(1*time.Hour)},
	}

	setupDbMockFn := func(dbMock sqlmock.Sqlmock) {
		groupQueryRgx := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
		articleQueryRgx := regexp.QuoteMeta("SELECT group_id, id, h_messageid, h_subject, h_from, h_date, received, thread_id, parent, h_references, h_lines, h_bytes FROM articles WHERE group_id = ? AND YEAR(received) = ? AND MONTH(received) = ? ORDER BY received DESC, id DESC")
		prevMonthNavQueryRgx := regexp.QuoteMeta("SELECT YEAR(received), MONTH(received) FROM articles WHERE group_id = ? AND received < ? ORDER BY received DESC LIMIT 1")
		nextMonthNavQueryRgx := regexp.QuoteMeta("SELECT YEAR(received), MONTH(received) FROM articles WHERE group_id = ? AND received >= ? ORDER BY received ASC LIMIT 1")

		dbMock.ExpectQuery(groupQueryRgx).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
		rows := sqlmock.NewRows(articleColsNoGroup)
		for _, ma := range mockArticlesData {
			rows.AddRow(groupID, ma.id, ma.msgID, ma.subject, "Test User", ma.hDate, ma.received, ma.id, 0, ma.references, 10, 100)
		}
		dbMock.ExpectQuery(articleQueryRgx).WithArgs(groupID, targetYear, targetMonth).WillReturnRows(rows)
		dbMock.ExpectQuery(groupQueryRgx).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
		dbMock.ExpectQuery(prevMonthNavQueryRgx).WithArgs(groupID, sqlmock.AnyArg()).WillReturnError(sql.ErrNoRows)
		dbMock.ExpectQuery(groupQueryRgx).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
		dbMock.ExpectQuery(nextMonthNavQueryRgx).WithArgs(groupID, sqlmock.AnyArg()).WillReturnError(sql.ErrNoRows)
	}

	mockSqlDb, dbMock, err := sqlmock.New()
	if err != nil { t.Fatalf("Failed to create sqlmock: %v", err) }
	defer mockSqlDb.Close()
	testableDB := database.NewWithSQLDB(mockSqlDb, nil) // Pass nil for config
	nntpMock := new(MockNNTPClient)
	s := newTestServer(t, testableDB, nntpMock)

	setupDbMockFn(dbMock)

	req := httptest.NewRequest("GET", path, nil)
	rr := httptest.NewRecorder()
	s.routeGroupRequests(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()

	// Check for the root message subject
	assert.Contains(t, body, ">Root Message 1</a>")
	// Check for the correct thread count
	assert.Contains(t, body, "Thread: 2 messages")

	// Ensure the child message's subject is NOT directly rendered as a list item
	assert.NotContains(t, body, ">Re: Root Message 1</a>")

	// Ensure old CSS classes for recursive display are gone
	assert.NotContains(t, body, "thread-level-0")
	assert.NotContains(t, body, "thread-level-1")
	assert.NotContains(t, body, "thread-children")


	assert.NoError(t, dbMock.ExpectationsWereMet())
	nntpMock.AssertExpectations(t)
}

func TestServer_handleListMessages_DefaultToLatestMonth(t *testing.T) {
	groupName := "comp.lang.go"
	tests := []struct {
		name                 string
		setupDbMockFn        func(dbMock sqlmock.Sqlmock, latestYear, latestMonth int)
		latestYear           int
		latestMonth          int
		expectedStatusCode   int
		expectedBodyContains []string
	}{
		{
			name: "success - group has messages, defaults to latest month",
			latestYear: 2024, latestMonth: 5,
			setupDbMockFn: func(dbMock sqlmock.Sqlmock, latestY, latestM int) {
				var groupID uint16 = 1
				groupQueryRgx := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
				articleQueryRgx := regexp.QuoteMeta("SELECT group_id, id, h_messageid, h_subject, h_from, h_date, received, thread_id, parent, h_references, h_lines, h_bytes FROM articles WHERE group_id = ? AND YEAR(received) = ? AND MONTH(received) = ? ORDER BY received DESC, id DESC")
				prevMonthNavQueryRgx := regexp.QuoteMeta("SELECT YEAR(received), MONTH(received) FROM articles WHERE group_id = ? AND received < ? ORDER BY received DESC LIMIT 1")
				nextMonthNavQueryRgx := regexp.QuoteMeta("SELECT YEAR(received), MONTH(received) FROM articles WHERE group_id = ? AND received >= ? ORDER BY received ASC LIMIT 1")
				latestMonthQueryRgx := regexp.QuoteMeta("SELECT YEAR(received), MONTH(received) FROM articles WHERE group_id = ? ORDER BY received DESC LIMIT 1")

				dbMock.ExpectQuery(groupQueryRgx).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				dbMock.ExpectQuery(latestMonthQueryRgx).WithArgs(groupID).WillReturnRows(sqlmock.NewRows([]string{"YEAR", "MONTH"}).AddRow(latestY, latestM))
				dbMock.ExpectQuery(groupQueryRgx).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mockArticleDate := time.Now().Format(time.RFC1123Z)
				dbMock.ExpectQuery(articleQueryRgx).WithArgs(groupID, latestY, latestM).
					WillReturnRows(sqlmock.NewRows(articleColsNoGroup).AddRow(groupID, 101, "msg@latest", "Latest", "User", mockArticleDate, time.Now(), 1,0,"",10,100))
				dbMock.ExpectQuery(groupQueryRgx).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				dbMock.ExpectQuery(prevMonthNavQueryRgx).WithArgs(groupID, sqlmock.AnyArg()).WillReturnError(sql.ErrNoRows)
				dbMock.ExpectQuery(groupQueryRgx).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				dbMock.ExpectQuery(nextMonthNavQueryRgx).WithArgs(groupID, sqlmock.AnyArg()).WillReturnError(sql.ErrNoRows)
			},
			expectedStatusCode: http.StatusOK,
			expectedBodyContains: []string{fmt.Sprintf("Messages for %d-%02d", 2024, 5)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSqlDb, dbMock, err := sqlmock.New()
			if err != nil { t.Fatalf("Failed to create sqlmock: %v", err) }
			defer mockSqlDb.Close()
			testableDB := database.NewWithSQLDB(mockSqlDb, nil) // Pass nil for config
			nntpMock := new(MockNNTPClient)
			s := newTestServer(t, testableDB, nntpMock)
			if tt.setupDbMockFn != nil { tt.setupDbMockFn(dbMock, tt.latestYear, tt.latestMonth) }
			req := httptest.NewRequest("GET", fmt.Sprintf("/group/%s/", groupName), nil)
			rr := httptest.NewRecorder()
			s.routeGroupRequests(rr, req)
			assert.Equal(t, tt.expectedStatusCode, rr.Code)
			for _, sub := range tt.expectedBodyContains { assert.Contains(t, rr.Body.String(), sub) }
			assert.NoError(t, dbMock.ExpectationsWereMet())
			nntpMock.AssertExpectations(t)
		})
	}
}

func init() {
	// Logs will go to os.Stderr by default if not set, or set explicitly.
	// Removing log.SetOutput(io.Discard) to enable log viewing for tests.
	// If "log" import becomes unused, it will be caught by compiler.
}

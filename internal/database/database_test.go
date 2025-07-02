package database

import (
	"database/sql" // Added for sql.ErrNoRows
	"database/sql/driver"
	"errors" // Added for errors.New
	"fmt"    // Added for fmt.Sprintf
	"reflect" // Added for reflect.DeepEqual
	"regexp"
	"strings" // Added for strings.Contains
	"testing"
	"time" // Added for time.Date

	// "nntp-web/internal/config" // Removed: unused after cfg was commented out
	"nntp-web/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDB_GetAllNewsgroups(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()

	// db := &DB{sqlDB: mockDB} // Inject mock DB into our DB struct
	db := NewWithSQLDB(mockDB) // Use the test constructor for consistency

	query := "SELECT id, name, description FROM `groups` ORDER BY name"

	tests := []struct {
		name          string
		mockRows      *sqlmock.Rows
		expected      []models.Newsgroup
		expectErr     bool
		mockExpectErr error
	}{
		{
			name: "success - multiple groups",
			mockRows: sqlmock.NewRows([]string{"id", "name", "description"}).
				AddRow(1, "alt.test", "Alternative test group").
				AddRow(2, "comp.sys.cbm", "Commodore systems"),
			expected: []models.Newsgroup{
				{ID: 1, Name: "alt.test", Description: "Alternative test group"},
				{ID: 2, Name: "comp.sys.cbm", Description: "Commodore systems"},
			},
			expectErr: false,
		},
		{
			name:     "success - no groups",
			mockRows: sqlmock.NewRows([]string{"id", "name", "description"}),
			expected: []models.Newsgroup{},
			expectErr: false,
		},
		{
			name:          "database query error",
			mockRows:      nil, // Not used
			expected:      nil,
			expectErr:     true,
			mockExpectErr: sqlmock.ErrCancelled, // Example error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mockExpectErr != nil {
				mock.ExpectQuery(regexp.QuoteMeta(query)).WillReturnError(tt.mockExpectErr)
			} else {
				mock.ExpectQuery(regexp.QuoteMeta(query)).WillReturnRows(tt.mockRows)
			}

			groups, err := db.GetAllNewsgroups()

			if (err != nil) != tt.expectErr {
				t.Errorf("GetAllNewsgroups() error = %v, expectErr %v", err, tt.expectErr)
				return
			}

			if !tt.expectErr {
				if len(groups) != len(tt.expected) {
					t.Errorf("GetAllNewsgroups() got %d groups, want %d", len(groups), len(tt.expected))
				}
				// Could do a deeper comparison if needed
				for i, g := range groups {
					if g.ID != tt.expected[i].ID || g.Name != tt.expected[i].Name || g.Description != tt.expected[i].Description {
						t.Errorf("GetAllNewsgroups() group mismatch at index %d. Got %+v, want %+v", i, g, tt.expected[i])
					}
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("there were unfulfilled expectations: %s", err)
			}
		})
	}
}

func TestDB_GetArticleByDetails(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()
	db := NewWithSQLDB(mockDB)

	groupName := "rec.arts.books"
	var groupID uint16 = 3
	year, month, articleNum := 2024, 1, uint32(101)
	sampleReceived := time.Date(year, time.Month(month), 10, 0, 0, 0, 0, time.UTC)

	groupQuery := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
	articleQuery := regexp.QuoteMeta(`
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
		       received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND id = ?
		  AND YEAR(received) = ? AND MONTH(received) = ?
		LIMIT 1`)
	relaxedArticleQuery := regexp.QuoteMeta(`
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
			   received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND id = ?
		LIMIT 1`)

	tests := []struct {
		name           string
		groupName      string
		year           int
		month          int
		articleNum     uint32
		setupMock      func(mock sqlmock.Sqlmock)
		expectArticle  *models.Article
		expectErr      bool
		expectErrMsg   string
		checkReceived  bool // whether to check the Received field (for date mismatch tests)
	}{
		{
			name:      "success - article found with correct date",
			groupName: groupName, year: year, month: month, articleNum: articleNum,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(articleQuery).WithArgs(groupID, articleNum, year, month).
					WillReturnRows(sqlmock.NewRows([]string{"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date", "received", "thread_id", "parent", "h_references", "h_lines", "h_bytes"}).
						AddRow(groupID, articleNum, "msg1@host.com", "Subj", "From", "DateStr", sampleReceived, 123, 0, "", 10, 100))
			},
			expectArticle: &models.Article{GroupID: groupID, ArticleNum: articleNum, MessageID: "msg1@host.com", Subject: "Subj", From: "From", Date: "DateStr", Received: sampleReceived, ThreadID: 123, GroupName: groupName, Lines: 10, Bytes: 100},
			expectErr:     false,
			checkReceived: true,
		},
		{
			name:      "article not found with specific date, but found with relaxed date (redirect case)",
			groupName: groupName, year: year, month: month, articleNum: articleNum, // URL date
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(articleQuery).WithArgs(groupID, articleNum, year, month).WillReturnError(sql.ErrNoRows) // Not found with URL date

				// Found with different date
				differentReceived := time.Date(2023, 12, 15, 0,0,0,0, time.UTC)
				mock.ExpectQuery(relaxedArticleQuery).WithArgs(groupID, articleNum).
					WillReturnRows(sqlmock.NewRows([]string{"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date", "received", "thread_id", "parent", "h_references", "h_lines", "h_bytes"}).
						AddRow(groupID, articleNum, "msg1@host.com", "Subj", "From", "DateStr", differentReceived, 123, 0, "", 10, 100))
			},
			expectArticle: &models.Article{GroupID: groupID, ArticleNum: articleNum, MessageID: "msg1@host.com", Subject: "Subj", From: "From", Date: "DateStr", Received: time.Date(2023, 12, 15, 0,0,0,0, time.UTC), ThreadID: 123, GroupName: groupName, Lines: 10, Bytes: 100},
			expectErr:     false,
			checkReceived: true, // Critical to check this for redirect logic
		},
		{
			name:      "article truly not found (not with specific date, not with relaxed date)",
			groupName: groupName, year: year, month: month, articleNum: articleNum,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(articleQuery).WithArgs(groupID, articleNum, year, month).WillReturnError(sql.ErrNoRows)
				mock.ExpectQuery(relaxedArticleQuery).WithArgs(groupID, articleNum).WillReturnError(sql.ErrNoRows)
			},
			expectErr:    true,
			expectErrMsg: fmt.Sprintf("article num %d in group '%s' (year %d, month %d) not found", articleNum, groupName, year, month),
		},
		{
			name:      "group not found",
			groupName: "unknown.group", year: year, month: month, articleNum: articleNum,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs("unknown.group").WillReturnError(sql.ErrNoRows)
			},
			expectErr:    true,
			expectErrMsg: "group 'unknown.group' not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mock)
			article, err := db.GetArticleByDetails(tt.groupName, tt.year, tt.month, tt.articleNum)

			if (err != nil) != tt.expectErr {
				t.Errorf("GetArticleByDetails() error = %v, expectErr %v", err, tt.expectErr)
				return
			}
			if tt.expectErr {
				if !strings.Contains(err.Error(), tt.expectErrMsg) {
					t.Errorf("GetArticleByDetails() error message = '%s', want to contain '%s'", err.Error(), tt.expectErrMsg)
				}
				return
			}
			if article == nil && !tt.expectErr {
				t.Errorf("GetArticleByDetails() expected article, got nil")
				return
			}
			if article != nil {
				if article.ArticleNum != tt.expectArticle.ArticleNum || article.MessageID != tt.expectArticle.MessageID || article.GroupName != tt.expectArticle.GroupName {
					t.Errorf("GetArticleByDetails() got %+v, want %+v (basic check)", article, tt.expectArticle)
				}
				if tt.checkReceived && !article.Received.Equal(tt.expectArticle.Received) {
					t.Errorf("GetArticleByDetails() Received date mismatch: got %v, want %v", article.Received, tt.expectArticle.Received)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("sqlmock expectations were not met: %s", err)
			}
		})
	}
}


func TestDB_GetArticleByMessageID(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()
	db := NewWithSQLDB(mockDB)

	messageID := "unique.msg.id@example.com"
	sampleReceived := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

	query := regexp.QuoteMeta(`
		SELECT a.group_id, a.id, a.h_messageid, a.h_subject, a.h_from, a.h_date,
		       a.received, a.thread_id, a.parent, a.h_references, a.h_lines, a.h_bytes,
		       g.name as group_name
		FROM articles a
		JOIN ` + "`groups`" + ` g ON a.group_id = g.id
		WHERE a.h_messageid = ?
		LIMIT 1`)

	tests := []struct {
		name          string
		messageID     string
		setupMock     func(mock sqlmock.Sqlmock)
		expectArticle *models.Article
		expectErr     bool
		expectErrMsg  string
	}{
		{
			name:      "success - article found",
			messageID: messageID,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(query).WithArgs(messageID).
					WillReturnRows(sqlmock.NewRows([]string{"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date", "received", "thread_id", "parent", "h_references", "h_lines", "h_bytes", "group_name"}).
						AddRow(1, 101, messageID, "Found It", "Finder", "DateFound", sampleReceived, 123, 0, "", 10, 100, "found.group"))
			},
			expectArticle: &models.Article{GroupID: 1, ArticleNum: 101, MessageID: messageID, Subject: "Found It", From: "Finder", Date: "DateFound", Received: sampleReceived, ThreadID: 123, GroupName: "found.group", Lines: 10, Bytes: 100},
			expectErr:     false,
		},
		{
			name:      "article not found by message id",
			messageID: "nonexistent@msg.id",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(query).WithArgs("nonexistent@msg.id").WillReturnError(sql.ErrNoRows)
			},
			expectErr:    true,
			expectErrMsg: "article with Message-ID 'nonexistent@msg.id' not found",
		},
		{
			name:      "database query error",
			messageID: messageID,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(query).WithArgs(messageID).WillReturnError(errors.New("db fault"))
			},
			expectErr:    true,
			expectErrMsg: "failed to query article by Message-ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mock)
			article, err := db.GetArticleByMessageID(tt.messageID)

			if (err != nil) != tt.expectErr {
				t.Errorf("GetArticleByMessageID() error = %v, expectErr %v", err, tt.expectErr)
				return
			}
			if tt.expectErr {
				if !strings.Contains(err.Error(), tt.expectErrMsg) {
					t.Errorf("GetArticleByMessageID() error message = '%s', want to contain '%s'", err.Error(), tt.expectErrMsg)
				}
				return
			}
			if !reflect.DeepEqual(article, tt.expectArticle) {
				t.Errorf("GetArticleByMessageID() got = %+v, want %+v", article, tt.expectArticle)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("sqlmock expectations were not met: %s", err)
			}
		})
	}
}

func TestDB_GetThreadMessages(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()
	db := NewWithSQLDB(mockDB)

	threadID, currentArticleGroupID, currentArticleNum := uint32(789), uint16(1), uint32(101)
	sampleReceived1 := time.Date(2024, 1, 10, 10, 0, 0, 0, time.UTC)
	sampleReceived2 := time.Date(2024, 1, 10, 11, 0, 0, 0, time.UTC)


	query := regexp.QuoteMeta(`
		SELECT a.group_id, a.id, a.h_messageid, a.h_subject, a.h_from, a.h_date,
		       a.received, a.thread_id, a.parent, a.h_references, a.h_lines, a.h_bytes,
		       g.name as group_name
		FROM articles a
		JOIN ` + "`groups`" + ` g ON a.group_id = g.id
		WHERE a.thread_id = ? AND NOT (a.group_id = ? AND a.id = ?)
		ORDER BY a.received ASC, a.id ASC`)

	cols := []string{"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date", "received", "thread_id", "parent", "h_references", "h_lines", "h_bytes", "group_name"}

	tests := []struct {
		name                string
		threadID            uint32
		currentArticleGID   uint16
		currentArticleNum   uint32
		setupMock           func(mock sqlmock.Sqlmock)
		expectedCount       int
		expectErr           bool
		expectErrMsg        string
	}{
		{
			name: "success - multiple messages in thread",
			threadID: threadID, currentArticleGID: currentArticleGroupID, currentArticleNum: currentArticleNum,
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows(cols).
					AddRow(1, 102, "msg102@host", "Re: Subj", "UserA", "DateA", sampleReceived1, threadID, 101, "", 5, 50, "group1").
					AddRow(1, 103, "msg103@host", "Re: Re: Subj", "UserB", "DateB", sampleReceived2, threadID, 102, "", 6, 60, "group1")
				mock.ExpectQuery(query).WithArgs(threadID, currentArticleGroupID, currentArticleNum).WillReturnRows(rows)
			},
			expectedCount: 2,
			expectErr: false,
		},
		{
			name: "success - no other messages in thread",
			threadID: threadID, currentArticleGID: currentArticleGroupID, currentArticleNum: currentArticleNum,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(query).WithArgs(threadID, currentArticleGroupID, currentArticleNum).WillReturnRows(sqlmock.NewRows(cols))
			},
			expectedCount: 0,
			expectErr: false,
		},
		{
			name: "database query error",
			threadID: threadID, currentArticleGID: currentArticleGroupID, currentArticleNum: currentArticleNum,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(query).WithArgs(threadID, currentArticleGroupID, currentArticleNum).WillReturnError(errors.New("thread db fault"))
			},
			expectErr: true,
			expectErrMsg: "failed to query thread messages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mock)
			articles, err := db.GetThreadMessages(tt.threadID, tt.currentArticleGID, tt.currentArticleNum)

			if (err != nil) != tt.expectErr {
				t.Errorf("GetThreadMessages() error = %v, expectErr %v", err, tt.expectErr)
				return
			}
			if tt.expectErr {
				if !strings.Contains(err.Error(), tt.expectErrMsg) {
					t.Errorf("GetThreadMessages() error message = '%s', want to contain '%s'", err.Error(), tt.expectErrMsg)
				}
				return
			}
			if len(articles) != tt.expectedCount {
				t.Errorf("GetThreadMessages() got %d articles, want %d", len(articles), tt.expectedCount)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("sqlmock expectations were not met: %s", err)
			}
		})
	}
}

// TestDB_GetAllNewsgroups_ScanError simulates an error during rows.Scan
func TestDB_GetAllNewsgroups_ScanError(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()

	// db := &DB{sqlDB: mockDB}
	db := NewWithSQLDB(mockDB) // Use the test constructor
	query := "SELECT id, name, description FROM `groups` ORDER BY name"

	// Simulate a row that causes a scan error by providing incompatible data type
	rows := sqlmock.NewRows([]string{"id", "name", "description"}).
		AddRow(1, "comp.lang.go", "Go Programming Language"). // First row is fine
		AddRow("not-an-int", "another.group", "Another description") // Second row will cause Scan to fail

	mock.ExpectQuery(regexp.QuoteMeta(query)).WillReturnRows(rows)

	_, err = db.GetAllNewsgroups()

	if err == nil {
		t.Errorf("Expected an error from GetAllNewsgroups due to Scan failure, but got nil")
	}

	// Check if all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}


// Example of how to test New function (basic ping)
func TestNew(t *testing.T) {
	// This test requires a running MySQL instance or more complex mocking.
	// For now, we'll skip if no real DB config is provided or use sqlmock for a basic ping.

	// cfg := &config.Config{ // This variable was unused as the test is skipped.
	// 	DBHost: "localhost",
	// 	DBPort: 3306,
	// 	DBUser: "testuser",
	// 	DBPass: "testpass",
	// 	DBName: "testdb",
	// }

	// To truly test New, you'd point it to a test DB.
	// Here's how you might use sqlmock for the Ping part:
	mockDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("Failed to create mockDB: %v", err)
	}
	defer mockDB.Close()

	// Mock the DSN string that sql.Open would use
	// This is tricky because sql.Open is called internally.
	// A better approach for testing New might be to pass *sql.DB factory or use an interface.
	// For now, this specific test for New is more of an integration test.
	// We can test the Ping expectation.
	mock.ExpectPing().WillReturnError(nil) // Expect a ping and it should succeed.

	// To make this work with the current New, we'd need to be able to intercept sql.Open.
	// This is a limitation of testing functions that directly call sql.Open.
	// One common pattern is to have New accept a function like `func(driverName, dsn string) (*sql.DB, error)`
	// which defaults to sql.Open but can be replaced in tests.

	// Given the current structure of New, a simple unit test for it with sqlmock is hard.
	// We're focusing on method tests like GetAllNewsgroups.
	// A real test for New would involve setting up a test database.
	t.Log("Skipping direct test for New() with sqlmock due to sql.Open call. Integration test recommended.")
}

func TestDB_GetMinMaxMessageMonthsForGroup(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()
	db := NewWithSQLDB(mockDB)

	groupName := "test.group"
	var groupID uint16 = 1

	groupQuery := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
	countQuery := regexp.QuoteMeta("SELECT COUNT(*) FROM articles WHERE group_id = ?")
	dateQuery := regexp.QuoteMeta("SELECT MIN(received), MAX(received) FROM articles WHERE group_id = ?")

	tests := []struct {
		name             string
		setupMock        func(mock sqlmock.Sqlmock)
		expectedMinYear  int
		expectedMinMonth int
		expectedMaxYear  int
		expectedMaxMonth int
		expectedFound    bool
		expectErr        bool
		expectErrMsg     string
	}{
		{
			name: "success - messages found",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(countQuery).WithArgs(groupID).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(10)) // Has messages
				minDate := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
				maxDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
				mock.ExpectQuery(dateQuery).WithArgs(groupID).WillReturnRows(sqlmock.NewRows([]string{"MIN(received)", "MAX(received)"}).AddRow(minDate, maxDate))
			},
			expectedMinYear: 2023, expectedMinMonth: 5,
			expectedMaxYear: 2024, expectedMaxMonth: 1,
			expectedFound: true, expectErr: false,
		},
		{
			name: "no messages in group",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(countQuery).WithArgs(groupID).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0)) // No messages
			},
			expectedFound: false, expectErr: false,
		},
		{
			name: "group not found",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnError(sql.ErrNoRows)
			},
			expectErr: true, expectErrMsg: "group 'test.group' not found",
		},
		{
			name: "error counting messages",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(countQuery).WithArgs(groupID).WillReturnError(errors.New("count error"))
			},
			expectErr: true, expectErrMsg: "failed to count messages",
		},
		{
			name: "error getting min/max dates",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(countQuery).WithArgs(groupID).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(10))
				mock.ExpectQuery(dateQuery).WithArgs(groupID).WillReturnError(errors.New("date query error"))
			},
			expectErr: true, expectErrMsg: "failed to get min/max dates",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mock)
			minY, minM, maxY, maxM, found, err := db.GetMinMaxMessageMonthsForGroup(groupName)

			if (err != nil) != tt.expectErr {
				t.Errorf("GetMinMaxMessageMonthsForGroup() error = %v, expectErr %v", err, tt.expectErr)
				return
			}
			if tt.expectErr {
				if !strings.Contains(err.Error(), tt.expectErrMsg) {
					t.Errorf("GetMinMaxMessageMonthsForGroup() error msg = '%s', want to contain '%s'", err.Error(), tt.expectErrMsg)
				}
				return
			}
			if found != tt.expectedFound {
				t.Errorf("GetMinMaxMessageMonthsForGroup() found = %v, want %v", found, tt.expectedFound)
			}
			if found {
				if minY != tt.expectedMinYear || minM != tt.expectedMinMonth || maxY != tt.expectedMaxYear || maxM != tt.expectedMaxMonth {
					t.Errorf("GetMinMaxMessageMonthsForGroup() dates: got min %d-%d, max %d-%d; want min %d-%d, max %d-%d",
						minY, minM, maxY, maxM, tt.expectedMinYear, tt.expectedMinMonth, tt.expectedMaxYear, tt.expectedMaxMonth)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("sqlmock expectations were not met: %s", err)
			}
		})
	}
}


func TestDB_GetPrevMonthWithMessages(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()
	db := NewWithSQLDB(mockDB)

	groupName := "test.group"
	var groupID uint16 = 1
	currentYear, currentMonth := 2024, 3 // March 2024

	groupQuery := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
	prevMonthQuery := regexp.QuoteMeta(`
		SELECT YEAR(received), MONTH(received)
		FROM articles
		WHERE group_id = ? AND received < ?
		ORDER BY received DESC
		LIMIT 1`)

	tests := []struct{
		name string
		setupMock func(mock sqlmock.Sqlmock)
		expectedYear int
		expectedMonth int
		expectedFound bool
		expectErr bool
		expectErrMsg string
	}{
		{
			name: "success - prev month found",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				targetDate := time.Date(currentYear, time.Month(currentMonth), 1, 0,0,0,0,time.UTC)
				mock.ExpectQuery(prevMonthQuery).WithArgs(groupID, targetDate).
					WillReturnRows(sqlmock.NewRows([]string{"YEAR(received)", "MONTH(received)"}).AddRow(2024, 2))
			},
			expectedYear: 2024, expectedMonth: 2, expectedFound: true, expectErr: false,
		},
		{
			name: "no previous month with messages",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				targetDate := time.Date(currentYear, time.Month(currentMonth), 1, 0,0,0,0,time.UTC)
				mock.ExpectQuery(prevMonthQuery).WithArgs(groupID, targetDate).WillReturnError(sql.ErrNoRows)
			},
			expectedFound: false, expectErr: false,
		},
		{
			name: "group not found",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnError(sql.ErrNoRows)
			},
			expectErr: true, expectErrMsg: "group 'test.group' not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mock)
			pY, pM, found, err := db.GetPrevMonthWithMessages(groupName, currentYear, currentMonth)
			if (err != nil) != tt.expectErr {
				t.Errorf("GetPrevMonthWithMessages() error = %v, expectErr %v", err, tt.expectErr)
				return
			}
			if tt.expectErr {
				if !strings.Contains(err.Error(), tt.expectErrMsg) {
					t.Errorf("GetPrevMonthWithMessages() error msg = '%s', want to contain '%s'", err.Error(), tt.expectErrMsg)
				}
				return
			}
			if found != tt.expectedFound {
				t.Errorf("GetPrevMonthWithMessages() found = %v, want %v", found, tt.expectedFound)
			}
			if found {
				if pY != tt.expectedYear || pM != tt.expectedMonth {
					t.Errorf("GetPrevMonthWithMessages() date: got %d-%d, want %d-%d", pY, pM, tt.expectedYear, tt.expectedMonth)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("sqlmock expectations were not met: %s", err)
			}
		})
	}
}

func TestDB_GetNextMonthWithMessages(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()
	db := NewWithSQLDB(mockDB)

	groupName := "test.group"
	var groupID uint16 = 1
	currentYear, currentMonth := 2024, 1 // Jan 2024

	groupQuery := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
	nextMonthQuery := regexp.QuoteMeta(`
		SELECT YEAR(received), MONTH(received)
		FROM articles
		WHERE group_id = ? AND received >= ?
		ORDER BY received ASC
		LIMIT 1`)

	tests := []struct{
		name string
		setupMock func(mock sqlmock.Sqlmock)
		expectedYear int
		expectedMonth int
		expectedFound bool
		expectErr bool
		expectErrMsg string
	}{
		{
			name: "success - next month found",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				targetDate := time.Date(currentYear, time.Month(currentMonth), 1, 0,0,0,0,time.UTC).AddDate(0,1,0)
				mock.ExpectQuery(nextMonthQuery).WithArgs(groupID, targetDate).
					WillReturnRows(sqlmock.NewRows([]string{"YEAR(received)", "MONTH(received)"}).AddRow(2024, 2))
			},
			expectedYear: 2024, expectedMonth: 2, expectedFound: true, expectErr: false,
		},
		{
			name: "no next month with messages",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				targetDate := time.Date(currentYear, time.Month(currentMonth), 1, 0,0,0,0,time.UTC).AddDate(0,1,0)
				mock.ExpectQuery(nextMonthQuery).WithArgs(groupID, targetDate).WillReturnError(sql.ErrNoRows)
			},
			expectedFound: false, expectErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mock)
			nY, nM, found, err := db.GetNextMonthWithMessages(groupName, currentYear, currentMonth)
			if (err != nil) != tt.expectErr {
				t.Errorf("GetNextMonthWithMessages() error = %v, expectErr %v", err, tt.expectErr)
				return
			}
			// ... (error and found checks similar to GetPrevMonthWithMessages)
			if found != tt.expectedFound {
				t.Errorf("GetNextMonthWithMessages() found = %v, want %v", found, tt.expectedFound)
			}
			if found {
				if nY != tt.expectedYear || nM != tt.expectedMonth {
					t.Errorf("GetNextMonthWithMessages() date: got %d-%d, want %d-%d", nY, nM, tt.expectedYear, tt.expectedMonth)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("sqlmock expectations were not met: %s", err)
			}
		})
	}
}

// Helper to create a valid driver.Value from string for testing time.Time scans
// Not directly used in GetAllNewsgroups but useful for other tests involving time.
func timeToDriverValue(t time.Time) driver.Value {
	return t
}

func TestDB_GetMessagesForGroupMonth(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()

	db := NewWithSQLDB(mockDB) // Use the test constructor

	groupName := "comp.test"
	year, month := 2024, 3
	var groupID uint16 = 1

	groupQuery := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
	articleQuery := regexp.QuoteMeta(`
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
		       received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND YEAR(received) = ? AND MONTH(received) = ?
		ORDER BY received DESC, id DESC`)

	// Default time for articles
	sampleReceivedTime := time.Date(year, time.Month(month), 15, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name               string
		groupName          string
		year               int
		month              int
		setupMock          func(mock sqlmock.Sqlmock)
		expectedArticleCount int
		expectErr          bool
		expectedErrMsg     string // Substring of expected error message
	}{
		{
			name:      "success - messages found",
			groupName: groupName, year: year, month: month,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				articleRows := sqlmock.NewRows([]string{
					"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date",
					"received", "thread_id", "parent", "h_references", "h_lines", "h_bytes",
				}).
					AddRow(groupID, 101, "msg1@example.com", "Subject 1", "User1 <user1@example.com>", "Date1", sampleReceivedTime, 1, 0, "", 10, 100).
					AddRow(groupID, 102, "msg2@example.com", "Subject 2", "User2 <user2@example.com>", "Date2", sampleReceivedTime.Add(-time.Hour), 2, 0, "", 12, 120)
				mock.ExpectQuery(articleQuery).WithArgs(groupID, year, month).WillReturnRows(articleRows)
			},
			expectedArticleCount: 2,
			expectErr:            false,
		},
		{
			name:      "success - no messages found for month",
			groupName: groupName, year: year, month: month,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				emptyArticleRows := sqlmock.NewRows([]string{
					"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date",
					"received", "thread_id", "parent", "h_references", "h_lines", "h_bytes",
				})
				mock.ExpectQuery(articleQuery).WithArgs(groupID, year, month).WillReturnRows(emptyArticleRows)
			},
			expectedArticleCount: 0,
			expectErr:            false,
		},
		{
			name:      "group not found",
			groupName: "unknown.group", year: year, month: month,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs("unknown.group").WillReturnError(sql.ErrNoRows)
			},
			expectErr:      true,
			expectedErrMsg: "group 'unknown.group' not found",
		},
		{
			name:      "error querying group id",
			groupName: groupName, year: year, month: month,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnError(errors.New("db error during group lookup"))
			},
			expectErr:      true,
			expectedErrMsg: "failed to query group ID",
		},
		{
			name:      "error querying articles",
			groupName: groupName, year: year, month: month,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(articleQuery).WithArgs(groupID, year, month).WillReturnError(errors.New("db error during article lookup"))
			},
			expectErr:      true,
			expectedErrMsg: "failed to query articles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mock)
			articles, err := db.GetMessagesForGroupMonth(tt.groupName, tt.year, tt.month)

			if (err != nil) != tt.expectErr {
				t.Errorf("GetMessagesForGroupMonth() error = %v, expectErr %v", err, tt.expectErr)
				return
			}
			if tt.expectErr && err != nil {
				if !strings.Contains(err.Error(), tt.expectedErrMsg) {
					t.Errorf("GetMessagesForGroupMonth() error message = %s, want to contain %s", err.Error(), tt.expectedErrMsg)
				}
				return // Error expected and received, no further checks needed
			}

			if len(articles) != tt.expectedArticleCount {
				t.Errorf("GetMessagesForGroupMonth() got %d articles, want %d", len(articles), tt.expectedArticleCount)
			}

			if tt.expectedArticleCount > 0 && len(articles) > 0 {
				// Check if GroupName is populated
				if articles[0].GroupName != tt.groupName {
					t.Errorf("Expected GroupName to be '%s', got '%s'", tt.groupName, articles[0].GroupName)
				}
			}


			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("there were unfulfilled expectations: %s", err)
			}
		})
	}
}

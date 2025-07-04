package database

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"nntp-web/internal/models"
	// "nntp-web/internal/config" // Removed: unused after cfg was commented out

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDB_GetAllNewsgroups(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()

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
			mockRows:      nil,
			expected:      nil,
			expectErr:     true,
			mockExpectErr: errors.New("db query error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Pass nil for config, as these tests don't rely on config-specific behavior like SQL logging.
			// NewWithSQLDB will use a default TTL if config is nil or CacheTTLSeconds is not positive.
			db := NewWithSQLDB(mockDB, nil)
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
				// Loop carefully, only if lengths match and expected is not empty
				if len(groups) == len(tt.expected) && len(tt.expected) > 0 {
					for i, g := range groups {
						if g.ID != tt.expected[i].ID || g.Name != tt.expected[i].Name || g.Description != tt.expected[i].Description {
							t.Errorf("GetAllNewsgroups() group mismatch at index %d. Got %+v, want %+v", i, g, tt.expected[i])
						}
					}
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("there were unfulfilled expectations: %s", err)
			}
		})
	}
}

func TestDB_GetLatestMessageMonthForGroup(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()

	groupName := "test.group.latest"
	var groupID uint16 = 7

	groupQuery := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
	latestMonthQuery := regexp.QuoteMeta(`
		SELECT YEAR(received), MONTH(received)
		FROM articles
		WHERE group_id = ?
		ORDER BY received DESC
		LIMIT 1`)

	tests := []struct {
		name          string
		setupMock     func(dbMock sqlmock.Sqlmock)
		expectedYear  int
		expectedMonth int
		expectedFound bool
		runTwiceForCacheCheck bool // If true, run the DB call twice to check cache
	}{
		{
			name: "success - latest month found",
			setupMock: func(dbMock sqlmock.Sqlmock) {
				dbMock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				dbMock.ExpectQuery(latestMonthQuery).WithArgs(groupID).
					WillReturnRows(sqlmock.NewRows([]string{"YEAR(received)", "MONTH(received)"}).AddRow(2024, 7))
			},
			expectedYear:  2024,
			expectedMonth: 7,
			expectedFound: true,
		},
		{
			name: "group found, but no messages",
			setupMock: func(dbMock sqlmock.Sqlmock) {
				dbMock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				dbMock.ExpectQuery(latestMonthQuery).WithArgs(groupID).WillReturnError(sql.ErrNoRows)
			},
			expectedFound: false,
		},
		{
			name: "group not found",
			setupMock: func(dbMock sqlmock.Sqlmock) {
				dbMock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnError(sql.ErrNoRows)
			},
			expectedFound: false,
		},
		{
			name: "error querying group ID",
			setupMock: func(dbMock sqlmock.Sqlmock) {
				dbMock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnError(errors.New("db group error"))
			},
			expectedFound: false, // Expect found=false on error
		},
		{
			name: "error querying latest month",
			setupMock: func(dbMock sqlmock.Sqlmock) {
				dbMock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				dbMock.ExpectQuery(latestMonthQuery).WithArgs(groupID).WillReturnError(errors.New("db article error"))
			},
			expectedFound: false, // Expect found=false on error
		},
		{
			name: "cache hit - latest month found",
			setupMock: func(dbMock sqlmock.Sqlmock) {
				// First call populates cache (mocked once)
				dbMock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				dbMock.ExpectQuery(latestMonthQuery).WithArgs(groupID).
					WillReturnRows(sqlmock.NewRows([]string{"YEAR(received)", "MONTH(received)"}).AddRow(2024, 7))
				// Second call should hit cache, so no more DB interactions expected by sqlmock for this specific sub-test run
			},
			expectedYear:  2024,
			expectedMonth: 7,
			expectedFound: true,
			runTwiceForCacheCheck: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := NewWithSQLDB(mockDB, nil) // New DB (and cache) for each sub-test, pass nil for config
			tt.setupMock(mock)

			year, month, found := db.GetLatestMessageMonthForGroup(groupName)

			if found != tt.expectedFound {
				t.Errorf("GetLatestMessageMonthForGroup() found = %v, want %v", found, tt.expectedFound)
			}
			if tt.expectedFound {
				if year != tt.expectedYear || month != tt.expectedMonth {
					t.Errorf("GetLatestMessageMonthForGroup() year = %d, month = %d, want year = %d, month = %d", year, month, tt.expectedYear, tt.expectedMonth)
				}
			}

			if tt.runTwiceForCacheCheck {
				// Call again, should hit cache. setupMock already prepared expectations for the first call.
				// No new DB calls should be made if cache works.
				year2, month2, found2 := db.GetLatestMessageMonthForGroup(groupName)
				if found2 != tt.expectedFound {
					t.Errorf("GetLatestMessageMonthForGroup() (cache check) found = %v, want %v", found2, tt.expectedFound)
				}
				if tt.expectedFound {
					if year2 != tt.expectedYear || month2 != tt.expectedMonth {
						t.Errorf("GetLatestMessageMonthForGroup() (cache check) year = %d, month = %d, want year = %d, month = %d", year2, month2, tt.expectedYear, tt.expectedMonth)
					}
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("sqlmock expectations were not met: %s", err)
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

	groupName := "rec.arts.books"
	var groupID uint16 = 3
	year, month, articleNum := 2024, 1, uint32(101)
	sampleReceived := time.Date(year, time.Month(month), 10, 0, 0, 0, 0, time.UTC)
	rawRefsOriginal := "<ref1@host> <ref2@domain>"
	parsedRefsOriginal := models.ParseReferencesString(rawRefsOriginal)


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

	articleCols := []string{"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date", "received", "thread_id", "parent", "h_references", "h_lines", "h_bytes"}

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
		checkReceived  bool
	}{
		{
			name:      "success - article found with correct date",
			groupName: groupName, year: year, month: month, articleNum: articleNum,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(articleDetailsQuery).WithArgs(groupID, articleNum, year, month).
					WillReturnRows(sqlmock.NewRows(articleCols).
						AddRow(groupID, articleNum, "msg1@host.com", "Subj", "From", "DateStr", sampleReceived, uint32(123), uint32(0), rawRefsOriginal, uint32(10), uint32(100)))
			},
			expectArticle: &models.Article{GroupID: groupID, ArticleNum: articleNum, MessageID: "msg1@host.com", Subject: "Subj", From: "From", Date: "DateStr", Received: sampleReceived, ThreadID: 123, GroupName: groupName, RawReferences: rawRefsOriginal, References: parsedRefsOriginal, Lines: 10, Bytes: 100},
			expectErr:     false,
			checkReceived: true,
		},
		{
			name:      "article not found with specific date, but found with relaxed date (redirect case)",
			groupName: groupName, year: year, month: month, articleNum: articleNum,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(articleDetailsQuery).WithArgs(groupID, articleNum, year, month).WillReturnError(sql.ErrNoRows)
				differentReceived := time.Date(2023, 12, 15, 0,0,0,0, time.UTC)
				mock.ExpectQuery(relaxedArticleDetailsQuery).WithArgs(groupID, articleNum).
					WillReturnRows(sqlmock.NewRows(articleCols).
						AddRow(groupID, articleNum, "msg1@host.com", "Subj", "From", "DateStr", differentReceived, uint32(123), uint32(0), rawRefsOriginal, uint32(10), uint32(100)))
			},
			expectArticle: &models.Article{GroupID: groupID, ArticleNum: articleNum, MessageID: "msg1@host.com", Subject: "Subj", From: "From", Date: "DateStr", Received: time.Date(2023, 12, 15, 0,0,0,0, time.UTC), ThreadID: 123, GroupName: groupName, RawReferences: rawRefsOriginal, References: parsedRefsOriginal, Lines: 10, Bytes: 100},
			expectErr:     false,
			checkReceived: true,
		},
		{
			name:      "article truly not found (not with specific date, not with relaxed date)",
			groupName: groupName, year: year, month: month, articleNum: articleNum,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(groupQuery).WithArgs(groupName).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(groupID))
				mock.ExpectQuery(articleDetailsQuery).WithArgs(groupID, articleNum, year, month).WillReturnError(sql.ErrNoRows)
				mock.ExpectQuery(relaxedArticleDetailsQuery).WithArgs(groupID, articleNum).WillReturnError(sql.ErrNoRows)
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
			db := NewWithSQLDB(mockDB, nil)
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
			if !reflect.DeepEqual(article, tt.expectArticle) {
                 if !(article == nil && tt.expectArticle == nil) {
				    t.Errorf("GetArticleByDetails() got = \n%+v, want \n%+v", article, tt.expectArticle)
                 }
			}
			if tt.checkReceived && article != nil && tt.expectArticle != nil && !article.Received.Equal(tt.expectArticle.Received) {
				t.Errorf("GetArticleByDetails() Received date mismatch: got %v, want %v", article.Received, tt.expectArticle.Received)
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

	messageID := "unique.msg.id@example.com"
	sampleReceived := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	rawRefs := "<ref@msgid>"
	parsedRefs := models.ParseReferencesString(rawRefs)
	articleColsWithGroup := []string{"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date", "received", "thread_id", "parent", "h_references", "h_lines", "h_bytes", "group_name"}

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
					WillReturnRows(sqlmock.NewRows(articleColsWithGroup).
						AddRow(1, 101, messageID, "Found It", "Finder", "DateFound", sampleReceived, uint32(123), uint32(0), rawRefs, uint32(10), uint32(100), "found.group"))
			},
			expectArticle: &models.Article{GroupID: 1, ArticleNum: 101, MessageID: messageID, Subject: "Found It", From: "Finder", Date: "DateFound", Received: sampleReceived, ThreadID: 123, GroupName: "found.group", RawReferences: rawRefs, References: parsedRefs, Lines: 10, Bytes: 100},
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
			db := NewWithSQLDB(mockDB, nil)
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

	threadIDToTest := uint32(789) // Test with a specific thread ID
	sampleReceived1 := time.Date(2024, 1, 10, 10, 0, 0, 0, time.UTC)
	sampleReceived2 := time.Date(2024, 1, 10, 11, 0, 0, 0, time.UTC)
	cols := []string{"group_id", "id", "h_messageid", "h_subject", "h_from", "h_date", "received", "thread_id", "parent", "h_references", "h_lines", "h_bytes", "group_name"}

	query := regexp.QuoteMeta(`
		SELECT a.group_id, a.id, a.h_messageid, a.h_subject, a.h_from, a.h_date,
		       a.received, a.thread_id, a.parent, a.h_references, a.h_lines, a.h_bytes,
		       g.name as group_name
		FROM articles a
		JOIN ` + "`groups` g ON a.group_id = g.id" + `
		WHERE a.thread_id = ?
		ORDER BY a.received ASC, a.id ASC`)

	tests := []struct {
		name                string
		threadID            uint32
		setupMock           func(mock sqlmock.Sqlmock)
		expectedArticles    []models.Article
		expectErr           bool
		expectErrMsg        string
	}{
		{
			name: "success - all messages in thread returned",
			threadID: threadIDToTest,
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows(cols).
					AddRow(1, 101, "msg101@host", "Original Subj", "User0", "Date0", sampleReceived1.Add(-time.Hour), threadIDToTest, 0, "", 4, 40, "group1"). // The "current" article
					AddRow(1, 102, "msg102@host", "Re: Subj", "UserA", "DateA", sampleReceived1, threadIDToTest, 101, "<refA>", 5, 50, "group1").
					AddRow(1, 103, "msg103@host", "Re: Re: Subj", "UserB", "DateB", sampleReceived2, threadIDToTest, 102, "<refB1> <refB2>", 6, 60, "group1")
				mock.ExpectQuery(query).WithArgs(threadIDToTest).WillReturnRows(rows)
			},
			expectedArticles: []models.Article{
				{GroupID: 1, ArticleNum: 101, MessageID: "msg101@host", Subject: "Original Subj", From: "User0", Date: "Date0", Received: sampleReceived1.Add(-time.Hour), ThreadID: threadIDToTest, ParentNum: 0, RawReferences: "", References: models.ParseReferencesString(""), Lines: 4, Bytes: 40, GroupName: "group1"},
				{GroupID: 1, ArticleNum: 102, MessageID: "msg102@host", Subject: "Re: Subj", From: "UserA", Date: "DateA", Received: sampleReceived1, ThreadID: threadIDToTest, ParentNum: 101, RawReferences: "<refA>", References: models.ParseReferencesString("<refA>"), Lines: 5, Bytes: 50, GroupName: "group1"},
				{GroupID: 1, ArticleNum: 103, MessageID: "msg103@host", Subject: "Re: Re: Subj", From: "UserB", Date: "DateB", Received: sampleReceived2, ThreadID: threadIDToTest, ParentNum: 102, RawReferences: "<refB1> <refB2>", References: models.ParseReferencesString("<refB1> <refB2>"), Lines: 6, Bytes: 60, GroupName: "group1"},
			},
			expectErr: false,
		},
		{
			name: "success - no messages for threadID (empty result)", // e.g. threadID doesn't exist
			threadID: uint32(9999), // A different thread ID
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(query).WithArgs(uint32(9999)).WillReturnRows(sqlmock.NewRows(cols))
			},
			expectedArticles: []models.Article{},
			expectErr: false,
		},
		{
			name: "database query error",
			threadID: threadIDToTest,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(query).WithArgs(threadIDToTest).WillReturnError(errors.New("thread db fault"))
			},
			expectErr: true,
			expectErrMsg: "failed to query thread messages for thread_id 789", // Updated to match actual threadID
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := NewWithSQLDB(mockDB, nil)
			tt.setupMock(mock)
			articles, err := db.GetThreadMessages(tt.threadID)

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
			if len(articles) != len(tt.expectedArticles) {
				t.Errorf("GetThreadMessages() got %d articles, want %d", len(articles), len(tt.expectedArticles))
			}
			for i := range articles {
				if len(tt.expectedArticles) <= i || !reflect.DeepEqual(articles[i], tt.expectedArticles[i]) {
					t.Errorf("GetThreadMessages() article %d = %+v, want %+v", i, articles[i], tt.expectedArticles[i])
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("sqlmock expectations were not met: %s", err)
			}
		})
	}
}

func TestDB_GetAllNewsgroups_ScanError(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()

	db := NewWithSQLDB(mockDB, nil)
	query := "SELECT id, name, description FROM `groups` ORDER BY name"

	rows := sqlmock.NewRows([]string{"id", "name", "description"}).
		AddRow(1, "comp.lang.go", "Go Programming Language").
		AddRow("not-an-int", "another.group", "Another description")

	mock.ExpectQuery(regexp.QuoteMeta(query)).WillReturnRows(rows)
	_, err = db.GetAllNewsgroups()
	if err == nil {
		t.Errorf("Expected an error from GetAllNewsgroups due to Scan failure, but got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestNew(t *testing.T) {
	t.Log("Skipping direct test for New() with sqlmock due to sql.Open call. Integration test recommended.")
}

func TestDB_GetMinMaxMessageMonthsForGroup(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()

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
				mock.ExpectQuery(countQuery).WithArgs(groupID).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(10))
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
				mock.ExpectQuery(countQuery).WithArgs(groupID).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0))
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
            db := NewWithSQLDB(mockDB, nil)
			tt.setupMock(mock)
			minY, minM, maxY, maxM, found, err := db.GetMinMaxMessageMonthsForGroup(groupName)

			if (err != nil) != tt.expectErr { t.Errorf("GetMinMaxMessageMonthsForGroup() error = %v, expectErr %v", err, tt.expectErr); return }
			if tt.expectErr { if !strings.Contains(err.Error(), tt.expectErrMsg) { t.Errorf("GetMinMaxMessageMonthsForGroup() error msg = '%s', want to contain '%s'", err.Error(), tt.expectErrMsg) }; return }
			if found != tt.expectedFound { t.Errorf("GetMinMaxMessageMonthsForGroup() found = %v, want %v", found, tt.expectedFound) }
			if found { if minY != tt.expectedMinYear || minM != tt.expectedMinMonth || maxY != tt.expectedMaxYear || maxM != tt.expectedMaxMonth { t.Errorf("GetMinMaxMessageMonthsForGroup() dates: got min %d-%d, max %d-%d; want min %d-%d, max %d-%d", minY, minM, maxY, maxM, tt.expectedMinYear, tt.expectedMinMonth, tt.expectedMaxYear, tt.expectedMaxMonth) } }
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

	groupName := "test.group"
	var groupID uint16 = 1
	currentYear, currentMonth := 2024, 3

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
            db := NewWithSQLDB(mockDB, nil)
			tt.setupMock(mock)
			pY, pM, found, err := db.GetPrevMonthWithMessages(groupName, currentYear, currentMonth)
            if (err != nil) != tt.expectErr { t.Errorf("GetPrevMonthWithMessages() error = %v, expectErr %v", err, tt.expectErr); return }
            if tt.expectErr { if !strings.Contains(err.Error(), tt.expectErrMsg) {t.Errorf("GetPrevMonthWithMessages() error msg = '%s', want to contain '%s'", err.Error(), tt.expectErrMsg)}; return }
            if found != tt.expectedFound { t.Errorf("GetPrevMonthWithMessages() found = %v, want %v", found, tt.expectedFound) }
            if found { if pY != tt.expectedYear || pM != tt.expectedMonth { t.Errorf("GetPrevMonthWithMessages() date: got %d-%d, want %d-%d", pY, pM, tt.expectedYear, tt.expectedMonth) } }
			if err := mock.ExpectationsWereMet(); err != nil { t.Errorf("sqlmock expectations were not met: %s", err) }
		})
	}
}

func TestDB_GetNextMonthWithMessages(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()

	groupName := "test.group"
	var groupID uint16 = 1
	currentYear, currentMonth := 2024, 1

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
            db := NewWithSQLDB(mockDB, nil)
			tt.setupMock(mock)
			nY, nM, found, err := db.GetNextMonthWithMessages(groupName, currentYear, currentMonth)
            if (err != nil) != tt.expectErr { t.Errorf("GetNextMonthWithMessages() error = %v, expectErr %v", err, tt.expectErr); return }
            if tt.expectErr { if !strings.Contains(err.Error(), tt.expectErrMsg) {t.Errorf("GetNextMonthWithMessages() error msg = '%s', want to contain '%s'", err.Error(), tt.expectErrMsg)}; return }
            if found != tt.expectedFound { t.Errorf("GetNextMonthWithMessages() found = %v, want %v", found, tt.expectedFound) }
            if found { if nY != tt.expectedYear || nM != tt.expectedMonth { t.Errorf("GetNextMonthWithMessages() date: got %d-%d, want %d-%d", nY, nM, tt.expectedYear, tt.expectedMonth) } }
			if err := mock.ExpectationsWereMet(); err != nil { t.Errorf("sqlmock expectations were not met: %s", err)}
		})
	}
}

func timeToDriverValue(t time.Time) driver.Value {
	return t
}

func TestDB_GetMessagesForGroupMonth(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockDB.Close()

	groupName := "comp.test"
	year, month := 2024, 3
	var groupID uint16 = 1
	rawRefs1 := "<ref1@host>"
	rawRefs2 := "<ref2@host>"
	parsedRefs1 := models.ParseReferencesString(rawRefs1)
	parsedRefs2 := models.ParseReferencesString(rawRefs2)


	groupQuery := regexp.QuoteMeta("SELECT id FROM `groups` WHERE name = ?")
	articleQuery := regexp.QuoteMeta(`
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
		       received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND YEAR(received) = ? AND MONTH(received) = ?
		ORDER BY received DESC, id DESC`)

	sampleReceivedTime := time.Date(year, time.Month(month), 15, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name               string
		groupName          string
		year               int
		month              int
		setupMock          func(mock sqlmock.Sqlmock)
		expectedArticles   []models.Article
		expectErr          bool
		expectedErrMsg     string
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
					AddRow(groupID, 101, "msg1@example.com", "Subject 1", "User1 <user1@example.com>", "Date1", sampleReceivedTime, 1, 0, rawRefs1, 10, 100).
					AddRow(groupID, 102, "msg2@example.com", "Subject 2", "User2 <user2@example.com>", "Date2", sampleReceivedTime.Add(-time.Hour), 2, 0, rawRefs2, 12, 120)
				mock.ExpectQuery(articleQuery).WithArgs(groupID, year, month).WillReturnRows(articleRows)
			},
			expectedArticles: []models.Article{
				{GroupID: groupID, ArticleNum: 101, MessageID: "msg1@example.com", Subject: "Subject 1", From: "User1 <user1@example.com>", Date: "Date1", Received: sampleReceivedTime, ThreadID: 1, ParentNum: 0, RawReferences: rawRefs1, References: parsedRefs1, Lines: 10, Bytes: 100, GroupName: groupName},
				{GroupID: groupID, ArticleNum: 102, MessageID: "msg2@example.com", Subject: "Subject 2", From: "User2 <user2@example.com>", Date: "Date2", Received: sampleReceivedTime.Add(-time.Hour), ThreadID: 2, ParentNum: 0, RawReferences: rawRefs2, References: parsedRefs2, Lines: 12, Bytes: 120, GroupName: groupName},
			},
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
			expectedArticles: []models.Article{},
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
            db := NewWithSQLDB(mockDB, nil)
			tt.setupMock(mock)
			articles, err := db.GetMessagesForGroupMonth(tt.groupName, tt.year, tt.month)

            if (err != nil) != tt.expectErr { t.Errorf("GetMessagesForGroupMonth() error = %v, expectErr %v", err, tt.expectErr); return }
            if tt.expectErr && err != nil { if !strings.Contains(err.Error(), tt.expectedErrMsg) {t.Errorf("GetMessagesForGroupMonth() error message = '%s', want to contain '%s'", err.Error(), tt.expectedErrMsg)}; return }
			if len(articles) != len(tt.expectedArticles) {
				t.Errorf("GetMessagesForGroupMonth() got %d articles, want %d", len(articles), len(tt.expectedArticles))
			}
			// Compare relevant fields, especially References
			if len(articles) == len(tt.expectedArticles) && len(articles) > 0 {
				for i := range articles {
					// Must compare all relevant fields or use reflect.DeepEqual on the items after ensuring GroupName is set in expected.
					tt.expectedArticles[i].GroupName = articles[i].GroupName // Ensure groupname matches for DeepEqual if it's set by func
					if !reflect.DeepEqual(articles[i], tt.expectedArticles[i]) { // Full struct comparison
						 t.Errorf("GetMessagesForGroupMonth() article %d mismatch.\nGot: %+v\nWant:%+v", i, articles[i], tt.expectedArticles[i])
					}
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("there were unfulfilled expectations: %s", err)
			}
		})
	}
}

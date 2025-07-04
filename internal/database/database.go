package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"nntp-web/internal/config"
	"nntp-web/internal/models"
	"nntp-web/internal/cache" // Added for cache
	"strings"                 // Added

	_ "github.com/go-sql-driver/mysql" // MySQL driver
)

// DB wraps the sql.DB connection pool and an optional cache.
type DB struct {
	sqlDB    *sql.DB
	cache    *cache.Cache
	cacheTTL time.Duration
	cfg      *config.Config // Added to hold config for things like LogSQLQueries
}

// New creates a new DB instance and connects to the database.
// It also initializes it with the provided cache, TTL, and application config.
func New(appCfg *config.Config, appCache *cache.Cache) (*DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4",
		appCfg.DBUser,
		appCfg.DBPass,
		appCfg.DBHost,
		appCfg.DBPort,
		appCfg.DBName,
	)

	log.Printf("Connecting to database: %s@tcp(%s:%d)/%s", appCfg.DBUser, appCfg.DBHost, appCfg.DBPort, appCfg.DBName)

	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Configure connection pool settings (optional, but good practice)
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Verify the connection
	if err = sqlDB.Ping(); err != nil {
		sqlDB.Close() // Close the connection if ping fails
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{
		sqlDB:    sqlDB,
		cache:    appCache,
		cacheTTL: time.Duration(appCfg.CacheTTLSeconds) * time.Second, // Use appCfg here
		cfg:      appCfg, // Store the config
	}, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	if db.sqlDB != nil {
		return db.sqlDB.Close()
	}
	return nil
}

// NewWithSQLDB is a constructor for testing purposes, allowing injection of a custom *sql.DB and config.
// It also initializes a new cache with a default TTL for tests.
func NewWithSQLDB(sqlDb *sql.DB, appCfg *config.Config) *DB {
	testCache := cache.NewCache()
	var ttl time.Duration
	if appCfg != nil && appCfg.CacheTTLSeconds > 0 {
		ttl = time.Duration(appCfg.CacheTTLSeconds) * time.Second
	} else {
		ttl = 1 * time.Minute // Default test TTL
	}
	return &DB{sqlDB: sqlDb, cache: testCache, cacheTTL: ttl, cfg: appCfg}
}

// GetAllNewsgroups retrieves all newsgroups from the database, ordered by name.
// It uses a cache to store results.
func (db *DB) GetAllNewsgroups() ([]models.Newsgroup, error) {
	cacheKey := "newsgroups:all"
	if db.cache != nil {
		if cached, found := db.cache.Get(cacheKey); found {
			if groups, ok := cached.([]models.Newsgroup); ok {
				log.Println("Cache hit for GetAllNewsgroups")
				return groups, nil
			}
			log.Println("Cache data type mismatch for GetAllNewsgroups") // Should not happen if cache is used correctly
		}
	}
	log.Println("Cache miss for GetAllNewsgroups, querying DB")

	query := "SELECT id, name, description FROM `groups` ORDER BY name"
	rows, err := db.sqlDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var groups []models.Newsgroup
	for rows.Next() {
		var group models.Newsgroup
		if err := rows.Scan(&group.ID, &group.Name, &group.Description); err != nil {
			return nil, fmt.Errorf("failed to scan group row: %w", err)
		}
		groups = append(groups, group)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iteration error: %w", err)
	}

	if db.cache != nil && len(groups) > 0 { // Only cache if there are results
		db.cache.Set(cacheKey, groups, db.cacheTTL)
		log.Println("Cached result for GetAllNewsgroups")
	}
	return groups, nil
}

// GetMessagesForGroupMonth retrieves articles for a specific group and month/year.
// Articles are ordered by received date, descending. It uses a cache.
func (db *DB) GetMessagesForGroupMonth(groupName string, year int, month int) ([]models.Article, error) {
	cacheKey := fmt.Sprintf("groupmsgs:%s:%d-%02d", groupName, year, month)
	if db.cache != nil {
		if cached, found := db.cache.Get(cacheKey); found {
			if articles, ok := cached.([]models.Article); ok {
				log.Printf("Cache hit for GetMessagesForGroupMonth: %s", cacheKey)
				return articles, nil
			}
		}
	}
	log.Printf("Cache miss for GetMessagesForGroupMonth: %s, querying DB", cacheKey)

	var groupID uint16
	groupQuery := "SELECT id FROM `groups` WHERE name = ?"
	err := db.sqlDB.QueryRow(groupQuery, groupName).Scan(&groupID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("group '%s' not found: %w", groupName, err)
		}
		return nil, fmt.Errorf("failed to query group ID for '%s': %w", groupName, err)
	}

	articleQuery := `
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
		       received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND YEAR(received) = ? AND MONTH(received) = ?
		ORDER BY received DESC, id DESC`

	rows, err := db.sqlDB.Query(articleQuery, groupID, year, month)
	if err != nil {
		return nil, fmt.Errorf("failed to query articles for group '%s' (%d) year %d, month %d: %w", groupName, groupID, year, month, err)
	}
	defer rows.Close()

	var articles []models.Article
	for rows.Next() {
		var article models.Article
		article.GroupName = groupName // Populate GroupName
		var rawMsgID string
		if err := rows.Scan(
			&article.GroupID, &article.ArticleNum, &rawMsgID, &article.Subject,
			&article.From, &article.Date, &article.Received, &article.ThreadID,
			&article.ParentNum, &article.RawReferences, &article.Lines, &article.Bytes,
		); err != nil {
			log.Printf("Error scanning article row for group '%s': %v", groupName, err)
			// Decide on error handling: skip row or return error
			// return nil, fmt.Errorf("failed to scan article row: %w", err)
			continue // Skip problematic row
		}
		article.MessageID = strings.Trim(rawMsgID, "<>")
		article.References = models.ParseReferencesString(article.RawReferences)
		articles = append(articles, article)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iteration error while fetching articles for group '%s': %w", groupName, err)
	}

	if db.cache != nil && len(articles) > 0 { // Only cache if results were found
		db.cache.Set(cacheKey, articles, db.cacheTTL)
		log.Printf("Cached result for GetMessagesForGroupMonth: %s", cacheKey)
	}
	return articles, nil
}

// GetMinMaxMessageMonthsForGroup finds the earliest and latest month/year with messages for a group.
// Returns found=false if the group has no messages or does not exist. It uses a cache.
// Note: Caching complex return values (multiple return values) requires a struct or careful handling.
// For simplicity, we'll cache a struct here.
type MinMaxMonthsResult struct {
	MinYear int
	MinMonth int
	MaxYear int
	MaxMonth int
	Found bool
}

func (db *DB) GetMinMaxMessageMonthsForGroup(groupName string) (minYear, minMonth, maxYear, maxMonth int, found bool, err error) {
	cacheKey := fmt.Sprintf("groupminmax:%s", groupName)
	if db.cache != nil {
		if cached, foundCache := db.cache.Get(cacheKey); foundCache {
			if result, ok := cached.(MinMaxMonthsResult); ok {
				log.Printf("Cache hit for GetMinMaxMessageMonthsForGroup: %s", cacheKey)
				return result.MinYear, result.MinMonth, result.MaxYear, result.MaxMonth, result.Found, nil
			}
		}
	}
	log.Printf("Cache miss for GetMinMaxMessageMonthsForGroup: %s, querying DB", cacheKey)

	var groupID uint16
	groupQuery := "SELECT id FROM `groups` WHERE name = ?"
	err = db.sqlDB.QueryRow(groupQuery, groupName).Scan(&groupID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, 0, 0, 0, false, fmt.Errorf("group '%s' not found: %w", groupName, err)
		}
		return 0, 0, 0, 0, false, fmt.Errorf("failed to query group ID for '%s': %w", groupName, err)
	}

	// query := ` // This query was unused after switching to MIN(received), MAX(received)
	// 	SELECT
	// 		MIN(YEAR(received)), MIN(MONTH(received)),
	// 		MAX(YEAR(received)), MAX(MONTH(received))
	// 	FROM articles
	// 	WHERE group_id = ?`

	// Need to handle NULL if no rows, sql.NullInt32 or similar, or check count first.
	// Simpler: check if any messages exist for the group first.
	var count int
	countQuery := "SELECT COUNT(*) FROM articles WHERE group_id = ?"
	err = db.sqlDB.QueryRow(countQuery, groupID).Scan(&count)
	if err != nil {
		return 0,0,0,0, false, fmt.Errorf("failed to count messages for group %d: %w", groupID, err)
	}
	if count == 0 {
		return 0,0,0,0, false, nil // No messages in group, found = false
	}

	// Since MySQL MIN/MAX on date parts won't give the correct month for MIN(YEAR(received)) if it's not Jan.
	// It's better to get MIN(received) and MAX(received) directly.
	var minDate, maxDate time.Time
	dateQuery := "SELECT MIN(received), MAX(received) FROM articles WHERE group_id = ?"
	err = db.sqlDB.QueryRow(dateQuery, groupID).Scan(&minDate, &maxDate)
	if err != nil {
		// This should not happen if count > 0, but handle defensively.
		return 0,0,0,0, false, fmt.Errorf("failed to get min/max dates for group %d: %w", groupID, err)
	}

	result := MinMaxMonthsResult{
		MinYear: minDate.Year(), MinMonth: int(minDate.Month()),
		MaxYear: maxDate.Year(), MaxMonth: int(maxDate.Month()),
		Found: true,
	}
	if db.cache != nil {
		db.cache.Set(cacheKey, result, db.cacheTTL)
		log.Printf("Cached result for GetMinMaxMessageMonthsForGroup: %s", cacheKey)
	}
	return result.MinYear, result.MinMonth, result.MaxYear, result.MaxMonth, result.Found, nil
}

// CachedMonthNavResult stores result for prev/next month navigation functions.
type CachedMonthNavResult struct {
	Year int
	Month int
	Found bool
}

// GetPrevMonthWithMessages finds the nearest previous month with messages for the group. It uses a cache.
func (db *DB) GetPrevMonthWithMessages(groupName string, currentYear, currentMonth int) (prevYear, prevMonth int, found bool, err error) {
	cacheKey := fmt.Sprintf("prevmonth:%s:%d-%02d", groupName, currentYear, currentMonth)
	if db.cache != nil {
		if cached, foundCache := db.cache.Get(cacheKey); foundCache {
			if result, ok := cached.(CachedMonthNavResult); ok {
				log.Printf("Cache hit for GetPrevMonthWithMessages: %s", cacheKey)
				return result.Year, result.Month, result.Found, nil
			}
		}
	}
	log.Printf("Cache miss for GetPrevMonthWithMessages: %s, querying DB", cacheKey)

	var groupID uint16
	groupQuery := "SELECT id FROM `groups` WHERE name = ?"
	err = db.sqlDB.QueryRow(groupQuery, groupName).Scan(&groupID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, 0, false, fmt.Errorf("group '%s' not found: %w", groupName, err)
		}
		return 0, 0, false, fmt.Errorf("failed to query group ID for '%s': %w", groupName, err)
	}

	// Target date represents the first day of the current month
	targetDate := time.Date(currentYear, time.Month(currentMonth), 1, 0, 0, 0, 0, time.UTC)

	query := `
		SELECT YEAR(received), MONTH(received)
		FROM articles
		WHERE group_id = ? AND received < ?
		ORDER BY received DESC
		LIMIT 1`

	var pYear, pMonth int
	err = db.sqlDB.QueryRow(query, groupID, targetDate).Scan(&pYear, &pMonth)
	if err != nil {
		if err == sql.ErrNoRows {
			if db.cache != nil {
				db.cache.Set(cacheKey, CachedMonthNavResult{Found: false}, db.cacheTTL)
			}
			return 0, 0, false, nil // No previous month with messages
		}
		return 0, 0, false, fmt.Errorf("failed to find previous month for group %d: %w", groupID, err)
	}
	if db.cache != nil {
		db.cache.Set(cacheKey, CachedMonthNavResult{Year: pYear, Month: pMonth, Found: true}, db.cacheTTL)
	}
	return pYear, pMonth, true, nil
}

// GetNextMonthWithMessages finds the nearest next month with messages for the group. It uses a cache.
func (db *DB) GetNextMonthWithMessages(groupName string, currentYear, currentMonth int) (nextYear, nextMonth int, found bool, err error) {
	cacheKey := fmt.Sprintf("nextmonth:%s:%d-%02d", groupName, currentYear, currentMonth)
	if db.cache != nil {
		if cached, foundCache := db.cache.Get(cacheKey); foundCache {
			if result, ok := cached.(CachedMonthNavResult); ok {
				log.Printf("Cache hit for GetNextMonthWithMessages: %s", cacheKey)
				return result.Year, result.Month, result.Found, nil
			}
		}
	}
	log.Printf("Cache miss for GetNextMonthWithMessages: %s, querying DB", cacheKey)

	var groupID uint16
	groupQuery := "SELECT id FROM `groups` WHERE name = ?"
	err = db.sqlDB.QueryRow(groupQuery, groupName).Scan(&groupID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, 0, false, fmt.Errorf("group '%s' not found: %w", groupName, err)
		}
		return 0, 0, false, fmt.Errorf("failed to query group ID for '%s': %w", groupName, err)
	}

	// Target date represents the first day of the *next* month relative to current view
	targetDate := time.Date(currentYear, time.Month(currentMonth), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)

	query := `
		SELECT YEAR(received), MONTH(received)
		FROM articles
		WHERE group_id = ? AND received >= ?
		ORDER BY received ASC
		LIMIT 1`

	var nYear, nMonth int
	err = db.sqlDB.QueryRow(query, groupID, targetDate).Scan(&nYear, &nMonth)
	if err != nil {
		if err == sql.ErrNoRows {
			if db.cache != nil {
				db.cache.Set(cacheKey, CachedMonthNavResult{Found: false}, db.cacheTTL)
			}
			return 0, 0, false, nil // No next month with messages
		}
		return 0, 0, false, fmt.Errorf("failed to find next month for group %d: %w", groupID, err)
	}
	if db.cache != nil {
		db.cache.Set(cacheKey, CachedMonthNavResult{Year: nYear, Month: nMonth, Found: true}, db.cacheTTL)
	}
	return nYear, nMonth, true, nil
}

// CachedLatestMonthResult stores result for GetLatestMessageMonthForGroup.
type CachedLatestMonthResult struct {
	Year  int
	Month int
	Found bool
}

// GetLatestMessageMonthForGroup finds the most recent year and month with messages for a group.
// Returns found=false if the group has no messages or does not exist. It uses a cache.
func (db *DB) GetLatestMessageMonthForGroup(groupName string) (year, month int, found bool) {
	cacheKey := fmt.Sprintf("latestmonth:%s", groupName)
	if db.cache != nil {
		if cached, foundCache := db.cache.Get(cacheKey); foundCache {
			if result, ok := cached.(CachedLatestMonthResult); ok {
				log.Printf("Cache hit for GetLatestMessageMonthForGroup: %s", cacheKey)
				return result.Year, result.Month, result.Found
			}
		}
	}
	log.Printf("Cache miss for GetLatestMessageMonthForGroup: %s, querying DB", cacheKey)

	var groupID uint16
	groupQuery := "SELECT id FROM `groups` WHERE name = ?"
	err := db.sqlDB.QueryRow(groupQuery, groupName).Scan(&groupID)
	if err != nil {
		if err == sql.ErrNoRows { // Group not found
			log.Printf("Group '%s' not found in GetLatestMessageMonthForGroup.", groupName)
			if db.cache != nil {
				db.cache.Set(cacheKey, CachedLatestMonthResult{Found: false}, db.cacheTTL)
			}
			return 0, 0, false
		}
		log.Printf("Error querying group ID for '%s' in GetLatestMessageMonthForGroup: %v", groupName, err)
		// For other DB errors, we don't cache and let the error propagate implicitly by returning false
		return 0, 0, false
	}

	query := `
		SELECT YEAR(received), MONTH(received)
		FROM articles
		WHERE group_id = ?
		ORDER BY received DESC
		LIMIT 1`

	var y, m int
	err = db.sqlDB.QueryRow(query, groupID).Scan(&y, &m)
	if err != nil {
		if err == sql.ErrNoRows { // No messages in the group
			log.Printf("No messages found for group '%s' (ID: %d) in GetLatestMessageMonthForGroup.", groupName, groupID)
			if db.cache != nil {
				db.cache.Set(cacheKey, CachedLatestMonthResult{Found: false}, db.cacheTTL)
			}
			return 0, 0, false
		}
		// For other DB errors, don't cache, return found=false
		log.Printf("Error querying latest month for group '%s' (ID: %d): %v", groupName, groupID, err)
		return 0, 0, false
	}

	if db.cache != nil {
		db.cache.Set(cacheKey, CachedLatestMonthResult{Year: y, Month: m, Found: true}, db.cacheTTL)
		log.Printf("Cached result for GetLatestMessageMonthForGroup: %s (Year: %d, Month: %d)", cacheKey, y, m)
	}
	return y, m, true
}


// GetArticleByDetails retrieves a specific article by its group name, year, month, and article number (articles.id).
// It also populates the Article.GroupName field. It uses a cache.
func (db *DB) GetArticleByDetails(groupName string, year int, month int, articleNum uint32) (*models.Article, error) {
	cacheKey := fmt.Sprintf("article:%s:%d-%02d:%d", groupName, year, month, articleNum)
	if db.cache != nil {
		if cached, found := db.cache.Get(cacheKey); found {
			if article, ok := cached.(*models.Article); ok { // Note: pointer type
				log.Printf("Cache hit for GetArticleByDetails: %s", cacheKey)
				return article, nil
			}
		}
	}
	log.Printf("Cache miss for GetArticleByDetails: %s, querying DB", cacheKey)

	var groupID uint16
	groupQuery := "SELECT id FROM `groups` WHERE name = ?"
	err := db.sqlDB.QueryRow(groupQuery, groupName).Scan(&groupID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("group '%s' not found for article lookup: %w", groupName, err)
		}
		return nil, fmt.Errorf("failed to query group ID for '%s': %w", groupName, err)
	}

	// Note: The schema uses articles.id as the article number within a group.
	// The date components in the query are to ensure we are specific, though 'id' within a group should be unique.
	// However, the problem implies year/month might be part of canonical URL, so we check against `received`.
	articleQuery := `
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
		       received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND id = ?
		  AND YEAR(received) = ? AND MONTH(received) = ?
		LIMIT 1`
	// We might also want a variant that only uses group_id and id if year/month in URL might be wrong.
	// The handler will decide if a redirect is needed based on `article.Received` vs URL year/month.

	var article models.Article
	article.GroupName = groupName
	var rawMsgID string

	err = db.sqlDB.QueryRow(articleQuery, groupID, articleNum, year, month).Scan(
		&article.GroupID, &article.ArticleNum, &rawMsgID, &article.Subject,
		&article.From, &article.Date, &article.Received, &article.ThreadID,
		&article.ParentNum, &article.RawReferences, &article.Lines, &article.Bytes,
	)
	if err == nil {
		article.MessageID = strings.Trim(rawMsgID, "<>")
		article.References = models.ParseReferencesString(article.RawReferences)
	} else if err == sql.ErrNoRows {
		// Attempt to find the article by group_id and articleNum only, to check if year/month were wrong
		relaxedArticleQuery := `
			SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
					received, thread_id, parent, h_references, h_lines, h_bytes
			FROM articles
			WHERE group_id = ? AND id = ?
			LIMIT 1`
		var relaxedRawMsgID string
		// article struct is reused, GroupName is already set.
		errRelaxed := db.sqlDB.QueryRow(relaxedArticleQuery, groupID, articleNum).Scan(
			&article.GroupID, &article.ArticleNum, &relaxedRawMsgID, &article.Subject,
			&article.From, &article.Date, &article.Received, &article.ThreadID,
			&article.ParentNum, &article.RawReferences, &article.Lines, &article.Bytes,
		)
		if errRelaxed == nil {
			article.MessageID = strings.Trim(relaxedRawMsgID, "<>")
			article.References = models.ParseReferencesString(article.RawReferences)
			// This is the article to return (for redirect), cache it under the original key
			if db.cache != nil {
				db.cache.Set(cacheKey, &article, db.cacheTTL)
				log.Printf("Cached result (after relaxed query) for GetArticleByDetails: %s", cacheKey)
			}
			return &article, nil
		}
		// If still not found, then it's a genuine ErrNoRows for this group/articleNum combination.
		return nil, fmt.Errorf("article num %d in group '%s' (year %d, month %d) not found: %w", articleNum, groupName, year, month, sql.ErrNoRows)
	} else { // Other error from the initial query
		return nil, fmt.Errorf("failed to query article num %d in group '%s': %w", articleNum, groupName, err)
	}
	// This part is reached if the first query succeeded.
	if db.cache != nil {
		db.cache.Set(cacheKey, &article, db.cacheTTL)
		log.Printf("Cached result for GetArticleByDetails: %s", cacheKey)
	}
	return &article, nil
}


// GetArticleByMessageID retrieves a specific article by its h_messageid (full Message-ID).
// It also populates the Article.GroupName field. It uses a cache.
func (db *DB) GetArticleByMessageID(messageIDVal string) (*models.Article, error) {
	cacheKey := fmt.Sprintf("article:msgid:%s", messageIDVal)
	if db.cache != nil {
		if cached, found := db.cache.Get(cacheKey); found {
			if article, ok := cached.(*models.Article); ok {
				log.Printf("Cache hit for GetArticleByMessageID: %s", cacheKey)
				return article, nil
			}
		}
	}
	log.Printf("Cache miss for GetArticleByMessageID: %s, querying DB", cacheKey)

	articleQuery := `
		SELECT a.group_id, a.id, a.h_messageid, a.h_subject, a.h_from, a.h_date,
		       a.received, a.thread_id, a.parent, a.h_references, a.h_lines, a.h_bytes,
		       g.name as group_name
		FROM articles a
		JOIN ` + "`groups`" + ` g ON a.group_id = g.id
		WHERE a.h_messageid = ?
		LIMIT 1`

	var article models.Article
	var rawMsgID string // To scan h_messageid which includes <>
	err := db.sqlDB.QueryRow(articleQuery, messageIDVal).Scan(
		&article.GroupID, &article.ArticleNum, &rawMsgID, &article.Subject,
		&article.From, &article.Date, &article.Received, &article.ThreadID,
		&article.ParentNum, &article.RawReferences, &article.Lines, &article.Bytes,
		&article.GroupName,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("article with Message-ID '%s' not found: %w", messageIDVal, err)
		}
		return nil, fmt.Errorf("failed to query article by Message-ID '%s': %w", messageIDVal, err)
	}
	article.MessageID = strings.Trim(rawMsgID, "<>")
	article.References = models.ParseReferencesString(article.RawReferences)

	if db.cache != nil {
		db.cache.Set(cacheKey, &article, db.cacheTTL)
		log.Printf("Cached result for GetArticleByMessageID: %s", cacheKey)
	}
	return &article, nil
}

// GetThreadMessages retrieves all messages in the same thread.
// It includes the article that might be the "current" one if it's part of the thread.
// Articles are ordered by their received date. It also populates the Article.GroupName field. It uses a cache.
func (db *DB) GetThreadMessages(threadID uint32) ([]models.Article, error) { // currentArticleGroupID and currentArticleNum removed
	// Cache key updated to reflect fetching the full thread for a given threadID
	cacheKey := fmt.Sprintf("threadmsgs:full:%d", threadID)
	if db.cache != nil {
		if cached, found := db.cache.Get(cacheKey); found {
			if articles, ok := cached.([]models.Article); ok {
				log.Printf("Cache hit for GetThreadMessages (full thread): %s", cacheKey)
				return articles, nil
			}
		}
	}
	log.Printf("Cache miss for GetThreadMessages (full thread): %s, querying DB", cacheKey)

	query := `
		SELECT a.group_id, a.id, a.h_messageid, a.h_subject, a.h_from, a.h_date,
		       a.received, a.thread_id, a.parent, a.h_references, a.h_lines, a.h_bytes,
		       g.name as group_name
		FROM articles a
		JOIN ` + "`groups` g ON a.group_id = g.id" + `
		WHERE a.thread_id = ?
		ORDER BY a.received ASC, a.id ASC` // Order by date, then by article number for tie-breaking

	if db.cfg != nil && db.cfg.LogSQLQueries {
		log.Printf("DB_QUERY: GetThreadMessages - SQL: %s - Args: [%d]", strings.ReplaceAll(strings.TrimSpace(query), "\n", " "), threadID)
	}

	rows, err := db.sqlDB.Query(query, threadID) // currentArticleGroupID and currentArticleNum removed from query parameters
	if err != nil {
		return nil, fmt.Errorf("failed to query thread messages for thread_id %d: %w", threadID, err)
	}
	defer rows.Close()

	var articles []models.Article
	for rows.Next() {
		var article models.Article
		var rawMsgID string
		if err := rows.Scan(
			&article.GroupID, &article.ArticleNum, &rawMsgID, &article.Subject,
			&article.From, &article.Date, &article.Received, &article.ThreadID,
			&article.ParentNum, &article.RawReferences, &article.Lines, &article.Bytes,
			&article.GroupName,
		); err != nil {
			log.Printf("Error scanning article row for thread_id %d: %v", threadID, err)
			continue
		}
		article.MessageID = strings.Trim(rawMsgID, "<>")
		article.References = models.ParseReferencesString(article.RawReferences)
		articles = append(articles, article)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iteration error while fetching thread messages for thread_id %d: %w", threadID, err)
	}

	if db.cache != nil && len(articles) > 0 {
		db.cache.Set(cacheKey, articles, db.cacheTTL)
		log.Printf("Cached result for GetThreadMessages: %s", cacheKey)
	}
	return articles, nil
}

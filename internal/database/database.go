package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"nntp-web/internal/config"
	"nntp-web/internal/models"
	"nntp-web/internal/cache" // Added for cache

	_ "github.com/go-sql-driver/mysql" // MySQL driver
)

// DB wraps the sql.DB connection pool and an optional cache.
type DB struct {
	sqlDB    *sql.DB
	cache    *cache.Cache
	cacheTTL time.Duration
}

// New creates a new DB instance and connects to the database.
// It also initializes it with the provided cache and TTL.
func New(cfg *config.Config, appCache *cache.Cache) (*DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4",
		cfg.DBUser,
		cfg.DBPass,
		cfg.DBHost,
		cfg.DBPort,
		cfg.DBName,
	)

	log.Printf("Connecting to database: %s@tcp(%s:%d)/%s", cfg.DBUser, cfg.DBHost, cfg.DBPort, cfg.DBName)

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
		cacheTTL: time.Duration(cfg.CacheTTLSeconds) * time.Second,
	}, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	if db.sqlDB != nil {
		return db.sqlDB.Close()
	}
	return nil
}

// NewWithSQLDB is a constructor for testing purposes, allowing injection of a custom *sql.DB.
// It also initializes a new cache with a default TTL for tests.
func NewWithSQLDB(sqlDb *sql.DB) *DB {
	testCache := cache.NewCache()
	// Use a short, predictable TTL for testing, or make it configurable if needed for specific cache tests.
	return &DB{sqlDB: sqlDb, cache: testCache, cacheTTL: 1 * time.Minute}
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
		if err := rows.Scan(
			&article.GroupID, &article.ArticleNum, &article.MessageID, &article.Subject,
			&article.From, &article.Date, &article.Received, &article.ThreadID,
			&article.ParentNum, &article.RawReferences, &article.Lines, &article.Bytes,
		); err != nil {
			log.Printf("Error scanning article row for group '%s': %v", groupName, err)
			// Decide on error handling: skip row or return error
			// return nil, fmt.Errorf("failed to scan article row: %w", err)
			continue // Skip problematic row
		}
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
	article.GroupName = groupName // Populate GroupName

	err = db.sqlDB.QueryRow(articleQuery, groupID, articleNum, year, month).Scan(
		&article.GroupID, &article.ArticleNum, &article.MessageID, &article.Subject,
		&article.From, &article.Date, &article.Received, &article.ThreadID,
		&article.ParentNum, &article.RawReferences, &article.Lines, &article.Bytes,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			// Attempt to find the article by group_id and articleNum only, to check if year/month were wrong
			relaxedArticleQuery := `
				SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
					   received, thread_id, parent, h_references, h_lines, h_bytes
				FROM articles
				WHERE group_id = ? AND id = ?
				LIMIT 1`
			var relaxedArticle models.Article
			relaxedArticle.GroupName = groupName
			errRelaxed := db.sqlDB.QueryRow(relaxedArticleQuery, groupID, articleNum).Scan(
				&relaxedArticle.GroupID, &relaxedArticle.ArticleNum, &relaxedArticle.MessageID, &relaxedArticle.Subject,
				&relaxedArticle.From, &relaxedArticle.Date, &relaxedArticle.Received, &relaxedArticle.ThreadID,
				&relaxedArticle.ParentNum, &relaxedArticle.RawReferences, &relaxedArticle.Lines, &relaxedArticle.Bytes,
			)
			if errRelaxed == nil {
				relaxedArticle.References = models.ParseReferencesString(relaxedArticle.RawReferences)
				return &relaxedArticle, nil
			}
			// If still not found, then it's a genuine ErrNoRows for this group/articleNum combination.
			return nil, fmt.Errorf("article num %d in group '%s' (year %d, month %d) not found: %w", articleNum, groupName, year, month, sql.ErrNoRows)
		}
		return nil, fmt.Errorf("failed to query article num %d in group '%s': %w", articleNum, groupName, err)
	}
	article.References = models.ParseReferencesString(article.RawReferences)

	if db.cache != nil {
		db.cache.Set(cacheKey, &article, db.cacheTTL) // Cache the pointer
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
	err := db.sqlDB.QueryRow(articleQuery, messageIDVal).Scan(
		&article.GroupID, &article.ArticleNum, &article.MessageID, &article.Subject,
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
	article.References = models.ParseReferencesString(article.RawReferences)

	if db.cache != nil {
		db.cache.Set(cacheKey, &article, db.cacheTTL)
		log.Printf("Cached result for GetArticleByMessageID: %s", cacheKey)
	}
	return &article, nil
}

// GetThreadMessages retrieves all messages in the same thread as a given article,
// excluding the article itself. Articles are ordered by their received date.
// It also populates the Article.GroupName field. It uses a cache.
func (db *DB) GetThreadMessages(threadID uint32, currentArticleGroupID uint16, currentArticleNum uint32) ([]models.Article, error) {
	cacheKey := fmt.Sprintf("threadmsgs:%d:%d-%d", threadID, currentArticleGroupID, currentArticleNum)
	if db.cache != nil {
		if cached, found := db.cache.Get(cacheKey); found {
			if articles, ok := cached.([]models.Article); ok {
				log.Printf("Cache hit for GetThreadMessages: %s", cacheKey)
				return articles, nil
			}
		}
	}
	log.Printf("Cache miss for GetThreadMessages: %s, querying DB", cacheKey)

	query := `
		SELECT a.group_id, a.id, a.h_messageid, a.h_subject, a.h_from, a.h_date,
		       a.received, a.thread_id, a.parent, a.h_references, a.h_lines, a.h_bytes,
		       g.name as group_name
		FROM articles a
		JOIN ` + "`groups`" + ` g ON a.group_id = g.id
		WHERE a.thread_id = ? AND NOT (a.group_id = ? AND a.id = ?)
		ORDER BY a.received ASC, a.id ASC` // Order by date, then by article number for tie-breaking

	rows, err := db.sqlDB.Query(query, threadID, currentArticleGroupID, currentArticleNum)
	if err != nil {
		return nil, fmt.Errorf("failed to query thread messages for thread_id %d: %w", threadID, err)
	}
	defer rows.Close()

	var articles []models.Article
	for rows.Next() {
		var article models.Article
		if err := rows.Scan(
			&article.GroupID, &article.ArticleNum, &article.MessageID, &article.Subject,
			&article.From, &article.Date, &article.Received, &article.ThreadID,
			&article.ParentNum, &article.RawReferences, &article.Lines, &article.Bytes,
			&article.GroupName,
		); err != nil {
			log.Printf("Error scanning article row for thread_id %d: %v", threadID, err)
			continue
		}
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

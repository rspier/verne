package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"nntp-web/internal/config"
	"nntp-web/internal/models"
	"nntp-web/internal/cache" // Added for cache
	"strings" // Added

	_ "github.com/go-sql-driver/mysql" // MySQL driver

	"context" // Added for OTel
	// "go.opentelemetry.io/contrib/instrumentation/database/sql/otelsql" // Commented out due to resolution issues
	"go.opentelemetry.io/otel"                        // Added for OTel tracer
	"go.opentelemetry.io/otel/attribute"              // Added for OTel attributes
	"go.opentelemetry.io/otel/trace"                  // Added for trace.WithAttributes
	// semconv "go.opentelemetry.io/otel/semconv/v1.21.0" // No longer used directly here after commenting out otelsql
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

	var err error
	var sqlDB *sql.DB

	// Defaulting to standard sql.Open due to otelsql issues.
	sqlDB, err = sql.Open("mysql", dsn)
	if appCfg.OTelMetricsEnabled || appCfg.OTelExporterOTLPTracesEndpoint != "" {
		log.Println("OTel is enabled, but database connection will use standard sql.Open due to otelsql package issues. Some DB metrics/spans may be missing.")
	} else {
		log.Println("Database connection using standard sql.Open (OTel disabled or otelsql issue).")
	}


	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	if err = sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{
		sqlDB:    sqlDB,
		cache:    appCache,
		cacheTTL: time.Duration(appCfg.CacheTTLSeconds) * time.Second,
		cfg:      appCfg,
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
func NewWithSQLDB(sqlDb *sql.DB, appCfg *config.Config) *DB {
	testCache := cache.NewCache()
	var ttl time.Duration
	if appCfg != nil && appCfg.CacheTTLSeconds > 0 {
		ttl = time.Duration(appCfg.CacheTTLSeconds) * time.Second
	} else {
		ttl = 1 * time.Minute
	}
	return &DB{sqlDB: sqlDb, cache: testCache, cacheTTL: ttl, cfg: appCfg}
}

func (db *DB) GetAllNewsgroups(ctx context.Context) ([]models.Newsgroup, error) {
	_, span := otel.Tracer("nntp-web/internal/database").Start(ctx, "DB.GetAllNewsgroups")
	defer span.End()

	cacheKey := "newsgroups:all"
	if db.cache != nil {
		if cached, found := db.cache.Get(ctx, cacheKey); found {
			if groups, ok := cached.([]models.Newsgroup); ok {
				log.Println("Cache hit for GetAllNewsgroups")
				span.SetAttributes(attribute.Bool("cache.hit", true))
				return groups, nil
			}
			log.Println("Cache data type mismatch for GetAllNewsgroups")
		}
	}
	log.Println("Cache miss for GetAllNewsgroups, querying DB")
	span.SetAttributes(attribute.Bool("cache.hit", false))

	query := `
		SELECT g.id, g.name, g.description, last_post.max_received_date
		FROM ` + "`groups`" + ` g
		LEFT JOIN (SELECT group_id, MAX(received) as max_received_date FROM articles GROUP BY group_id) last_post
		ON g.id = last_post.group_id
		ORDER BY g.name`
	rows, err := db.sqlDB.QueryContext(ctx, query)
	if err != nil {
		err = fmt.Errorf("query for newsgroups with stats failed: %w", err)
		span.RecordError(err)
		return nil, err
	}
	defer rows.Close()

	var groups []models.Newsgroup
	for rows.Next() {
		var group models.Newsgroup
		var lastPostNullTime sql.NullTime
		if err := rows.Scan(&group.ID, &group.Name, &group.Description, &lastPostNullTime); err != nil {
			scanErr := fmt.Errorf("failed to scan group row with last post date: %w", err)
			span.RecordError(scanErr)
			return nil, scanErr
		}
		if lastPostNullTime.Valid {
			group.LastPostDate = &lastPostNullTime.Time
		}
		groups = append(groups, group)
	}

	if iterErr := rows.Err(); iterErr != nil {
		err = fmt.Errorf("iteration error for newsgroups: %w", iterErr)
		span.RecordError(err)
		return nil, err
	}

	if db.cache != nil && len(groups) > 0 {
		db.cache.Set(ctx, cacheKey, groups, db.cacheTTL)
		log.Println("Cached result for GetAllNewsgroups")
	}
	return groups, nil
}

func (db *DB) GetMessagesForGroupMonth(ctx context.Context, groupName string, year int, month int) ([]models.Article, error) {
	_, span := otel.Tracer("nntp-web/internal/database").Start(ctx, "DB.GetMessagesForGroupMonth",
		trace.WithAttributes(
			attribute.String("group.name", groupName),
			attribute.Int("year", year),
			attribute.Int("month", month),
		),
	)
	defer span.End()

	cacheKey := fmt.Sprintf("groupmsgs:%s:%d-%02d", groupName, year, month)
	if db.cache != nil {
		if cached, found := db.cache.Get(ctx, cacheKey); found {
			if articles, ok := cached.([]models.Article); ok {
				log.Printf("Cache hit for GetMessagesForGroupMonth: %s", cacheKey)
				span.SetAttributes(attribute.Bool("cache.hit", true))
				return articles, nil
			}
		}
	}
	log.Printf("Cache miss for GetMessagesForGroupMonth: %s, querying DB", cacheKey)
	span.SetAttributes(attribute.Bool("cache.hit", false))

	var groupID uint16
	groupQuery := "SELECT id FROM `groups` WHERE name = ?"
	err := db.sqlDB.QueryRowContext(ctx, groupQuery, groupName).Scan(&groupID)
	if err != nil {
		if err == sql.ErrNoRows {
			err = fmt.Errorf("group '%s' not found: %w", groupName, err)
		} else {
			err = fmt.Errorf("failed to query group ID for '%s': %w", groupName, err)
		}
		span.RecordError(err)
		return nil, err
	}
	span.SetAttributes(attribute.Int("group.id", int(groupID)))

	articleQuery := `
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
		       received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND YEAR(received) = ? AND MONTH(received) = ?
		ORDER BY received DESC, id DESC`

	rows, err := db.sqlDB.QueryContext(ctx, articleQuery, groupID, year, month)
	if err != nil {
		err = fmt.Errorf("failed to query articles for group '%s' (%d) year %d, month %d: %w", groupName, groupID, year, month, err)
		span.RecordError(err)
		return nil, err
	}
	defer rows.Close()

	var articles []models.Article
	for rows.Next() {
		var article models.Article
		article.GroupName = groupName
		var rawMsgID string
		if scanErr := rows.Scan(
			&article.GroupID, &article.ArticleNum, &rawMsgID, &article.Subject,
			&article.From, &article.Date, &article.Received, &article.ThreadID,
			&article.ParentNum, &article.RawReferences, &article.Lines, &article.Bytes,
		); scanErr != nil {
			log.Printf("Error scanning article row for group '%s': %v", groupName, scanErr)
			span.AddEvent("Row scan error", trace.WithAttributes(attribute.String("error.message", scanErr.Error())))
			continue
		}
		article.MessageID = strings.Trim(rawMsgID, "<>")
		article.References = models.ParseReferencesString(article.RawReferences)
		articles = append(articles, article)
	}

	if iterErr := rows.Err(); iterErr != nil {
		err = fmt.Errorf("iteration error while fetching articles for group '%s': %w", groupName, iterErr)
		span.RecordError(err)
		return nil, err
	}

	if db.cache != nil && len(articles) > 0 {
		db.cache.Set(ctx, cacheKey, articles, db.cacheTTL)
		log.Printf("Cached result for GetMessagesForGroupMonth: %s", cacheKey)
	}
	return articles, nil
}

type CachedGroupActivityStats struct {
	AvgPostsPerDay    float64
	TotalPostsInPeriod int
	HasRecentActivity bool
}

func (db *DB) GetGroupActivityStats(ctx context.Context, groupID uint16, lookbackDays int) (avgPosts float64, totalPostsInPeriod int, hasRecentActivity bool, err error) {
	_, span := otel.Tracer("nntp-web/internal/database").Start(ctx, "DB.GetGroupActivityStats",
		trace.WithAttributes(
			attribute.Int("group.id", int(groupID)),
			attribute.Int("lookback.days", lookbackDays),
		),
	)
	defer span.End()

	cacheKey := fmt.Sprintf("groupactivity:%d:%ddays", groupID, lookbackDays)
	if db.cache != nil {
		if cached, found := db.cache.Get(ctx, cacheKey); found {
			if cachedStats, ok := cached.(CachedGroupActivityStats); ok {
				log.Printf("Cache hit for GetGroupActivityStats: %s", cacheKey)
				span.SetAttributes(attribute.Bool("cache.hit", true))
				return cachedStats.AvgPostsPerDay, cachedStats.TotalPostsInPeriod, cachedStats.HasRecentActivity, nil
			}
		}
	}
	log.Printf("Cache miss for GetGroupActivityStats: %s, querying DB", cacheKey)
	span.SetAttributes(attribute.Bool("cache.hit", false))

	if lookbackDays <= 0 {
		err = fmt.Errorf("lookbackDays must be positive, got %d", lookbackDays)
		span.RecordError(err)
		return
	}

	query := `
		SELECT COUNT(*)
		FROM articles
		WHERE group_id = ? AND received >= DATE_SUB(NOW(), INTERVAL ? DAY)`

	var postCount int
	dbErr := db.sqlDB.QueryRowContext(ctx, query, groupID, lookbackDays).Scan(&postCount)
	if dbErr != nil {
		if dbErr != sql.ErrNoRows {
			err = fmt.Errorf("failed to count recent articles for group %d: %w", groupID, dbErr)
			span.RecordError(err)
			return
		}
		postCount = 0
	}
	span.SetAttributes(attribute.Int("posts.count", postCount))
	totalPostsInPeriod = postCount
	if totalPostsInPeriod > 0 {
		hasRecentActivity = true
		avgPosts = float64(totalPostsInPeriod) / float64(lookbackDays)
	} else {
		hasRecentActivity = false
		avgPosts = 0.0
	}

	if db.cache != nil {
		cachedData := CachedGroupActivityStats{
			AvgPostsPerDay:    avgPosts,
			TotalPostsInPeriod: totalPostsInPeriod,
			HasRecentActivity: hasRecentActivity,
		}
		db.cache.Set(ctx, cacheKey, cachedData, 1*time.Hour)
		log.Printf("Cached result for GetGroupActivityStats: %s", cacheKey)
	}
	return
}

type MinMaxMonthsResult struct {
	MinYear int; MinMonth int; MaxYear int; MaxMonth int; Found bool
}

func (db *DB) GetMinMaxMessageMonthsForGroup(ctx context.Context, groupName string) (minYear, minMonth, maxYear, maxMonth int, found bool, err error) {
	_, span := otel.Tracer("nntp-web/internal/database").Start(ctx, "DB.GetMinMaxMessageMonthsForGroup",
		trace.WithAttributes(attribute.String("group.name", groupName)),
	)
	defer span.End()

	cacheKey := fmt.Sprintf("groupminmax:%s", groupName)
	if db.cache != nil {
		if cached, foundCache := db.cache.Get(ctx, cacheKey); foundCache {
			if result, ok := cached.(MinMaxMonthsResult); ok {
				log.Printf("Cache hit for GetMinMaxMessageMonthsForGroup: %s", cacheKey)
				span.SetAttributes(attribute.Bool("cache.hit", true))
				return result.MinYear, result.MinMonth, result.MaxYear, result.MaxMonth, result.Found, nil
			}
		}
	}
	log.Printf("Cache miss for GetMinMaxMessageMonthsForGroup: %s, querying DB", cacheKey)
	span.SetAttributes(attribute.Bool("cache.hit", false))

	var groupID uint16
	groupQuery := "SELECT id FROM `groups` WHERE name = ?"
	err = db.sqlDB.QueryRowContext(ctx, groupQuery, groupName).Scan(&groupID)
	if err != nil {
		if err == sql.ErrNoRows { err = fmt.Errorf("group '%s' not found: %w", groupName, err)
		} else { err = fmt.Errorf("failed to query group ID for '%s': %w", groupName, err) }
		span.RecordError(err); return 0, 0, 0, 0, false, err
	}
	span.SetAttributes(attribute.Int("group.id", int(groupID)))

	var count int
	countQuery := "SELECT COUNT(*) FROM articles WHERE group_id = ?"
	err = db.sqlDB.QueryRowContext(ctx, countQuery, groupID).Scan(&count)
	if err != nil {
		err = fmt.Errorf("failed to count messages for group %d: %w", groupID, err)
		span.RecordError(err); return 0, 0, 0, 0, false, err
	}
	span.SetAttributes(attribute.Int("messages.count", count))
	if count == 0 {
		if db.cache != nil { db.cache.Set(ctx, cacheKey, MinMaxMonthsResult{Found: false}, db.cacheTTL) }
		return 0, 0, 0, 0, false, nil
	}

	var minDate, maxDate time.Time
	dateQuery := "SELECT MIN(received), MAX(received) FROM articles WHERE group_id = ?"
	err = db.sqlDB.QueryRowContext(ctx, dateQuery, groupID).Scan(&minDate, &maxDate)
	if err != nil {
		err = fmt.Errorf("failed to get min/max dates for group %d: %w", groupID, err)
		span.RecordError(err); return 0, 0, 0, 0, false, err
	}

	result := MinMaxMonthsResult{
		MinYear:  minDate.Year(), MinMonth: int(minDate.Month()),
		MaxYear:  maxDate.Year(), MaxMonth: int(maxDate.Month()),
		Found:    true,
	}
	if db.cache != nil {
		db.cache.Set(ctx, cacheKey, result, db.cacheTTL)
		log.Printf("Cached result for GetMinMaxMessageMonthsForGroup: %s", cacheKey)
	}
	return result.MinYear, result.MinMonth, result.MaxYear, result.MaxMonth, result.Found, nil
}

type CachedMonthNavResult struct { Year int; Month int; Found bool }

func (db *DB) GetPrevMonthWithMessages(ctx context.Context, groupName string, currentYear, currentMonth int) (prevYear, prevMonth int, found bool, err error) {
	_, span := otel.Tracer("nntp-web/internal/database").Start(ctx, "DB.GetPrevMonthWithMessages",
		trace.WithAttributes(
			attribute.String("group.name", groupName),
			attribute.Int("current.year", currentYear),
			attribute.Int("current.month", currentMonth),
		),
	)
	defer span.End()

	cacheKey := fmt.Sprintf("prevmonth:%s:%d-%02d", groupName, currentYear, currentMonth)
	if db.cache != nil {
		if cached, foundCache := db.cache.Get(ctx, cacheKey); foundCache {
			if result, ok := cached.(CachedMonthNavResult); ok {
				log.Printf("Cache hit for GetPrevMonthWithMessages: %s", cacheKey)
				span.SetAttributes(attribute.Bool("cache.hit", true)); return result.Year, result.Month, result.Found, nil
			}
		}
	}
	log.Printf("Cache miss for GetPrevMonthWithMessages: %s, querying DB", cacheKey)
	span.SetAttributes(attribute.Bool("cache.hit", false))

	var groupID uint16
	groupQuery := "SELECT id FROM `groups` WHERE name = ?"
	err = db.sqlDB.QueryRowContext(ctx, groupQuery, groupName).Scan(&groupID)
	if err != nil {
		if err == sql.ErrNoRows { err = fmt.Errorf("group '%s' not found: %w", groupName, err)
		} else { err = fmt.Errorf("failed to query group ID for '%s': %w", groupName, err) }
		span.RecordError(err); return 0, 0, false, err
	}
	span.SetAttributes(attribute.Int("group.id", int(groupID)))

	targetDate := time.Date(currentYear, time.Month(currentMonth), 1, 0, 0, 0, 0, time.UTC)
	query := `SELECT YEAR(received), MONTH(received) FROM articles WHERE group_id = ? AND received < ? ORDER BY received DESC LIMIT 1`
	var pYear, pMonth int
	err = db.sqlDB.QueryRowContext(ctx, query, groupID, targetDate).Scan(&pYear, &pMonth)
	if err != nil {
		if err == sql.ErrNoRows {
			if db.cache != nil { db.cache.Set(ctx, cacheKey, CachedMonthNavResult{Found: false}, db.cacheTTL) }
			span.SetAttributes(attribute.Bool("found", false)); return 0, 0, false, nil
		}
		err = fmt.Errorf("failed to find previous month for group %d: %w", groupID, err)
		span.RecordError(err); return 0, 0, false, err
	}
	if db.cache != nil { db.cache.Set(ctx, cacheKey, CachedMonthNavResult{Year: pYear, Month: pMonth, Found: true}, db.cacheTTL) }
	span.SetAttributes(attribute.Bool("found", true), attribute.Int("prev.year", pYear), attribute.Int("prev.month", pMonth))
	return pYear, pMonth, true, nil
}

func (db *DB) GetNextMonthWithMessages(ctx context.Context, groupName string, currentYear, currentMonth int) (nextYear, nextMonth int, found bool, err error) {
	_, span := otel.Tracer("nntp-web/internal/database").Start(ctx, "DB.GetNextMonthWithMessages",
		trace.WithAttributes(
			attribute.String("group.name", groupName),
			attribute.Int("current.year", currentYear),
			attribute.Int("current.month", currentMonth),
		),
	)
	defer span.End()

	cacheKey := fmt.Sprintf("nextmonth:%s:%d-%02d", groupName, currentYear, currentMonth)
	if db.cache != nil {
		if cached, foundCache := db.cache.Get(ctx, cacheKey); foundCache {
			if result, ok := cached.(CachedMonthNavResult); ok {
				log.Printf("Cache hit for GetNextMonthWithMessages: %s", cacheKey)
				span.SetAttributes(attribute.Bool("cache.hit", true)); return result.Year, result.Month, result.Found, nil
			}
		}
	}
	log.Printf("Cache miss for GetNextMonthWithMessages: %s, querying DB", cacheKey)
	span.SetAttributes(attribute.Bool("cache.hit", false))

	var groupID uint16
	groupQuery := "SELECT id FROM `groups` WHERE name = ?"
	err = db.sqlDB.QueryRowContext(ctx, groupQuery, groupName).Scan(&groupID)
	if err != nil {
		if err == sql.ErrNoRows { err = fmt.Errorf("group '%s' not found: %w", groupName, err)
		} else { err = fmt.Errorf("failed to query group ID for '%s': %w", groupName, err) }
		span.RecordError(err); return 0, 0, false, err
	}
	span.SetAttributes(attribute.Int("group.id", int(groupID)))

	targetDate := time.Date(currentYear, time.Month(currentMonth), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	query := `SELECT YEAR(received), MONTH(received) FROM articles WHERE group_id = ? AND received >= ? ORDER BY received ASC LIMIT 1`
	var nYear, nMonth int
	err = db.sqlDB.QueryRowContext(ctx, query, groupID, targetDate).Scan(&nYear, &nMonth)
	if err != nil {
		if err == sql.ErrNoRows {
			if db.cache != nil { db.cache.Set(ctx, cacheKey, CachedMonthNavResult{Found: false}, db.cacheTTL) }
			span.SetAttributes(attribute.Bool("found", false)); return 0, 0, false, nil
		}
		err = fmt.Errorf("failed to find next month for group %d: %w", groupID, err)
		span.RecordError(err); return 0, 0, false, err
	}
	if db.cache != nil { db.cache.Set(ctx, cacheKey, CachedMonthNavResult{Year: nYear, Month: nMonth, Found: true}, db.cacheTTL) }
	span.SetAttributes(attribute.Bool("found", true), attribute.Int("next.year", nYear), attribute.Int("next.month", nMonth))
	return nYear, nMonth, true, nil
}

type CachedLatestMonthResult struct { Year  int; Month int; Found bool }

func (db *DB) GetLatestMessageMonthForGroup(ctx context.Context, groupName string) (year, month int, found bool) {
	_, span := otel.Tracer("nntp-web/internal/database").Start(ctx, "DB.GetLatestMessageMonthForGroup",
		trace.WithAttributes(attribute.String("group.name", groupName)),
	)
	defer span.End()

	cacheKey := fmt.Sprintf("latestmonth:%s", groupName)
	if db.cache != nil {
		if cached, foundCache := db.cache.Get(ctx, cacheKey); foundCache {
			if result, ok := cached.(CachedLatestMonthResult); ok {
				log.Printf("Cache hit for GetLatestMessageMonthForGroup: %s", cacheKey)
				span.SetAttributes(attribute.Bool("cache.hit", true)); return result.Year, result.Month, result.Found
			}
		}
	}
	log.Printf("Cache miss for GetLatestMessageMonthForGroup: %s, querying DB", cacheKey)
	span.SetAttributes(attribute.Bool("cache.hit", false))

	var groupID uint16
	groupQuery := "SELECT id FROM `groups` WHERE name = ?"
	dbErr := db.sqlDB.QueryRowContext(ctx, groupQuery, groupName).Scan(&groupID)
	if dbErr != nil {
		if dbErr == sql.ErrNoRows {
			log.Printf("Group '%s' not found in GetLatestMessageMonthForGroup.", groupName)
			if db.cache != nil { db.cache.Set(ctx, cacheKey, CachedLatestMonthResult{Found: false}, db.cacheTTL) }
		} else {
			log.Printf("Error querying group ID for '%s' in GetLatestMessageMonthForGroup: %v", groupName, dbErr)
			span.RecordError(dbErr)
		}
		return 0, 0, false
	}
	span.SetAttributes(attribute.Int("group.id", int(groupID)))

	query := `SELECT YEAR(received), MONTH(received) FROM articles WHERE group_id = ? ORDER BY received DESC LIMIT 1`
	var y, m int
	dbErr = db.sqlDB.QueryRowContext(ctx, query, groupID).Scan(&y, &m)
	if dbErr != nil {
		if dbErr == sql.ErrNoRows {
			log.Printf("No messages found for group '%s' (ID: %d) in GetLatestMessageMonthForGroup.", groupName, groupID)
			if db.cache != nil { db.cache.Set(ctx, cacheKey, CachedLatestMonthResult{Found: false}, db.cacheTTL) }
		} else {
			log.Printf("Error querying latest month for group '%s' (ID: %d): %v", groupName, groupID, dbErr)
			span.RecordError(dbErr)
		}
		return 0, 0, false
	}

	if db.cache != nil {
		db.cache.Set(ctx, cacheKey, CachedLatestMonthResult{Year: y, Month: m, Found: true}, db.cacheTTL)
		log.Printf("Cached result for GetLatestMessageMonthForGroup: %s (Year: %d, Month: %d)", cacheKey, y, m)
	}
	span.SetAttributes(attribute.Bool("found", true), attribute.Int("latest.year", y), attribute.Int("latest.month", m))
	return y, m, true
}

func (db *DB) GetArticleByDetails(ctx context.Context, groupName string, year int, month int, articleNum uint32) (*models.Article, error) {
	_, span := otel.Tracer("nntp-web/internal/database").Start(ctx, "DB.GetArticleByDetails",
		trace.WithAttributes(
			attribute.String("group.name", groupName),
			attribute.Int("year", year),
			attribute.Int("month", month),
			attribute.Int("article.num", int(articleNum)),
		),
	)
	defer span.End()

	cacheKey := fmt.Sprintf("article:%s:%d-%02d:%d", groupName, year, month, articleNum)
	if db.cache != nil {
		if cached, found := db.cache.Get(ctx, cacheKey); found {
			if article, ok := cached.(*models.Article); ok {
				log.Printf("Cache hit for GetArticleByDetails: %s", cacheKey)
				span.SetAttributes(attribute.Bool("cache.hit", true)); return article, nil
			}
		}
	}
	log.Printf("Cache miss for GetArticleByDetails: %s, querying DB", cacheKey)
	span.SetAttributes(attribute.Bool("cache.hit", false))

	var groupID uint16
	groupQuery := "SELECT id FROM `groups` WHERE name = ?"
	err := db.sqlDB.QueryRowContext(ctx, groupQuery, groupName).Scan(&groupID)
	if err != nil {
		if err == sql.ErrNoRows { err = fmt.Errorf("group '%s' not found for article lookup: %w", groupName, err)
		} else { err = fmt.Errorf("failed to query group ID for '%s': %w", groupName, err) }
		span.RecordError(err); return nil, err
	}
	span.SetAttributes(attribute.Int("group.id", int(groupID)))

	articleQuery := `
		SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
		       received, thread_id, parent, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND id = ? AND YEAR(received) = ? AND MONTH(received) = ?
		LIMIT 1`
	var article models.Article
	article.GroupName = groupName
	var rawMsgID string
	err = db.sqlDB.QueryRowContext(ctx, articleQuery, groupID, articleNum, year, month).Scan(
		&article.GroupID, &article.ArticleNum, &rawMsgID, &article.Subject,
		&article.From, &article.Date, &article.Received, &article.ThreadID,
		&article.ParentNum, &article.RawReferences, &article.Lines, &article.Bytes,
	)
	if err == nil {
		article.MessageID = strings.Trim(rawMsgID, "<>")
		article.References = models.ParseReferencesString(article.RawReferences)
	} else if err == sql.ErrNoRows {
		span.AddEvent("Strict article query failed (no rows), trying relaxed query.")
		relaxedArticleQuery := `
			SELECT group_id, id, h_messageid, h_subject, h_from, h_date,
					received, thread_id, parent, h_references, h_lines, h_bytes
			FROM articles WHERE group_id = ? AND id = ? LIMIT 1`
		var relaxedRawMsgID string
		errRelaxed := db.sqlDB.QueryRowContext(ctx, relaxedArticleQuery, groupID, articleNum).Scan(
			&article.GroupID, &article.ArticleNum, &relaxedRawMsgID, &article.Subject,
			&article.From, &article.Date, &article.Received, &article.ThreadID,
			&article.ParentNum, &article.RawReferences, &article.Lines, &article.Bytes,
		)
		if errRelaxed == nil {
			article.MessageID = strings.Trim(relaxedRawMsgID, "<>")
			article.References = models.ParseReferencesString(article.RawReferences)
			if db.cache != nil {
				db.cache.Set(ctx, cacheKey, &article, db.cacheTTL)
				log.Printf("Cached result (after relaxed query) for GetArticleByDetails: %s", cacheKey)
			}
			span.SetAttributes(attribute.Bool("found_relaxed", true)); return &article, nil
		}
		err = fmt.Errorf("article num %d in group '%s' (year %d, month %d) not found (strict and relaxed): %w", articleNum, groupName, year, month, sql.ErrNoRows)
		span.RecordError(err); return nil, err
	} else {
		err = fmt.Errorf("failed to query article num %d in group '%s': %w", articleNum, groupName, err)
		span.RecordError(err); return nil, err
	}

	if db.cache != nil {
		db.cache.Set(ctx, cacheKey, &article, db.cacheTTL)
		log.Printf("Cached result for GetArticleByDetails: %s", cacheKey)
	}
	return &article, nil
}

func (db *DB) GetArticleByMessageID(ctx context.Context, messageIDVal string) (*models.Article, error) {
	_, span := otel.Tracer("nntp-web/internal/database").Start(ctx, "DB.GetArticleByMessageID",
		trace.WithAttributes(attribute.String("message.id", messageIDVal)),
	)
	defer span.End()

	cacheKey := fmt.Sprintf("article:msgid:%s", messageIDVal)
	if db.cache != nil {
		if cached, found := db.cache.Get(ctx, cacheKey); found {
			if article, ok := cached.(*models.Article); ok {
				log.Printf("Cache hit for GetArticleByMessageID: %s", cacheKey)
				span.SetAttributes(attribute.Bool("cache.hit", true)); return article, nil
			}
		}
	}
	log.Printf("Cache miss for GetArticleByMessageID: %s, querying DB", cacheKey)
	span.SetAttributes(attribute.Bool("cache.hit", false))

	articleQuery := `
		SELECT a.group_id, a.id, a.h_messageid, a.h_subject, a.h_from, a.h_date,
		       a.received, a.thread_id, a.parent, a.h_references, a.h_lines, a.h_bytes,
		       g.name as group_name
		FROM articles a JOIN ` + "`groups`" + ` g ON a.group_id = g.id
		WHERE a.h_messageid = ? LIMIT 1`
	var article models.Article
	var rawMsgID string
	err := db.sqlDB.QueryRowContext(ctx, articleQuery, messageIDVal).Scan(
		&article.GroupID, &article.ArticleNum, &rawMsgID, &article.Subject,
		&article.From, &article.Date, &article.Received, &article.ThreadID,
		&article.ParentNum, &article.RawReferences, &article.Lines, &article.Bytes,
		&article.GroupName,
	)
	if err != nil {
		if err == sql.ErrNoRows { err = fmt.Errorf("article with Message-ID '%s' not found: %w", messageIDVal, err)
		} else { err = fmt.Errorf("failed to query article by Message-ID '%s': %w", messageIDVal, err)}
		span.RecordError(err); return nil, err
	}
	article.MessageID = strings.Trim(rawMsgID, "<>")
	article.References = models.ParseReferencesString(article.RawReferences)
	if db.cache != nil {
		db.cache.Set(ctx, cacheKey, &article, db.cacheTTL)
		log.Printf("Cached result for GetArticleByMessageID: %s", cacheKey)
	}
	return &article, nil
}

func (db *DB) GetThreadMessages(ctx context.Context, threadID uint32, groupID uint16) ([]models.Article, error) {
	_, span := otel.Tracer("nntp-web/internal/database").Start(ctx, "DB.GetThreadMessages",
		trace.WithAttributes(
			attribute.Int("thread.id", int(threadID)),
			attribute.Int("group.id", int(groupID)),
		),
	)
	defer span.End()

	cacheKey := fmt.Sprintf("threadmsgs:full:%d:%d", threadID, groupID)
	if db.cache != nil {
		if cached, found := db.cache.Get(ctx, cacheKey); found {
			if articles, ok := cached.([]models.Article); ok {
				log.Printf("Cache hit for GetThreadMessages (full thread, group %d): %s", groupID, cacheKey)
				span.SetAttributes(attribute.Bool("cache.hit", true)); return articles, nil
			}
		}
	}
	log.Printf("Cache miss for GetThreadMessages (full thread, group %d): %s, querying DB", groupID, cacheKey)
	span.SetAttributes(attribute.Bool("cache.hit", false))

	query := `
		SELECT a.group_id, a.id, a.h_messageid, a.h_subject, a.h_from, a.h_date,
		       a.received, a.thread_id, a.parent, a.h_references, a.h_lines, a.h_bytes,
		       g.name as group_name
		FROM articles a JOIN ` + "`groups` g ON a.group_id = g.id" + `
		WHERE a.thread_id = ? AND a.group_id = ? ORDER BY a.received ASC, a.id ASC`
	if db.cfg != nil && db.cfg.LogSQLQueries {
		log.Printf("DB_QUERY: GetThreadMessages - SQL: %s - Args: [%d, %d]", strings.ReplaceAll(strings.TrimSpace(query), "\n", " "), threadID, groupID)
	}

	rows, err := db.sqlDB.QueryContext(ctx, query, threadID, groupID)
	if err != nil {
		err = fmt.Errorf("failed to query thread messages for thread_id %d: %w", threadID, err)
		span.RecordError(err); return nil, err
	}
	defer rows.Close()

	var articles []models.Article
	for rows.Next() {
		var article models.Article
		var rawMsgID string
		if errScan := rows.Scan(
			&article.GroupID, &article.ArticleNum, &rawMsgID, &article.Subject,
			&article.From, &article.Date, &article.Received, &article.ThreadID,
			&article.ParentNum, &article.RawReferences, &article.Lines, &article.Bytes,
			&article.GroupName,
		); errScan != nil {
			log.Printf("Error scanning article row for thread_id %d: %v", threadID, errScan)
			span.AddEvent("Row scan error", trace.WithAttributes(attribute.String("error.message", errScan.Error())))
			continue
		}
		article.MessageID = strings.Trim(rawMsgID, "<>")
		article.References = models.ParseReferencesString(article.RawReferences)
		articles = append(articles, article)
	}

	if iterErr := rows.Err(); iterErr != nil {
		err = fmt.Errorf("iteration error while fetching thread messages for thread_id %d: %w", threadID, iterErr)
		span.RecordError(err); return nil, err
	}

	if db.cache != nil && len(articles) > 0 {
		db.cache.Set(ctx, cacheKey, articles, db.cacheTTL)
		log.Printf("Cached result for GetThreadMessages: %s", cacheKey)
	}
	return articles, nil
}

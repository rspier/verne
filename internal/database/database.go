package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"nntp-web/internal/config"
	"nntp-web/internal/models"

	_ "github.com/go-sql-driver/mysql" // MySQL driver
)

// DB wraps the sql.DB connection pool.
type DB struct {
	sqlDB *sql.DB
}

// New creates a new DB instance and connects to the database.
func New(cfg *config.Config) (*DB, error) {
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

	return &DB{sqlDB: sqlDB}, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	if db.sqlDB != nil {
		return db.sqlDB.Close()
	}
	return nil
}

// NewWithSQLDB is a constructor for testing purposes, allowing injection of a custom *sql.DB.
// This should only be used in tests.
func NewWithSQLDB(sqlDb *sql.DB) *DB {
	return &DB{sqlDB: sqlDb}
}

// GetAllNewsgroups retrieves all newsgroups from the database, ordered by name.
func (db *DB) GetAllNewsgroups() ([]models.Newsgroup, error) {
	query := "SELECT id, name, description FROM `groups` ORDER BY name" // Backticks for table name
	rows, err := db.sqlDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var groups []models.Newsgroup
	for rows.Next() {
		var group models.Newsgroup
		if err := rows.Scan(&group.ID, &group.Name, &group.Description); err != nil {
			return nil, fmt.Errorf("failed to scan group row: %w", err) // Return error immediately
		}
		groups = append(groups, group)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iteration error: %w", err)
	}

	return groups, nil
}

// GetMessagesForGroupMonth retrieves articles for a specific group and month/year.
// Articles are ordered by received date, descending.
func (db *DB) GetMessagesForGroupMonth(groupName string, year int, month int) ([]models.Article, error) {
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
			&article.ParentNum, &article.References, &article.Lines, &article.Bytes,
		); err != nil {
			log.Printf("Error scanning article row for group '%s': %v", groupName, err)
			// Decide on error handling: skip row or return error
			// return nil, fmt.Errorf("failed to scan article row: %w", err)
			continue // Skip problematic row
		}
		articles = append(articles, article)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iteration error while fetching articles for group '%s': %w", groupName, err)
	}

	return articles, nil
}

// GetArticleByDetails retrieves a specific article by its group name, year, month, and article number (articles.id).
// It also populates the Article.GroupName field.
func (db *DB) GetArticleByDetails(groupName string, year int, month int, articleNum uint32) (*models.Article, error) {
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
		&article.ParentNum, &article.References, &article.Lines, &article.Bytes,
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
				&relaxedArticle.ParentNum, &relaxedArticle.References, &relaxedArticle.Lines, &relaxedArticle.Bytes,
			)
			if errRelaxed == nil {
				// Article found, but with different date. Return this one for the handler to decide on redirect.
				return &relaxedArticle, nil
			}
			// If still not found, then it's a genuine ErrNoRows for this group/articleNum combination.
			return nil, fmt.Errorf("article num %d in group '%s' (year %d, month %d) not found: %w", articleNum, groupName, year, month, sql.ErrNoRows)
		}
		return nil, fmt.Errorf("failed to query article num %d in group '%s': %w", articleNum, groupName, err)
	}
	return &article, nil
}


// GetArticleByMessageID retrieves a specific article by its h_messageid (full Message-ID).
// It also populates the Article.GroupName field.
func (db *DB) GetArticleByMessageID(messageIDVal string) (*models.Article, error) {
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
		&article.ParentNum, &article.References, &article.Lines, &article.Bytes,
		&article.GroupName,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("article with Message-ID '%s' not found: %w", messageIDVal, err)
		}
		return nil, fmt.Errorf("failed to query article by Message-ID '%s': %w", messageIDVal, err)
	}
	return &article, nil
}

// GetThreadMessages retrieves all messages in the same thread as a given article,
// excluding the article itself. Articles are ordered by their received date.
// It also populates the Article.GroupName field.
func (db *DB) GetThreadMessages(threadID uint32, currentArticleGroupID uint16, currentArticleNum uint32) ([]models.Article, error) {
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
			&article.ParentNum, &article.References, &article.Lines, &article.Bytes,
			&article.GroupName,
		); err != nil {
			log.Printf("Error scanning article row for thread_id %d: %v", threadID, err)
			continue
		}
		articles = append(articles, article)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iteration error while fetching thread messages for thread_id %d: %w", threadID, err)
	}

	return articles, nil
}

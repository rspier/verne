package database

import (
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/user/colobus/internal/config" // Import config for GroupConfig type

	_ "github.com/go-sql-driver/mysql" // MySQL driver
	// _ "github.com/mattn/go-sqlite3" // Example for SQLite, if we support it
)

// DB wraps the sql.DB connection pool.
type DB struct {
	*sql.DB
}

// ArticleHeader represents the essential headers for an article from the DB.
type ArticleHeader struct {
	ID           int64          // Article number within its group
	HashedMsgID  string         // This is the md5_hex(actual_message_id) in the 'articles' table's 'msgid' column
	Subject      sql.NullString `db:"h_subject"`
	From         sql.NullString `db:"h_from"`
	Date         sql.NullString `db:"h_date"`       // Original date string from header
	MessageID    sql.NullString `db:"h_messageid"`  // Actual <message-id> header content
	References   sql.NullString `db:"h_references"` // References header content
	Lines        sql.NullInt64  `db:"h_lines"`
	Bytes        sql.NullInt64  `db:"h_bytes"`
	GroupID      int            // The numeric ID of the group this article belongs to (set during query/retrieval)
	Xref         string         // To be constructed: servername group1:artid1 group2:artid2 ...
	Newsgroups   string         // To be constructed: group1,group2,...
	RawMessageID string         // The actual Message-ID string, like <foo@bar.com> (derived from MessageID.String)
}

// Group holds information about a newsgroup, primarily from the database.
type Group struct {
	ID   int
	Name string
	// MinArticleID, MaxArticleID, Postings can be queried separately or joined
}

var (
	// Updated regex to be less greedy and handle missing parts better
	perlMySQLExtractPattern  = regexp.MustCompile(`(?:database|dbname)=([^;]+)(?:;host=([^;]+))?(?:;port=([^;]+))?`)
	sqliteFilePattern        = regexp.MustCompile(`(?i)\.db$`)
	sqliteDSNPrefixPattern   = regexp.MustCompile(`(?i)^sqlite3?:`)
)


// Connect establishes a connection to the database.
// It attempts to parse Perl DBI-style DSNs for MySQL or recognize SQLite file paths.
// Otherwise, it assumes a Go-compatible DSN.
func Connect(dsn, user, pass string) (*DB, error) {
	var driverName, finalDSN string

	normalizedDSN := strings.ToLower(dsn)

	if strings.HasPrefix(normalizedDSN, "dbi:mysql:") {
		driverName = "mysql"
		paramsPart := strings.TrimPrefix(dsn, "DBI:mysql:") // Use original case for param part
		paramsPart = strings.TrimPrefix(paramsPart, "dbi:mysql:")


		matches := perlMySQLExtractPattern.FindStringSubmatch(paramsPart)
		dbName := "colobus" // Default
		host := "localhost"
		port := "3306"

		// matches[0] is the full string, [1] is dbname, [2] is host, [3] is port
		if len(matches) > 1 && matches[1] != "" {
			dbName = matches[1]
		}
		if len(matches) > 2 && matches[2] != "" {
			host = matches[2]
		}
		if len(matches) > 3 && matches[3] != "" {
			port = matches[3]
		}

		finalDSN = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&multiStatements=true", user, pass, host, port, dbName)
		log.Printf("Interpreted Perl DSN. User: '%s', Host: '%s', Port: '%s', DBName: '%s'", user, host, port, dbName)

	} else if sqliteFilePattern.MatchString(dsn) || sqliteDSNPrefixPattern.MatchString(normalizedDSN) {
		driverName = "sqlite3"
		finalDSN = strings.TrimPrefix(strings.TrimPrefix(dsn, "sqlite:"), "sqlite3:")
		log.Printf("Interpreted DSN as SQLite. Path: %s", finalDSN)
	} else {
		// Assume it's a Go-compatible DSN;
		finalDSN = dsn // Use the DSN as is
		// Try to guess driver if not obvious, default to mysql
		if strings.Contains(normalizedDSN, "sqlite") || strings.HasSuffix(normalizedDSN, ".db"){
			driverName = "sqlite3"
		} else {
			driverName = "mysql" // Default guess
		}
		log.Printf("Using provided DSN '%s' with assumed driver '%s'. User/Pass from config: '%s'", dsn, driverName, user)
		// Note: If user/pass are part of `finalDSN` already, they take precedence.
		// If not, the driver must support user/pass fields or they must be in the DSN string.
		// For MySQL, `go-sql-driver` allows user:pass@tcp(...)/dbname
		// If DSN is just `tcp(host)/dbname` and user/pass are provided, we might need to prepend them.
		// However, the current MySQL driver format is `user:password@protocol(address)/dbname?param=value`
		// So, if user/pass are separate, they should be prepended if not in DSN.
		if user != "" && !strings.Contains(finalDSN, "@") && driverName == "mysql" {
			// This is a basic attempt to prepend; might need more robust DSN parsing/construction
			// if DSN is just "tcp(localhost)/dbname" for example.
			// A common pattern is "username:password@tcp(host)/dbname"
			// If finalDSN is "tcp(host)/dbname", then user:pass@finalDSN
			if strings.HasPrefix(finalDSN, "tcp(") || strings.HasPrefix(finalDSN, "unix(") {
				finalDSN = fmt.Sprintf("%s:%s@%s", user, pass, finalDSN)
				log.Printf("Prepended user/pass to DSN, new DSN: %s/******@%s", user, strings.SplitN(finalDSN, "@", 2)[1])
			}
		}

	}


	db, err := sql.Open(driverName, finalDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection (driver: %s): %w", driverName, err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database (driver: %s): %w", driverName, err)
	}

	log.Printf("Successfully connected to database using driver %s", driverName)
	return &DB{db}, nil
}


// Close closes the database connection.
func (db *DB) Close() error {
	if db.DB != nil {
		log.Println("Closing database connection.")
		return db.DB.Close()
	}
	return nil
}

func (db *DB) GetGroupByName(name string) (*Group, error) {
	query := "SELECT id, name FROM groups WHERE name = ?"
	g := &Group{}
	err := db.QueryRow(query, name).Scan(&g.ID, &g.Name)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Not found
		}
		return nil, fmt.Errorf("querying group by name '%s': %w", name, err)
	}
	return g, nil
}

func (db *DB) GetGroupByID(id int) (*Group, error) {
	query := "SELECT id, name FROM groups WHERE id = ?"
	g := &Group{}
	err := db.QueryRow(query, id).Scan(&g.ID, &g.Name)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Not found
		}
		return nil, fmt.Errorf("querying group by id %d: %w", id, err)
	}
	return g, nil
}


// GetGroupMinMaxArticleIDs retrieves the min and max article IDs for a given group ID.
func (db *DB) GetGroupMinMaxArticleIDs(groupID int) (minArticleID, maxArticleID int64, count int64, err error) {
	query := "SELECT MIN(id), MAX(id) FROM articles WHERE group_id = ?"
	var sqlMin, sqlMax sql.NullInt64

	err = db.QueryRow(query, groupID).Scan(&sqlMin, &sqlMax)
	if err != nil {
		// ErrNoRows should ideally not happen for MIN/MAX aggregates unless the table is empty
		// or group_id doesn't exist, in which case NULLs are returned.
		// So, a scan error here is more likely a real DB error.
		return 0, 0, 0, fmt.Errorf("querying min/max articles for group_id %d: %w", groupID, err)
	}

	if sqlMin.Valid {
		minArticleID = sqlMin.Int64
	} else { // No articles in group or group_id does not match any articles
		minArticleID = 0
	}
	if sqlMax.Valid {
		maxArticleID = sqlMax.Int64
	} else { // No articles in group
		maxArticleID = 0
	}

	if maxArticleID == 0 && minArticleID == 0 { // Covers case where group has no articles
	    count = 0
	} else if maxArticleID >= minArticleID {
		count = maxArticleID - minArticleID + 1
	} else { // Should not happen if min/max are valid and min <= max
		count = 0
	}
	return minArticleID, maxArticleID, count, nil
}


// GetArticleHeaders retrieves overview information for a range of articles in a group.
func (db *DB) GetArticleHeaders(groupID int, startArticleNum, endArticleNum int64) ([]ArticleHeader, error) {
	query := `
		SELECT id, msgid, h_subject, h_from, h_date, h_messageid, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND id >= ? AND id <= ?
		ORDER BY id ASC`

	rows, err := db.Query(query, groupID, startArticleNum, endArticleNum)
	if err != nil {
		return nil, fmt.Errorf("querying article headers for group %d (articles %d-%d): %w", groupID, startArticleNum, endArticleNum, err)
	}
	defer rows.Close()

	var headers []ArticleHeader
	for rows.Next() {
		var h ArticleHeader
		h.GroupID = groupID
		err := rows.Scan(
			&h.ID, &h.HashedMsgID,
			&h.Subject, &h.From, &h.Date, &h.MessageID, &h.References,
			&h.Lines, &h.Bytes,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning article header row: %w", err)
		}
		if h.MessageID.Valid {
			h.RawMessageID = h.MessageID.String
		}
		headers = append(headers, h)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating article header rows: %w", err)
	}
	return headers, nil
}

// GetArticleHeaderByNum retrieves a single article's overview information by its number in a group.
func (db *DB) GetArticleHeaderByNum(groupID int, articleNum int64) (*ArticleHeader, error) {
	query := `
		SELECT id, msgid, h_subject, h_from, h_date, h_messageid, h_references, h_lines, h_bytes
		FROM articles
		WHERE group_id = ? AND id = ?`

	var h ArticleHeader
	h.GroupID = groupID
	err := db.QueryRow(query, groupID, articleNum).Scan(
		&h.ID, &h.HashedMsgID,
		&h.Subject, &h.From, &h.Date, &h.MessageID, &h.References,
		&h.Lines, &h.Bytes,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Not found
		}
		return nil, fmt.Errorf("querying article header for group %d article %d: %w", groupID, articleNum, err)
	}
	if h.MessageID.Valid {
		h.RawMessageID = h.MessageID.String
	}
	return &h, nil
}


// GetArticleXrefsByHashedMsgID retrieves all (group_id, article_id) pairs for a given hashed message ID.
func (db *DB) GetArticleXrefsByHashedMsgID(hashedMsgID string) (map[int]int64, error) {
	query := `SELECT group_id, id FROM articles WHERE msgid = ?`
	rows, err := db.Query(query, hashedMsgID)
	if err != nil {
		return nil, fmt.Errorf("querying xrefs for msgid %s: %w", hashedMsgID, err)
	}
	defer rows.Close()

	xrefs := make(map[int]int64)
	for rows.Next() {
		var groupID int
		var articleID int64
		if err := rows.Scan(&groupID, &articleID); err != nil {
			return nil, fmt.Errorf("scanning xref row for msgid %s: %w", hashedMsgID, err)
		}
		xrefs[groupID] = articleID
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating xref rows for msgid %s: %w", hashedMsgID, err)
	}
	return xrefs, nil
}

// GetArticleGroupAndNumByHashedMsgID retrieves the group_id and article number for a given hashed message ID.
// It limits to 1, assuming msgid should be unique enough or we only care about one instance.
func (db *DB) GetArticleGroupAndNumByHashedMsgID(hashedMsgID string) (groupID int, articleNum int64, found bool, err error) {
	query := `SELECT group_id, id FROM articles WHERE msgid = ? LIMIT 1`
	err = db.QueryRow(query, hashedMsgID).Scan(&groupID, &articleNum)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, 0, false, nil
		}
		return 0, 0, false, fmt.Errorf("querying group/id by msgid %s: %w", hashedMsgID, err)
	}
	return groupID, articleNum, true, nil
}

// CheckHashedMessageIDExists checks if a hashed message ID exists in the database.
func (db *DB) CheckHashedMessageIDExists(hashedMsgID string) (bool, error) {
	var tempID int64 // Changed from int to int64 to match typical PK types
	query := "SELECT id FROM articles WHERE msgid = ? LIMIT 1" // Assuming 'id' here is the article's own primary key in 'articles' table
	err := db.QueryRow(query, hashedMsgID).Scan(&tempID)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("checking message ID existence for hashed msgid %s: %w", hashedMsgID, err)
	}
	return true, nil
}

// EnsureGroupsExistInDB syncs groups from the config file to the database 'groups' table.
// Returns two maps: one from group name to its effective numeric ID, and one from ID to name.
func (db *DB) EnsureGroupsExistInDB(configGroups map[string]*config.GroupConfig) (
	effectiveNameToNum map[string]int,
	effectiveNumToName map[int]string,
	err error,
) {
	tx, err := db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("starting transaction for group sync: %w", err)
	}
	defer tx.Rollback()

	// Load existing groups from DB
	dbGroupsByName := make(map[string]int)    // name -> id
	dbGroupsByID := make(map[int]string) // id -> name
	rows, err := tx.Query("SELECT id, name FROM groups")
	if err != nil {
		return nil, nil, fmt.Errorf("querying existing groups from DB: %w", err)
	}
	defer rows.Close()

	maxDBID := 0
	for rows.Next() {
		var id int
		var name string
		if scanErr := rows.Scan(&id, &name); scanErr != nil {
			return nil, nil, fmt.Errorf("scanning group row from DB: %w", scanErr)
		}
		dbGroupsByName[name] = id
		dbGroupsByID[id] = name
		if id > maxDBID {
			maxDBID = id
		}
	}
	if err = rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating groups from DB: %w", err)
	}

	insertStmt, err := tx.Prepare("INSERT INTO groups (id, name) VALUES (?, ?)")
	if err != nil {
		return nil, nil, fmt.Errorf("preparing insert statement for groups: %w", err)
	}
	defer insertStmt.Close()

	effectiveNameToNum = make(map[string]int)
	effectiveNumToName = make(map[int]string)

	// Iterate through config groups to determine their effective/final numeric ID
	for cfgGroupName, cfgGroup := range configGroups {
		dbIDForCfgGroup, inDBByName := dbGroupsByName[cfgGroupName]

		finalGroupNum := 0

		if cfgGroup.Num != 0 { // Num is specified in config
			finalGroupNum = cfgGroup.Num
			if nameInDBByID, idTaken := dbGroupsByID[finalGroupNum]; idTaken && nameInDBByID != cfgGroupName {
				// Configured num is taken by a DIFFERENT group in DB
				return nil, nil, fmt.Errorf("group '%s' config num %d conflicts with DB group '%s' (same num)", cfgGroupName, finalGroupNum, nameInDBByID)
			}
			if inDBByName && dbIDForCfgGroup != finalGroupNum {
				// Group name exists in DB but with a different ID than specified in config. This is a conflict.
				// Perl script just warns. For safety, we might want to be stricter or have a clear precedence rule.
				// Here, we'll log a warning and prioritize the config's number if it doesn't clash with another group's ID.
				log.Printf("Warning: Group '%s' has num %d in config but num %d in DB. Prioritizing config num.", cfgGroupName, finalGroupNum, dbIDForCfgGroup)
			}
		} else { // Num is NOT specified in config (cfgGroup.Num == 0)
			if inDBByName {
				finalGroupNum = dbIDForCfgGroup // Use existing DB ID
			} else {
				maxDBID++ // Assign a new ID
				finalGroupNum = maxDBID
			}
		}

		// Update the config object with the determined number
		cfgGroup.Num = finalGroupNum
		effectiveNameToNum[cfgGroupName] = finalGroupNum
		effectiveNumToName[finalGroupNum] = cfgGroupName

		// If group (by name) is not in DB, or if it is but ID needs to be established (e.g. config num was 0 and it wasn't in DB)
		if !inDBByName {
			log.Printf("Group '%s' (ID %d) not in DB. Inserting.", cfgGroupName, finalGroupNum)
			if _, execErr := insertStmt.Exec(finalGroupNum, cfgGroupName); execErr != nil {
				return nil, nil, fmt.Errorf("inserting group '%s' (ID %d) into DB: %w", cfgGroupName, finalGroupNum, execErr)
			}
			// Add to our loaded DB maps for subsequent checks within this transaction
			dbGroupsByName[cfgGroupName] = finalGroupNum
			dbGroupsByID[finalGroupNum] = cfgGroupName
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("committing group sync transaction: %w", err)
	}

	log.Println("Group synchronization with database complete.")
	return effectiveNameToNum, effectiveNumToName, nil
}

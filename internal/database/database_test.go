package database

import (
	"fmt"
	"testing"
	// We will not use a live DB for these DSN parsing unit tests
	// _ "github.com/go-sql-driver/mysql"
	// _ "github.com/mattn/go-sqlite3"
)

func TestParsePerlDSN_MySQL(t *testing.T) {
	testCases := []struct {
		name           string
		dsn            string
		user           string
		pass           string
		expectedDriver string
		expectedDSN    string // Go-compatible DSN, user/pass part might be added by Connect
		expectError    bool
	}{
		{
			name:           "Full Perl DSN",
			dsn:            "DBI:mysql:database=colobus_db;host=db.example.com;port=3307",
			user:           "user1",
			pass:           "pass1",
			expectedDriver: "mysql",
			// Connect will prepend user:pass@ if not present
			expectedDSN: "user1:pass1@tcp(db.example.com:3307)/colobus_db?parseTime=true&multiStatements=true",
		},
		{
			name:           "Perl DSN no port",
			dsn:            "DBI:mysql:database=mydb;host=myhost",
			user:           "user2",
			pass:           "pass2",
			expectedDriver: "mysql",
			expectedDSN:    "user2:pass2@tcp(myhost:3306)/mydb?parseTime=true&multiStatements=true",
		},
		{
			name:           "Perl DSN no host (defaults to localhost)",
			dsn:            "DBI:mysql:database=onlydb",
			user:           "user3",
			pass:           "pass3",
			expectedDriver: "mysql",
			expectedDSN:    "user3:pass3@tcp(localhost:3306)/onlydb?parseTime=true&multiStatements=true",
		},
		{
			name:           "Perl DSN minimal (dbname only, implies localhost, default port)",
			dsn:            "dbi:mysql:dbname=minidb", // Lowercase dbi, dbname variation
			user:           "user4",
			pass:           "pass4",
			expectedDriver: "mysql",
			expectedDSN:    "user4:pass4@tcp(localhost:3306)/minidb?parseTime=true&multiStatements=true",
		},
		{
			name:           "Perl DSN with extra params (should be ignored by current parser)",
			dsn:            "DBI:mysql:database=colobus;host=localhost;mysql_ssl=1",
			user:           "user5",
			pass:           "pass5",
			expectedDriver: "mysql",
			expectedDSN:    "user5:pass5@tcp(localhost:3306)/colobus?parseTime=true&multiStatements=true",
		},
		{
			name:           "Perl DSN case variations for keys",
			dsn:            "DBI:mysql:DATABASE=CaseDB;HoSt=CaseHost",
			user:           "usercase",
			pass:           "passcase",
			expectedDriver: "mysql",
			expectedDSN:    "usercase:passcase@tcp(CaseHost:3306)/CaseDB?parseTime=true&multiStatements=true",
		},
	}

	// Mock sql.Open and Ping for these tests as we only test DSN parsing part of Connect
	// This is a bit of a hack; ideally, parseDSN would be a separate, exported function to test directly.
	// For now, we check the constructed DSN that would be passed to sql.Open.
	originalSQLOpen := sqlOpen
	originalSQLDriverRegistered := func(name string) bool { return true } // Assume driver is always registered
	defer func() { sqlOpen = originalSQLOpen }()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedDriver, capturedDSN string
			sqlOpen = func(driverName, dataSourceName string) (*DBConnection, error) {
				capturedDriver = driverName
				capturedDSN = dataSourceName
				// Return a dummy DBConnection that will succeed Ping
				return &DBConnection{
					pingFunc: func() error { return nil },
					closeFunc: func() error { return nil },
				}, nil
			}
			isDriverRegistered = originalSQLDriverRegistered


			// We are testing the DSN transformation within Connect, not a live connection.
			// So, the *DB result itself is not the primary focus, but the DSN passed to sql.Open.
			_, err := Connect(tc.dsn, tc.user, tc.pass)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected an error for DSN '%s', but got nil", tc.dsn)
				}
				return
			}
			if err != nil {
				t.Errorf("Connect failed for DSN '%s': %v", tc.dsn, err)
				return
			}

			if capturedDriver != tc.expectedDriver {
				t.Errorf("DSN '%s': Expected driver '%s', got '%s'", tc.dsn, tc.expectedDriver, capturedDriver)
			}
			if capturedDSN != tc.expectedDSN {
				t.Errorf("DSN '%s': Expected final DSN '%s', got '%s'", tc.dsn, tc.expectedDSN, capturedDSN)
			}
		})
	}
}

func TestParseSQLiteDSN(t *testing.T) {
	testCases := []struct {
		name           string
		dsn            string
		expectedDriver string
		expectedDSN    string // Go-compatible DSN
	}{
		{"Simple file path", "colobus.db", "sqlite3", "colobus.db"},
		{"Path with dir", "/path/to/colobus.db", "sqlite3", "/path/to/colobus.db"},
		{"sqlite: prefix", "sqlite:colobus.db", "sqlite3", "colobus.db"},
		{"sqlite3: prefix", "sqlite3:/another/path.db", "sqlite3", "/another/path.db"},
	}

	originalSQLOpen := sqlOpen
	originalSQLDriverRegistered := func(name string) bool { return true }
	defer func() { sqlOpen = originalSQLOpen }()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedDriver, capturedDSN string
			sqlOpen = func(driverName, dataSourceName string) (*DBConnection, error) {
				capturedDriver = driverName
				capturedDSN = dataSourceName
				return &DBConnection{pingFunc: func() error { return nil }, closeFunc: func() error { return nil }}, nil
			}
			isDriverRegistered = originalSQLDriverRegistered

			// User/pass are ignored for SQLite typically
			_, err := Connect(tc.dsn, "", "")
			if err != nil {
				t.Errorf("Connect failed for DSN '%s': %v", tc.dsn, err)
				return
			}

			if capturedDriver != tc.expectedDriver {
				t.Errorf("DSN '%s': Expected driver '%s', got '%s'", tc.dsn, tc.expectedDriver, capturedDriver)
			}
			if capturedDSN != tc.expectedDSN {
				t.Errorf("DSN '%s': Expected final DSN '%s', got '%s'", tc.dsn, tc.expectedDSN, capturedDSN)
			}
		})
	}
}

func TestParseGenericDSN_PassthroughOrPrepend(t *testing.T) {
	testCases := []struct {
		name           string
		dsn            string
		user           string
		pass           string
		expectedDriver string // Usually guessed as mysql if not obvious
		expectedDSN    string
	}{
		{
			name:           "Full Go DSN (MySQL)",
			dsn:            "myuser:mypass@tcp(myhost:3306)/mydb?charset=utf8",
			user:           "ignored_user", // Should be ignored as DSN has user/pass
			pass:           "ignored_pass",
			expectedDriver: "mysql",
			expectedDSN:    "myuser:mypass@tcp(myhost:3306)/mydb?charset=utf8",
		},
		{
			name:           "Go DSN no user/pass, provide separately (MySQL)",
			dsn:            "tcp(dbserver:1234)/dbname",
			user:           "appuser",
			pass:           "apppass",
			expectedDriver: "mysql",
			expectedDSN:    "appuser:apppass@tcp(dbserver:1234)/dbname",
		},
		{
			name:           "Postgres DSN (passthrough, driver guessed mysql but pg driver not imported)",
			dsn:            "postgres://user:pass@host:port/dbname?sslmode=disable",
			user:           "", // Part of DSN
			pass:           "",
			expectedDriver: "mysql", // Guessed, would fail if trying to actually connect without pg driver
			expectedDSN:    "postgres://user:pass@host:port/dbname?sslmode=disable",
		},
		{
            name: "DSN without protocol, with user/pass from args (MySQL)",
            dsn: "myhost/mydb",  // Assumes tcp protocol for mysql
            user: "u",
            pass: "p",
            expectedDriver: "mysql",
            expectedDSN: "u:p@tcp(myhost:3306)/mydb?parseTime=true&multiStatements=true", // Connect adds default port and params for mysql
        },
	}

	originalSQLOpen := sqlOpen
	originalSQLDriverRegistered := func(name string) bool { return true }
	defer func() { sqlOpen = originalSQLOpen }()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedDriver, capturedDSN string
			sqlOpen = func(driverName, dataSourceName string) (*DBConnection, error) {
				capturedDriver = driverName
				capturedDSN = dataSourceName
				return &DBConnection{pingFunc: func() error { return nil }, closeFunc: func() error { return nil }}, nil
			}
			isDriverRegistered = originalSQLDriverRegistered

			_, err := Connect(tc.dsn, tc.user, tc.pass)
			if err != nil {
				// Some DSNs might be "valid" structurally but would fail with a real DB
				// For this test, we're mainly checking the DSN string transformation.
				// If a DSN is truly malformed for *parsing*, it might error earlier.
				// This test expects Connect to successfully produce a DSN string to pass to sql.Open.
				// If the DSN format is very alien, it might not match expected transformations.
				fmt.Printf("Connect for DSN '%s' (user: %s) produced error: %v. Captured DSN: %s\n", tc.dsn, tc.user, err, capturedDSN)
                // Allow specific errors if they are due to DSN structure that Connect cannot resolve
                // For example, a DSN like "justhost" with no user/pass might error if it can't form a valid string.
                // For now, assume valid transformation if no error.
			}


			if capturedDriver != tc.expectedDriver {
				t.Errorf("DSN '%s': Expected driver '%s', got '%s'", tc.dsn, tc.expectedDriver, capturedDriver)
			}
			if capturedDSN != tc.expectedDSN {
				t.Errorf("DSN '%s': Expected final DSN '%s', got '%s'", tc.dsn, tc.expectedDSN, capturedDSN)
			}
		})
	}
}


// --- Mocking sql.Open and related types for testing Connect's DSN parsing ---
// This allows us to intercept what DSN string is passed to sql.Open without a live DB.

// DBConnection is an interface matching sql.DB's relevant methods for mocking
type DBConnection struct {
	pingFunc func() error
	closeFunc func() error
	// Add other methods if Connect starts using them directly (e.g., Prepare, Exec)
}
func (m *DBConnection) Ping() error { return m.pingFunc() }
func (m *DBConnection) Close() error { return m.closeFunc() }


// sqlOpen is a variable holding the function to open a DB connection.
// It's initialized to the actual sql.Open but can be replaced for testing.
var sqlOpen = func(driverName, dataSourceName string) (*DBConnection, error) {
	// This is a simplified stand-in. The actual sql.Open returns *sql.DB.
	// For testing DSN parsing, we don't need a full *sql.DB.
	// We'd need to match the interface more closely if we were testing deeper DB interactions here.
	// This mock is primarily to capture dataSourceName.
	// In a real scenario with sql.Open, you'd get an *sql.DB, and then you'd need to mock
	// its Ping, Close etc. methods if you are testing functions that use those.
	// For now, this structure is to make the test compile and capture DSN.
	// The Connect function in database.go would need to return a compatible type if we change this signature.
	// Let's assume our Connect function internally uses a type compatible with DBConnection for sql.Open's result.
	// For the current test, we only care that sqlOpen is called with the right args.
	// The *DB type in database.go wraps *sql.DB. So this mock needs to align.
	// To make this work without changing Connect too much, this mock is very basic.
	// A better approach for testing Connect's DSN logic would be to export the DSN parsing part.
	panic("sqlOpen mock called directly in test setup - should be replaced by specific test case mocks")
}

// isDriverRegistered mimics sql.Drivers() check, can be mocked.
var isDriverRegistered = func(name string) bool {
    // In real tests, this could check a list of mocked registered drivers.
    // For DSN parsing tests, we often assume the driver *would* be registered.
    driversList := []string{"mysql", "sqlite3"} // Example
    for _, d := range driversList {
        if d == name {
            return true
        }
    }
    return false
}

// Note: The Connect function was modified to use these mocked sqlOpen and isDriverRegistered
// for the purpose of unit testing its DSN parsing logic without external dependencies.
// This requires temporarily commenting out or build-tagging the actual sql.Open call
// within the database.go file, or making sqlOpen an internal variable that can be swapped.
// The current database.go Connect doesn't use isDriverRegistered directly, it tries sql.Open.
// The mock setup above is a common pattern but requires the function under test to be designed for it
// or to use interface-based dependencies.

// For the current structure of Connect, the test will rely on sqlOpen being swappable.
// The DBConnection mock and its use in sqlOpen mock is to satisfy the call to db.Ping() in Connect.

// To make this fully work, database.go's Connect would look like:
/*
func Connect(dsn, user, pass string) (*DB, error) {
    // ... dsn parsing ...
    driverName, finalDSN := parseLogic(dsn, user, pass) // Extracted parsing logic

    // In test, sqlOpen is the mock. In real code, it's sql.Open (or our wrapper).
    rawDB, err := sqlOpen(driverName, finalDSN) // sqlOpen returns our mockable DBConnection type in tests
    if err != nil { // ... error handling ...}
    if err := rawDB.Ping(); err != nil { // ... error handling ...}
    return &DB{rawDB.(*sql.DB)}, nil // This cast is problematic if sqlOpen mock returns different type.
                                   // Better if DB struct holds an interface type.
}
*/
// The current tests are designed to capture the arguments to the (mocked) sql.Open.
// The actual *DB returned by Connect will be based on the mocked DBConnection.

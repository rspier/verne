package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/user/colobus/internal/config"
	"github.com/user/colobus/internal/database"
	"github.com/user/colobus/internal/nntp"
)

func main() {
	configFile := flag.String("config", "config", "Path to configuration file")
	listenAddr := flag.String("listen", ":1119", "Address and port to listen on (e.g., :119 or 0.0.0.0:119)")
	flag.Parse()

	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Error loading configuration from %s: %v", *configFile, err)
	}
	log.Printf("Configuration loaded. ServerName: %s", cfg.ServerName)

	db, err := database.Connect(cfg.DSN, cfg.DBUser, cfg.DBPass)
	if err != nil {
		log.Fatalf("Failed to connect to database using DSN '%s' (user: '%s'): %v", cfg.DSN, cfg.DBUser, err)
	}
	defer db.Close()
	log.Println("Database connection established.")

	groupNameToNum, groupNumToName, err := db.EnsureGroupsExistInDB(cfg.Groups)
	if err != nil {
		log.Fatalf("Failed to synchronize groups with database: %v", err)
	}
	// Update cfg.Groups with potentially new/corrected numbers from DB sync
	// This ensures the config object used by the server reflects the DB reality.
	for name, num := range groupNameToNum {
		if grpCfg, ok := cfg.Groups[name]; ok {
			if grpCfg.Num != num && grpCfg.Num != 0 { // Log if a specified number was different (should be handled by EnsureGroupsExistInDB checks)
				log.Printf("Group '%s' number in config was %d, effective number after DB sync is %d.", name, grpCfg.Num, num)
			} else if grpCfg.Num == 0 && num != 0 { // Log if a number was assigned
				log.Printf("Group '%s' assigned number %d after DB sync.", name, num)
			}
			grpCfg.Num = num // Ensure the config struct has the definitive number
		} else {
			// This case should ideally not happen if EnsureGroupsExistInDB is based on cfg.Groups
			log.Printf("Warning: Group '%s' (num %d) from DB sync not found in initial config map. This is unexpected.", name, num)
		}
	}


	server, err := nntp.NewServer(cfg, db, groupNameToNum, groupNumToName)
	if err != nil {
		log.Fatalf("Failed to create NNTP server: %v", err)
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigChan
		log.Printf("Received signal %s, shutting down...", sig)
		server.Shutdown()
		// db.Close() is deferred in main
		log.Println("Server shutdown initiated.")
	}()

	log.Printf("Starting Colobus server on %s...", *listenAddr)
	if err := server.ListenAndServe(*listenAddr); err != nil {
		if !isClosedConnError(err) {
			log.Fatalf("NNTP server error: %v", err)
		} else {
			log.Println("NNTP server stopped gracefully.")
		}
	} else {
		log.Println("Colobus server has stopped.")
	}
}

func isClosedConnError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "listener closed")
}

func init() {
	const defaultConfigPath = "config"
	if _, err := os.Stat(defaultConfigPath); os.IsNotExist(err) {
		log.Printf("No '%s' file found. Creating '%s.example' with default values.", defaultConfigPath, defaultConfigPath)
		log.Printf("Please review and rename it to '%s' or use the -config flag.", defaultConfigPath)
		dummyConfig := `
# Colobus Configuration Example
servername = news.example.com
dsn = DBI:mysql:database=colobus_db;host=localhost
dbuser = your_mysql_user
dbpass = your_mysql_password
timeout = 3600
disallow = xpat,newnews,xrover
mailinject = /var/qmail/bin/qmail-inject -a

[group example.test]
path = /var/spool/news/example/test
# num = 1 # Optional: If num is specified, it's asserted. If not, it's assigned/synced.
mail = test-group@example.com
moderated = false
desc = This is an example test group.

[group example.moderated]
path = /var/spool/news/example/moderated
# num = 2
mail = moderated-submit@example.com
moderated = true
desc = This is an example moderated group.
followup = example.test

[group misc.test]
path = /var/spool/news/misc/test
# num = 3
mail = misctest@example.com
desc = Another test group for various things.
hidden = true
`
		err := os.WriteFile(defaultConfigPath+".example", []byte(dummyConfig), 0644)
		if err != nil {
			log.Printf("Warning: Failed to write %s.example: %v", defaultConfigPath, err)
		}
	}
}

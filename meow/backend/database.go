package main

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"log/slog"
)

func InitDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "./meow.db")
	if err != nil {
		return nil, err
	}

	statement := `
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY,
			google_id TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			avatar_url TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS channels (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		);
		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY,
			channel_id INTEGER,
			user_id INTEGER,
			content TEXT NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(channel_id) REFERENCES channels(id),
			FOREIGN KEY(user_id) REFERENCES users(id)
		);
		CREATE TABLE IF NOT EXISTS direct_messages (
			id INTEGER PRIMARY KEY,
			sender_id INTEGER,
			receiver_id INTEGER,
			content TEXT NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(sender_id) REFERENCES users(id),
			FOREIGN KEY(receiver_id) REFERENCES users(id)
		);
	`
	_, err = db.Exec(statement)
	if err != nil {
		return nil, err
	}
	slog.Info("database tables created successfully")
	return db, nil
}

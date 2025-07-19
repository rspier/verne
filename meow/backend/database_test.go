package main

import (
	"os"
	"testing"
)

func TestDatabase(t *testing.T) {
	// Setup
	db, err := InitDB()
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	defer db.Close()
	defer os.Remove("./meow.db")

	store := NewStore(db)

	// Test CreateUser
	userID, err := store.CreateUser("test_google_id", "test_user", "test_avatar_url")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if userID == 0 {
		t.Fatal("expected user id to be non-zero")
	}

	// Test CreateChannel
	channelID, err := store.CreateChannel("test_channel")
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}
	if channelID == 0 {
		t.Fatal("expected channel id to be non-zero")
	}

	// Test CreateMessage
	messageID, err := store.CreateMessage(channelID, userID, "test_message")
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}
	if messageID == 0 {
		t.Fatal("expected message id to be non-zero")
	}

	// Test CreateDirectMessage
	dmID, err := store.CreateDirectMessage(userID, userID, "test_dm")
	if err != nil {
		t.Fatalf("failed to create direct message: %v", err)
	}
	if dmID == 0 {
		t.Fatal("expected direct message id to be non-zero")
	}
}

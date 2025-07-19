package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAPI(t *testing.T) {
	// Setup
	db, err := InitDB()
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	defer db.Close()
	defer os.Remove("./meow.db")

	store := NewStore(db)
	broker := NewBroker()
	go broker.Start()
	api := NewAPI(store, broker)

	// Create a test user and channel
	userID, _ := store.CreateUser("test_google_id", "test_user", "test_avatar_url")
	channelID, _ := store.CreateChannel("test_channel")

	// Test handleSendMessage
	msg := map[string]interface{}{
		"channel_id": channelID,
		"user_id":    userID,
		"content":    "test message",
	}
	body, _ := json.Marshal(msg)
	req, err := http.NewRequest("POST", "/messages/send", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(api.handleSendMessage)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusCreated {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusCreated)
	}

	// Test handleGetMessages
	req, err = http.NewRequest("GET", "/messages?channel_id=1", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr = httptest.NewRecorder()
	handler = http.HandlerFunc(api.handleGetMessages)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	var messages []*Message
	json.NewDecoder(rr.Body).Decode(&messages)

	if len(messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(messages))
	}
}

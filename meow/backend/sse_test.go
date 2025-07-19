package main

import (
	"testing"
	"time"
)

func TestBroker(t *testing.T) {
	broker := NewBroker()
	go broker.Start()

	client := make(chan string)
	broker.newClients <- client

	// Test message broadcasting
	broker.messages <- "test message"
	select {
	case msg := <-client:
		if msg != "test message" {
			t.Errorf("unexpected message received: got %s, want %s", msg, "test message")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for message")
	}

	// Test client disconnection
	broker.defunctClients <- client
	select {
	case _, ok := <-client:
		if ok {
			t.Fatal("client channel should be closed")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for client channel to close")
	}
}

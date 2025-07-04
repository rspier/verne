package nntpclient

import (
	"testing"
	"nntp-web/internal/config"
	"strings"
)

func TestNewClient(t *testing.T) {
	t.Run("valid server address (but likely unreachable)", func(t *testing.T) {
		// This test will attempt a real connection, which will likely fail for "test.nntp.server:119",
		// but it tests the path where a server address is provided.
		// A more robust test would use a mock NNTP server.
		cfg := &config.Config{NNTPServer: "test.nntp.server:119"}
		client, err := NewClient(cfg)
		if err == nil {
			// If it somehow connected, close it.
			client.Close()
			t.Logf("NewClient unexpectedly succeeded for %s. This test assumes it would fail to connect.", cfg.NNTPServer)
			// This is not a failure of NewClient's logic itself if the server was real.
		} else {
			// We expect an error because "test.nntp.server:119" is not real.
			t.Logf("NewClient failed as expected for dummy server: %v", err)
			if !strings.Contains(err.Error(), "nntp: failed to dial") && !strings.Contains(err.Error(), "no such host") && !strings.Contains(err.Error(), "connection refused") {
				t.Errorf("Expected dial error, got: %v", err)
			}
		}
	})

	t.Run("empty server address", func(t *testing.T) {
		cfg := &config.Config{NNTPServer: ""}
		_, err := NewClient(cfg)
		if err == nil {
			t.Errorf("NewClient should return an error for empty server address, got nil")
		} else if !strings.Contains(err.Error(), "nntp server address is not configured") {
			t.Errorf("NewClient error message mismatch: got '%v', want contains '%s'", err, "nntp server address is not configured")
		}
	})

	// Test for greeting failure would require a mock server that sends bad greeting.
	// Test for MODE READER response would also require a mock server.
}

// TestFetchArticleBody would require a mock NNTP server or a live server with known data.
// For now, its functionality is indirectly checked via handler tests if a connection can be established.
// func TestFetchArticleBody(t *testing.T) { ... }

// TestClient_Close tests the Close method.
func TestClient_Close(t *testing.T) {
	t.Run("close nil connection", func(t *testing.T) {
		client := &Client{} // No connection
		err := client.Close()
		if err != nil {
			t.Errorf("Close() on a client with nil connection should not error, got %v", err)
		}
	})

	// Test for closing an active connection would require a mock server to establish one.
}

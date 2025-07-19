package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAuth(t *testing.T) {
	// Setup
	os.Setenv("GOOGLE_CLIENT_ID", "test")
	os.Setenv("GOOGLE_CLIENT_SECRET", "test")

	// Test handleGoogleLogin
	req, err := http.NewRequest("GET", "/login", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(handleGoogleLogin)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusTemporaryRedirect {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusTemporaryRedirect)
	}
}

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"nntp-browser-app/backend/pkg/models"
	"reflect"
	"strings"
	"testing"
)

func TestGetGroupsHandler(t *testing.T) {
	req, err := http.NewRequest("GET", "/api/groups", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(GetGroupsHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	expectedContentType := "application/json"
	if contentType := rr.Header().Get("Content-Type"); contentType != expectedContentType {
		t.Errorf("handler returned wrong content type: got %v want %v",
			contentType, expectedContentType)
	}

	var groups []models.Group
	if err := json.NewDecoder(rr.Body).Decode(&groups); err != nil {
		t.Fatalf("could not decode response: %v", err)
	}

	// Basic check for non-empty list and expected structure.
	// A more thorough test would check specific values.
	if len(groups) == 0 {
		t.Errorf("expected non-empty list of groups")
	}
	if groups[0].Name == "" {
		t.Errorf("expected group name to be populated")
	}
}

func TestGetMessagesHandler(t *testing.T) {
	// Test with a sample group name
	groupName := "comp.lang.go"
	req, err := http.NewRequest("GET", "/api/groups/"+groupName+"/messages", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	// To test this properly with the current router setup, we need to simulate the path parsing.
	// The Router() function sets up a mux that would handle this.
	// For a unit test of the handler itself, we can call it directly.
	// However, our GetMessagesHandler parses the groupName from r.URL.Path.
	// So, the request URL must match what the handler expects.

	// We need a router instance to correctly dispatch to GetMessagesHandler
	testRouter := Router()
	testRouter.ServeHTTP(rr, req)


	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code for GetMessagesHandler: got %v want %v. Body: %s",
			status, http.StatusOK, rr.Body.String())
	}

	expectedContentType := "application/json"
	if contentType := rr.Header().Get("Content-Type"); contentType != expectedContentType {
		t.Errorf("handler returned wrong content type: got %v want %v",
			contentType, expectedContentType)
	}

	var messages []models.MessageOverview
	if err := json.NewDecoder(rr.Body).Decode(&messages); err != nil {
		t.Fatalf("could not decode response: %v. Body: %s", err, rr.Body.String())
	}

	if len(messages) == 0 {
		t.Errorf("expected non-empty list of messages")
	}
	if messages[0].Group != groupName {
		t.Errorf("expected message group to be '%s', got '%s'", groupName, messages[0].Group)
	}
}

func TestGetMessageHandler(t *testing.T) {
	groupName := "comp.lang.go"
	messageID := "msg1@example.com"
	req, err := http.NewRequest("GET", "/api/groups/"+groupName+"/messages/"+messageID, nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	testRouter := Router()
	testRouter.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code for GetMessageHandler: got %v want %v. Body: %s",
			status, http.StatusOK, rr.Body.String())
	}

	var message models.Message
	if err := json.NewDecoder(rr.Body).Decode(&message); err != nil {
		t.Fatalf("could not decode response: %v. Body: %s", err, rr.Body.String())
	}

	if !strings.Contains(message.ID, messageID) { // The mock handler appends the messageID to a string
		t.Errorf("expected message ID to contain '%s', got '%s'", messageID, message.ID)
	}
	if message.Group != groupName {
		t.Errorf("expected message group to be '%s', got '%s'", groupName, message.Group)
	}
	if message.Body == "" {
		t.Errorf("expected message body to be populated")
	}
}

func TestRouter(t *testing.T) {
	router := Router()

	ts := httptest.NewServer(router)
	defer ts.Close()

	// Test /api/groups
	res, err := http.Get(ts.URL + "/api/groups")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("GET /api/groups: expected status %d, got %d", http.StatusOK, res.StatusCode)
	}

	// Test /api/groups/{groupName}/messages
	res, err = http.Get(ts.URL + "/api/groups/alt.test/messages")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("GET /api/groups/alt.test/messages: expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
	var messages []models.MessageOverview
	if err := json.NewDecoder(res.Body).Decode(&messages); err != nil {
		t.Fatalf("could not decode response for messages: %v", err)
	}
	res.Body.Close()
	if len(messages) == 0 || messages[0].Group != "alt.test" {
		t.Errorf("unexpected message data for alt.test: %+v", messages)
	}


	// Test /api/groups/{groupName}/messages/{messageId}
	res, err = http.Get(ts.URL + "/api/groups/alt.test/messages/testmsg@123")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("GET /api/groups/alt.test/messages/testmsg@123: expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
	var msg models.Message
	if err := json.NewDecoder(res.Body).Decode(&msg); err != nil {
		t.Fatalf("could not decode response for single message: %v", err)
	}
	res.Body.Close()
	if msg.Group != "alt.test" || !strings.Contains(msg.ID, "testmsg@123") {
		t.Errorf("unexpected single message data: %+v", msg)
	}


	// Test invalid path
	res, err = http.Get(ts.URL + "/api/invalidpath")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNotFound { // Default Mux behavior for unhandled paths under /api/groups/ due to prefix match
		t.Errorf("GET /api/invalidpath: expected status %d, got %d", http.StatusNotFound, res.StatusCode)
	}
	res.Body.Close()

	// Test /api/groups/groupname/invalidsubpath
	res, err = http.Get(ts.URL + "/api/groups/alt.test/invalidsubpath")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("GET /api/groups/alt.test/invalidsubpath: expected status %d, got %d", http.StatusNotFound, res.StatusCode)
	}
	res.Body.Close()
}

// Helper function to compare slices of structs (useful if order doesn't matter or for complex structs)
func AreSlicesEqual(s1, s2 interface{}) bool {
    // For this basic test, direct comparison or length check is enough.
    // For more complex scenarios, consider libraries like go-cmp.
    return reflect.DeepEqual(s1, s2)
}

package api

import (
	"encoding/json"
	"net/http"
	"nntp-browser-app/backend/pkg/models"
	"strings"
	"time"

	// It's good practice to alias a router if you use one, e.g. "gorilla/mux"
	// For now, we'll use net/http's default ServeMux which is fine for simple cases.
)

// GetGroupsHandler returns a list of mock NNTP groups.
func GetGroupsHandler(w http.ResponseWriter, r *http.Request) {
	mockGroups := []models.Group{
		{Name: "comp.lang.go", Description: "Discussions about Go", Count: 12345, High: 12350, Low: 1},
		{Name: "alt.humor.puns", Description: "Puns, puns, and more puns", Count: 5678, High: 5680, Low: 10},
		{Name: "sci.space.news", Description: "Space news and announcements", Count: 91011, High: 91020, Low: 50},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(mockGroups)
}

// GetMessagesHandler returns a list of mock messages for a group.
// Path: /api/groups/{groupName}/messages
func GetMessagesHandler(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// Expected path: api/groups/{groupName}/messages
	if len(pathParts) < 4 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	groupName := pathParts[2]

	// TODO: Add pagination and filtering by month based on query params
	// For now, returning a static list

	mockMessages := []models.MessageOverview{
		{ID: "<msg1@example.com>", Group: groupName, Number: 101, Subject: "Hello from Go", From: "Go User <go@example.com>", Date: time.Now().Add(-24 * time.Hour), MessageCountInThread: 3},
		{ID: "<msg2@example.com>", Group: groupName, Number: 102, Subject: "Re: Hello from Go", From: "Another User <another@example.com>", Date: time.Now().Add(-23 * time.Hour), MessageCountInThread: 3},
		{ID: "<msg3@example.com>", Group: groupName, Number: 103, Subject: "React is cool", From: "React Fan <react@example.com>", Date: time.Now().Add(-22 * time.Hour), MessageCountInThread: 1},
		{ID: "<msg4@example.com>", Group: groupName, Number: 104, Subject: "Re: Re: Hello from Go", From: "Go User <go@example.com>", Date: time.Now().Add(-21 * time.Hour), MessageCountInThread: 3},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(mockMessages)
}

// GetMessageHandler returns a single mock message and its thread context.
// Path: /api/groups/{groupName}/messages/{messageId}
func GetMessageHandler(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// Expected path: api/groups/{groupName}/messages/{messageId}
	if len(pathParts) < 5 {
		http.Error(w, "Invalid path, missing messageId", http.StatusBadRequest)
		return
	}
	groupName := pathParts[2]
	// messageId is URL encoded, but for mock, we'll use it as is.
	// In a real scenario, you'd URL decode it.
	messageID := pathParts[4]

	// Mock parent and child for the thread context
	prevID := "prev-id@example.com"
	nextID := "next-id@example.com"
	parentID := "parent-id@example.com"

	mockMessage := models.Message{
		ID:         messageID,
		Group:      groupName,
		Number:     101, // Mock number
		Subject:    "Example Message: " + messageID,
		From:       "Mock User <mock@example.com>",
		Date:       time.Now().Add(-48 * time.Hour),
		References: []string{"<ref1@example.com>", "<ref2@example.com>"},
		Body:       "This is the body of the mock message.\n\nIt has multiple lines.",
		PrevMessage: &prevID,
		NextMessage: &nextID,
		Parent: &parentID,
		Children: []*models.Message{
			{
				ID:      nextID,
				Group:   groupName,
				Number:  102,
				Subject: "Re: Example Message",
				From:    "Child User <child@example.com>",
				Date:    time.Now().Add(-47 * time.Hour),
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(mockMessage)
}

// Router sets up the routes for the API.
// It returns an http.Handler, so it can be used with http.ListenAndServe.
func Router() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/groups", GetGroupsHandler)
	// Note: NewServeMux needs more specific path handling for parameters.
	// A library like gorilla/mux or chi would be better for {groupName} and {messageId}.
	// For now, we parse the path manually in handlers.
	// This means /api/groups/{groupName}/messages and /api/groups/{groupName}/messages/{messageId}
	// will both initially match the most general path registered, so we need to be careful.
	// A common pattern is to register one handler for a prefix and then delegate.
	// For simplicity, let's register distinct-enough paths for now,
	// but this will need refinement or a better router.

	// This will match /api/groups/ANYTHING/messages or /api/groups/ANYTHING/messages/ANYTHING_ELSE
	// We'll need to distinguish in the handler or use a more capable router.
	// Let's create a handler that can distinguish.

	// We will create specific handlers for these patterns.
	// This will require careful path parsing in the handlers.
	// A more robust solution would involve a router that supports path parameters.
	// For now, we'll handle it like this:
	// /api/groups/{groupName}/messages
	// /api/groups/{groupName}/messages/{messageID}

	// A simple way to handle this with NewServeMux is to have a single handler for a base path
	// and then parse the rest of the path.
	// For example, handle all /api/groups/ requests and then determine if it's for messages or a specific message.

	// Let's try registering more specific paths if possible, or use a prefix and parse.
	// For NewServeMux:
	// A path ending in / matches all paths that have it as a prefix.
	// So, /api/groups/ will match /api/groups/comp.lang.go/messages etc.

	groupSpecificHandler := func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/groups/")
		parts := strings.Split(path, "/")
		// parts[0] is groupName
		// if len(parts) == 2 and parts[1] == "messages", it's GetMessagesHandler
		// if len(parts) == 3 and parts[1] == "messages", it's GetMessageHandler (parts[2] is messageId)

		if len(parts) >= 2 && parts[1] == "messages" {
			if len(parts) == 2 { // e.g. /api/groups/groupname/messages
				GetMessagesHandler(w, r) // It will parse groupName itself
			} else if len(parts) == 3 { // e.g. /api/groups/groupname/messages/messageid
				GetMessageHandler(w, r) // It will parse groupName and messageId itself
			} else {
				http.NotFound(w, r)
			}
		} else {
			http.NotFound(w, r)
		}
	}

	mux.HandleFunc("/api/groups/", func(w http.ResponseWriter, r *http.Request) {
		// If path is exactly /api/groups or /api/groups/
		if r.URL.Path == "/api/groups" || r.URL.Path == "/api/groups/" {
			if r.Method == http.MethodGet {
				GetGroupsHandler(w, r)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
		// Otherwise, it's for a specific group's messages or a single message
		groupSpecificHandler(w,r)
	})


	return mux
}

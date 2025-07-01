package api

import (
	// "bufio" // No longer needed
	"encoding/json"
	"fmt"
	// "io" // No longer needed
	"log"
	"net/http"
	// "net/textproto" // No longer needed
	"nntp-browser-app/backend/pkg/config"
	"nntp-browser-app/backend/pkg/models"
	"nntp-browser-app/backend/pkg/nntpclient"
	"nntp-browser-app/backend/pkg/threading"
	// "strconv" // No longer needed
	// "strings" // No longer needed
	// "time" // No longer needed

	// kclient "github.com/kothawoc/go-nntp/client" // Not directly used by handlers
	// krootnntp "github.com/kothawoc/go-nntp"
	"github.com/julienschmidt/httprouter"
)

// APIHandler holds dependencies for API handlers, like an NNTP client.
type APIHandler struct {
	nntpClient *nntpclient.NNTPClient
}

// NewAPIHandler creates a new APIHandler with an initialized NNTP client.
func NewAPIHandler(cfg config.AppConfig) (*APIHandler, error) {
	client, err := nntpclient.New(cfg)
	if err != nil {
		log.Printf("Warning: Failed to initialize NNTP client: %v. API might serve errors or limited data.", err)
		return &APIHandler{nntpClient: nil}, fmt.Errorf("failed to create nntp client: %w", err)
	}
	return &APIHandler{nntpClient: client}, nil
}

// Close cleans up resources used by the APIHandler.
func (h *APIHandler) Close() {
	if h.nntpClient != nil {
		h.nntpClient.Close()
	}
}

// GetGroupsHandler fetches and returns a list of NNTP groups.
func (h *APIHandler) GetGroupsHandler(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if h.nntpClient == nil {
		http.Error(w, "NNTP client not available", http.StatusInternalServerError)
		return
	}
	groups, err := h.nntpClient.GetGroups()
	if err != nil {
		log.Printf("Error getting groups from NNTP client: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(groups)
}

// GetMessagesHandler returns a list of messages for a group.
func (h *APIHandler) GetMessagesHandler(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if h.nntpClient == nil {
		http.Error(w, "NNTP client not available", http.StatusInternalServerError)
		return
	}
	groupName := ps.ByName("groupName")
	if groupName == "" {
		http.Error(w, "Group name is required", http.StatusBadRequest)
		return
	}

	_, low, high, err := h.nntpClient.SelectGroup(groupName)
	if err != nil {
		log.Printf("Error selecting group %s: %v", groupName, err)
		http.Error(w, fmt.Sprintf("Could not select group %s: %s", groupName, err.Error()), http.StatusInternalServerError)
		return
	}

	articleCount := high - low + 1
	if articleCount <= 0 || high < low {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]models.MessageOverview{})
		return
	}

	// Determine fetch range (e.g., last 100 messages)
	// TODO: Implement proper pagination from query params
	fetchCount := 100
	if articleCount < fetchCount {
		fetchCount = articleCount
	}
	startArticleNum := high - fetchCount + 1
    if startArticleNum < low {
        startArticleNum = low
    }

	// Construct article number range for Over command if it takes individual numbers.
	// kothawoc/go-nntp/client.Over takes ...int.
	// If we want a range, we need to generate that slice or see if it supports "first-last" string.
	// The docs `Over(args ...int)` suggests individual numbers or a range passed as two numbers.
	// Let's assume it takes first, last for a range.
	// nntpclient.GetArticleOverviews takes (first, last int64) and returns []nntp.MessageOverview (from willglynn/nntp)
	nntpOverviews, err := h.nntpClient.GetArticleOverviews(int64(startArticleNum), int64(high))
	if err != nil {
		log.Printf("Error getting article overviews for group %s: %v", groupName, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
    // Extra brace removed from here

    fullMessagesForThreading := make([]*models.Message, len(nntpOverviews))
    msgMapForThreading := make(map[string]*models.Message)

    for i, overview := range nntpOverviews {
        // overview is nntp.MessageOverview from willglynn/nntp
        // Its fields: MessageNumber int64, Subject string, From string, Date time.Time,
        // MessageId string, References []string, Bytes int, Lines int
        // MessageId and References are already trimmed by nntpclient.GetArticleOverviews

        msg := &models.Message{
            ID:         overview.MessageId,
            Group:      groupName,
            Number:     overview.MessageNumber,
            Subject:    overview.Subject,
            From:       overview.From,
            Date:       overview.Date,
            References: overview.References, // This is already []string
        }
        fullMessagesForThreading[i] = msg
        msgMapForThreading[msg.ID] = msg
    }

    threading.ThreadMessages(fullMessagesForThreading)

    responseOverviews := make([]models.MessageOverview, len(fullMessagesForThreading))
    for i, threadedMsg := range fullMessagesForThreading {
        threadRoot := threading.GetThreadRoot(threadedMsg.ID, msgMapForThreading)
        threadCount := 0
        if threadRoot != nil {
            threadCount = threading.CountMessagesInThread(threadRoot, msgMapForThreading)
        }
        responseOverviews[i] = models.MessageOverview{
            ID:                   threadedMsg.ID,
            Group:                threadedMsg.Group,
            Number:               threadedMsg.Number,
            Subject:              threadedMsg.Subject,
            From:                 threadedMsg.From,
            Date:                 threadedMsg.Date,
            MessageCountInThread: threadCount,
        }
    }

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responseOverviews) // Encode responseOverviews
}

// GetMessageHandler returns a single message and its thread context.
func (h *APIHandler) GetMessageHandler(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if h.nntpClient == nil {
		http.Error(w, "NNTP client not available", http.StatusInternalServerError)
		return
	}
	groupName := ps.ByName("groupName")
	messageIDFromPath := ps.ByName("messageId")

	// nntpclient.GetArticleBody now returns *models.Message (already parsed)
	responseMessage, err := h.nntpClient.GetArticleBody(messageIDFromPath)
	if err != nil {
		log.Printf("Error getting article body for ID %s in group %s: %v", messageIDFromPath, groupName, err)
		http.Error(w, fmt.Sprintf("Could not get article %s: %s", messageIDFromPath, err.Error()), http.StatusNotFound)
		return
	}
    responseMessage.Group = groupName // Ensure group is set

	// TODO: Full thread context (Parent, Children, Next/Prev) for responseMessage
	// This would involve fetching more messages based on responseMessage.References,
	// messages that reference it, etc., and then applying threading.
	// For now, these fields will be empty/nil in responseMessage as filled by GetArticleBody.

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responseMessage)
}

// Router initializes and returns the HTTP router for the API.
func Router(apiHandler *APIHandler) *httprouter.Router {
	router := httprouter.New()
	router.GET("/api/groups", apiHandler.GetGroupsHandler)
	router.GET("/api/groups/:groupName/messages", apiHandler.GetMessagesHandler)
	router.GET("/api/groups/:groupName/messages/:messageId", apiHandler.GetMessageHandler)
	return router
}

// Ensure kclient and krootnntp are imported if their types are used directly here.
// For now, they are encapsulated in nntpclient results or models.
// var _ = kclient.OverItem{} // kclient import removed
// var _ = krootnntp.Group{}
// var _ = bufio.NewReader(nil) // bufio import removed
// var _ = textproto.NewReader(nil) // textproto import removed

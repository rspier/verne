package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type API struct {
	store  *Store
	broker *Broker
}

func NewAPI(store *Store, broker *Broker) *API {
	return &API{store: store, broker: broker}
}

func (a *API) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var msg struct {
		ChannelID int64  `json:"channel_id"`
		UserID    int64  `json:"user_id"`
		Content   string `json:"content"`
	}

	err := json.NewDecoder(r.Body).Decode(&msg)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	_, err = a.store.CreateMessage(msg.ChannelID, msg.UserID, msg.Content)
	if err != nil {
		http.Error(w, "Failed to create message", http.StatusInternalServerError)
		return
	}

	a.broker.messages <- msg.Content
	w.WriteHeader(http.StatusCreated)
}

func (a *API) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	channelID, err := strconv.ParseInt(r.URL.Query().Get("channel_id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid channel_id", http.StatusBadRequest)
		return
	}

	// TODO: Implement GetMessagesByChannelID in store.go
	messages, err := a.store.GetMessagesByChannelID(channelID)
	if err != nil {
		http.Error(w, "Failed to get messages", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(messages)
}

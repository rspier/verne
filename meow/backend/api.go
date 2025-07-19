package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
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

	sanitizedContent := Sanitize(msg.Content)

	_, err = a.store.CreateMessage(msg.ChannelID, msg.UserID, sanitizedContent)
	if err != nil {
		http.Error(w, "Failed to create message", http.StatusInternalServerError)
		return
	}

	a.broker.messages <- sanitizedContent
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

	messages, err := a.store.GetMessagesByChannelID(channelID)
	if err != nil {
		http.Error(w, "Failed to get messages", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(messages)
}

func (a *API) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	file, handler, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Failed to get file from form", http.StatusBadRequest)
		return
	}
	defer file.Close()

	err = os.MkdirAll("./uploads", os.ModePerm)
	if err != nil {
		http.Error(w, "Failed to create uploads directory", http.StatusInternalServerError)
		return
	}

	f, err := os.OpenFile(filepath.Join("./uploads", handler.Filename), os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		http.Error(w, "Failed to open file for writing", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	_, err = io.Copy(f, file)
	if err != nil {
		http.Error(w, "Failed to copy file", http.StatusInternalServerError)
		return
	}

	slog.Info("file uploaded successfully", "filename", handler.Filename)
	json.NewEncoder(w).Encode(map[string]string{"url": "/uploads/" + handler.Filename})
}

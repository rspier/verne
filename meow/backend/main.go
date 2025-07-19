package main

import (
	"log/slog"
	"net/http"
	"os"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	db, err := InitDB()
	if err != nil {
		slog.Error("failed to initialize database", "error", err)
		return
	}
	defer db.Close()

	store := NewStore(db)
	broker := NewBroker()
	go broker.Start()
	api := NewAPI(store, broker)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		slog.Info("request received", "method", r.Method, "path", r.URL.Path)
		w.Write([]byte("Hello from the Meow backend!"))
	})
	mux.HandleFunc("/login", handleGoogleLogin)
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		handleGoogleCallback(w, r, store)
	})
	mux.Handle("/events", broker)
	mux.HandleFunc("/messages", api.handleGetMessages)
	mux.HandleFunc("/messages/send", api.handleSendMessage)

	slog.Info("starting server", "port", 8080)
	if err := http.ListenAndServe(":8080", mux); err != nil {
		slog.Error("server failed to start", "error", err)
	}
}

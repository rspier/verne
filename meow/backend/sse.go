package main

import (
	"fmt"
	"log/slog"
	"net/http"
)

type Broker struct {
	clients    map[chan string]bool
	newClients chan chan string
	defunctClients chan chan string
	messages   chan string
}

func (b *Broker) Start() {
	for {
		select {
		case s := <-b.newClients:
			b.clients[s] = true
			slog.Info("client connected")
		case s := <-b.defunctClients:
			delete(b.clients, s)
			close(s)
			slog.Info("client disconnected")
		case msg := <-b.messages:
			for s := range b.clients {
				s <- msg
			}
			slog.Info("message sent to clients", "message", msg)
		}
	}
}

func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	messageChan := make(chan string)
	b.newClients <- messageChan
	defer func() {
		b.defunctClients <- messageChan
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for {
		select {
		case msg, open := <-messageChan:
			if !open {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			f.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func NewBroker() *Broker {
	return &Broker{
		clients:    make(map[chan string]bool),
		newClients: make(chan (chan string)),
		defunctClients: make(chan (chan string)),
		messages:   make(chan string),
	}
}

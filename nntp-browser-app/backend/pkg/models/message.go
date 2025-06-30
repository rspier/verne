package models

import "time"

// Message represents an NNTP article/message.
type Message struct {
	ID          string    `json:"id"`           // Message-ID header
	Group       string    `json:"group"`        // Newsgroup it belongs to
	Number      int64     `json:"number"`       // Article number in the group
	Subject     string    `json:"subject"`
	From        string    `json:"from"`
	Date        time.Time `json:"date"`
	References  []string  `json:"references"`   // References header, for threading
	Body        string    `json:"body"`         // Full body of the message
	NextMessage *string   `json:"nextMessage"`  // Message-ID of the next message in the thread
	PrevMessage *string   `json:"prevMessage"`  // Message-ID of the previous message in the thread
	Parent      *string   `json:"parent"`       // Message-ID of the parent message in the thread
	Children    []*Message `json:"children"`    // Children in the thread
}

// MessageOverview is a lighter version of Message for list views.
type MessageOverview struct {
	ID         string    `json:"id"`      // Message-ID header
	Group      string    `json:"group"`   // Newsgroup it belongs to
	Number     int64     `json:"number"`  // Article number in the group
	Subject    string    `json:"subject"`
	From       string    `json:"from"`
	Date       time.Time `json:"date"`
	MessageCountInThread int `json:"messageCountInThread"` // Number of messages in its thread
}

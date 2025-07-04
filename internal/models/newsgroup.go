package models

import "time"

// Newsgroup represents a newsgroup.
type Newsgroup struct {
	ID          uint16 `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`

	// Fields for displaying group statistics
	LastPostDate      *time.Time `json:"last_post_date,omitempty"` // Stores the full timestamp of the last post
	AvgPostsLastMonth float64    `json:"avg_posts_last_month,omitempty"`
	ShowAvgPosts      bool       `json:"show_avg_posts,omitempty"`
}

// Article represents an article in a newsgroup.
// Note: The SQL schema does not store the article body.
// Body content would need to be fetched from an NNTP server.
type Article struct {
	GroupID     uint16    `json:"group_id"`    // Corresponds to articles.group_id
	ArticleNum  uint32    `json:"article_num"` // Corresponds to articles.id (PK within group)
	MessageID   string    `json:"message_id"`  // Corresponds to articles.h_messageid (the actual <foo@bar.com>)
	Subject     string    `json:"subject"`     // Corresponds to articles.h_subject
	From        string    `json:"from"`        // Corresponds to articles.h_from
	Date        string    `json:"date"`        // Corresponds to articles.h_date (header string)
	Received    time.Time `json:"received"`    // Corresponds to articles.received (when processed by indexer)
	ThreadID    uint32    `json:"thread_id"`   // Corresponds to articles.thread_id
	ParentNum   uint32    `json:"parent_num"`  // Corresponds to articles.parent (parent's articles.id in the same group)
	References  []string  `json:"references"`  // Parsed from articles.h_references
	RawReferences string  `json:"-"`           // To hold the raw string from DB for parsing
	Lines       uint32    `json:"lines"`       // Corresponds to articles.h_lines
	Bytes       uint32    `json:"bytes"`       // Corresponds to articles.h_bytes

	// Fields to be populated by joining with `groups` table or for display logic
	GroupName   string `json:"group_name,omitempty"` // Name of the newsgroup
	DisplayFrom string `json:"display_from,omitempty"` // Obfuscated 'From' for display
}

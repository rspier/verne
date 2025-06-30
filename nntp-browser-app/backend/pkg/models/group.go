package models

// Group represents an NNTP newsgroup.
type Group struct {
	Name        string `json:"name"`
	Description string `json:"description"` // Usually not available directly from LIST command, might need LIST NEWSGROUPS
	Count       int64  `json:"count"`       // Estimated number of messages
	High        int64  `json:"high"`        // High watermark
	Low         int64  `json:"low"`         // Low watermark
}

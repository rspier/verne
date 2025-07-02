package templates

import (
	"embed"
	"io/fs"
)

//go:embed *.html.tmpl
var Files embed.FS

// Template names
const (
	GroupsList  = "groups.html.tmpl"
	MessageList = "message_list.html.tmpl"
	ArticleView = "article.html.tmpl"
	// Add other template names here as they are created
	// ErrorPage   = "error.html.tmpl" // Good to have a generic error page
)

// Get returns the embedded filesystem.
func Get() fs.FS {
	return Files
}

package main

import "github.com/microcosm-cc/bluemonday"

func Sanitize(html string) string {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("href").OnElements("a")
	return p.Sanitize(html)
}

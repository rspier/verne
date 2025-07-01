package nntpclient

import (
	"io"
	// "net/textproto" // nntp.Article uses map[string][]string for headers
	"strings"         // For mock
	"time"

	"github.com/rspier/wgnntp" // Using rspier/wgnntp fork
)

// NNTPConnection defines the interface for NNTP commands we'll use from rspier/wgnntp.
type NNTPConnection interface {
	// Group operations
	List(args ...string) ([]*nntp.Group, error)           // (c *Conn) List(a ...string) ([]*Group, error)
	Group(name string) (*nntp.Group, error)              // (c *Conn) Group(group string) (status *Group, err error)

	// Article overview
	// (c *Conn) Overview(begin, end int64) ([]MessageOverview, error)
	Overview(begin, end int64) ([]nntp.MessageOverview, error)

	// Fetching full article
	// (c *Conn) Article(id string) (*Article, error)
	// Article struct: Header map[string][]string, Body io.Reader
	Article(id string) (*nntp.Article, error)
	Head(id string) (*nntp.Article, error) // Returns *Article with nil Body

	Authenticate(user, pass string) error // (c *Conn) Authenticate(username, password string) error
	Quit() error                          // (c *Conn) Quit() error
	// Note: willglynn/nntp Conn doesn't have a separate Close that also Quits. Quit closes.
}

// nntpConnAdapter wraps a real *nntp.Conn (from willglynn/nntp)
type nntpConnAdapter struct {
    *nntp.Conn
}

// NewNNTPConnAdapter creates an adapter for a real nntp.Conn from willglynn/nntp
func NewNNTPConnAdapter(conn *nntp.Conn) NNTPConnection {
    return &nntpConnAdapter{Conn: conn}
}

// Implement NNTPConnection for nntpConnAdapter
// All methods should be directly promoted from the embedded *nntp.Conn
// if the signatures match the interface.

// MockNNTPConnection for testing
type MockNNTPConnection struct {
	ListFunc         func(args ...string) ([]*nntp.Group, error)
	GroupFunc        func(name string) (*nntp.Group, error)
	OverviewFunc     func(begin, end int64) ([]nntp.MessageOverview, error)
	ArticleFunc      func(id string) (*nntp.Article, error)
	HeadFunc         func(id string) (*nntp.Article, error)
	AuthenticateFunc func(user, pass string) error
	QuitFunc         func() error
}

func (m *MockNNTPConnection) List(args ...string) ([]*nntp.Group, error) { if m.ListFunc != nil { return m.ListFunc(args...) }; return nil, nil }
func (m *MockNNTPConnection) Group(name string) (*nntp.Group, error) { if m.GroupFunc != nil { return m.GroupFunc(name) }; return &nntp.Group{}, nil }
func (m *MockNNTPConnection) Overview(begin, end int64) ([]nntp.MessageOverview, error) { if m.OverviewFunc != nil { return m.OverviewFunc(begin,end) }; return nil, nil }
func (m *MockNNTPConnection) Article(id string) (*nntp.Article, error) { if m.ArticleFunc != nil { return m.ArticleFunc(id) }; return &nntp.Article{Header: map[string][]string{}, Body: strings.NewReader("")}, nil }
func (m *MockNNTPConnection) Head(id string) (*nntp.Article, error) { if m.HeadFunc != nil { return m.HeadFunc(id) }; return &nntp.Article{Header: map[string][]string{}, Body: nil}, nil } // Body is nil for Head
func (m *MockNNTPConnection) Authenticate(user, pass string) error { if m.AuthenticateFunc != nil { return m.AuthenticateFunc(user,pass) }; return nil }
func (m *MockNNTPConnection) Quit() error { if m.QuitFunc != nil { return m.QuitFunc() }; return nil }


// Mock helper functions for willglynn/nntp types
func MockWillglynnGroup(name string, count, high, low int64, status string) *nntp.Group {
	return &nntp.Group{ Name: name, Count: count, High: high, Low: low, Status: status }
}

func MockWillglynnMessageOverview(num int64, subject, from, msgId string, date time.Time, refs []string, bytes, lines int) nntp.MessageOverview {
    return nntp.MessageOverview{
		MessageNumber: num, Subject: subject, From: from, Date: date, MessageId: msgId, References: refs, Bytes: bytes, Lines: lines,
	}
}
func MockWillglynnArticle(headers map[string][]string, bodyContent string) *nntp.Article {
	return &nntp.Article{Header: headers, Body: strings.NewReader(bodyContent)}
}

var _ = time.Time{}
// var _ fmt.Formatter // fmt not used here currently
// var _ = textproto.Header{} // Not used here
var _ = io.EOF // For io.Reader in Article

package nntpclient

import (
	"errors"
	"io"
	// "net/textproto" // Not directly used for willglynn/nntp.Article.Header (it's map[string][]string)
	"nntp-browser-app/backend/pkg/config"
	"reflect"
	"strings"
	"testing"
	"time"
	"fmt"

	"nntp-browser-app/backend/pkg/models"
	"github.com/rspier/wgnntp" // Using rspier/wgnntp fork
)

func TestNNTPClient_GetGroups_Success(t *testing.T) {
	mockConn := &MockNNTPConnection{
		ListFunc: func(args ...string) ([]*nntp.Group, error) {
			return []*nntp.Group{
				{Name: "alt.test", Count: 10, High: 10, Low: 1, Status: "y"},
				{Name: "comp.sys", Count: 20, High: 30, Low: 11, Status: "m"},
			}, nil
		},
	}
	client := &NNTPClient{Conn: mockConn, Config: config.AppConfig{}}

	groups, err := client.GetGroups()
	if err != nil {
		t.Fatalf("GetGroups() error = %v, wantErr nil", err)
	}
	if len(groups) != 2 {
		t.Fatalf("GetGroups() got %d groups, want 2", len(groups))
	}
	if groups[0].Name != "alt.test" || groups[0].Count != 10 {
		t.Errorf("GetGroups() group[0] got %+v, want alt.test with count 10", groups[0])
	}
}

func TestNNTPClient_SelectGroup_Success(t *testing.T) {
	groupName := "alt.test"
	mockSelectedGroup := &nntp.Group{Name: groupName, Count: 10, Low: 1, High: 10, Status: "y"}
	mockConn := &MockNNTPConnection{
		GroupFunc: func(name string) (*nntp.Group, error) {
			if name != groupName {
				return nil, errors.New("wrong group selected")
			}
			return mockSelectedGroup, nil
		},
	}
	client := &NNTPClient{Conn: mockConn, Config: config.AppConfig{}}

	count, low, high, err := client.SelectGroup(groupName)
	if err != nil {
		t.Fatalf("SelectGroup() error = %v", err)
	}
	if count != 10 || low != 1 || high != 10 {
		t.Errorf("SelectGroup() got count=%d, low=%d, high=%d, want 10,1,10", count, low, high)
	}
}

func TestNNTPClient_GetArticleOverviews_Success(t *testing.T) {
	mockDate := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	mockConn := &MockNNTPConnection{
		OverviewFunc: func(begin, end int64) ([]nntp.MessageOverview, error) {
			return []nntp.MessageOverview{
				{MessageNumber: 1, MessageId: "<id1@host>", Subject: "Subject 1", From: "User1 <u1@h>", Date: mockDate, References: []string{"<ref0@host>"}},
			}, nil
		},
	}
	client := &NNTPClient{Conn: mockConn, Config: config.AppConfig{}}
	overviews, err := client.GetArticleOverviews(1, 10)
	if err != nil {
		t.Fatalf("GetArticleOverviews() error = %v", err)
	}
	if len(overviews) != 1 {
		t.Fatalf("Expected 1 overview, got %d", len(overviews))
	}
	// Check trimmed values as client method trims them
	if overviews[0].MessageId != "id1@host" || overviews[0].Subject != "Subject 1" {
		t.Errorf("Unexpected overview content: %+v", overviews[0])
	}
	if len(overviews[0].References) !=1 || overviews[0].References[0] != "ref0@host" {
		t.Errorf("Unexpected references: %v", overviews[0].References)
	}
}

func TestNNTPClient_GetArticleHeaders_Mapping(t *testing.T) {
	mockDate := time.Date(2023, 5, 10, 15, 30, 0, 0, time.UTC)
	mockNntpOverviews := []nntp.MessageOverview{
		{MessageNumber: 123, Subject: "Test Subject", From: "test@example.com", Date: mockDate, MessageId: "<test1@example.com>", References: []string{"<ref1@example.com>"}},
	}
	mockConn := &MockNNTPConnection{
		OverviewFunc: func(begin, end int64) ([]nntp.MessageOverview, error) {
			return mockNntpOverviews, nil
		},
	}
	client := &NNTPClient{Conn: mockConn, Config: config.AppConfig{}}

	messageOverviews, err := client.GetArticleHeaders(123, 123)
	if err != nil {
		t.Fatalf("GetArticleHeaders error: %v", err)
	}
	if len(messageOverviews) != 1 {
		t.Fatalf("Expected 1 message overview, got %d", len(messageOverviews))
	}
	expected := models.MessageOverview{
		ID: "test1@example.com", Number: 123, Subject: "Test Subject", From: "test@example.com", Date: mockDate,
	}
	if !reflect.DeepEqual(messageOverviews[0], expected) {
		t.Errorf("GetArticleHeaders() mismatch:\nGot:  %+v\nWant: %+v", messageOverviews[0], expected)
	}
}


func TestNNTPClient_GetArticleBody_Success(t *testing.T) {
	specifier := "article1@example.com"
	// mockArticleContent variable is not used if body is separated in mock.
	mockArticleBodyContentOnly := "This is the article body."

	mockConn := &MockNNTPConnection{} // Initialize empty, then set the needed func

	// The nntpclient.GetArticleBody method calls c.Conn.Article(specifier) which returns *nntp.Article.
	// So, we need to mock ArticleFunc.
	mockConn.ArticleFunc = func(id string) (*nntp.Article, error) {
		if id != specifier {
			return nil, errors.New("article not found by id: " + id)
		}
		hdrMap := map[string][]string{
			"Subject":    {"Test Subject"},
			"From":       {"Test User"},
			"Message-Id": {"<article1@example.com>"},
			"Date":       {"Mon, 01 Jan 2024 12:00:00 +0000"}, // Parsable by ParseNNTPDate
			"References": {"<ref1@host> <ref2@host>"}, // Mock two references
		}
		return &nntp.Article{ // This is willglynn/nntp.Article
			Header: hdrMap,
			Body:   strings.NewReader(mockArticleBodyContentOnly), // Use the specific body content
		}, nil
	}

	client := &NNTPClient{Conn: mockConn, Config: config.AppConfig{}}
	message, err := client.GetArticleBody(specifier)
	if err != nil {
		t.Fatalf("GetArticleBody() error = %v", err)
	}

	if message.Subject != "Test Subject" {
		t.Errorf("Unexpected subject: '%s'", message.Subject)
	}
	if message.ID != "article1@example.com" {
		t.Errorf("Unexpected Message-ID: '%s'", message.ID)
	}
	if message.Body != mockArticleBodyContentOnly { // Check against the body part only
		t.Errorf("Unexpected body: '%s', want '%s'", message.Body, mockArticleBodyContentOnly)
	}
	expectedRefs := []string{"ref1@host", "ref2@host"} // Expect two references
	if !reflect.DeepEqual(message.References, expectedRefs) { // Use reflect.DeepEqual for slice comparison
		t.Errorf("Unexpected references: got %v, want %v", message.References, expectedRefs)
	}
}


func TestNew_NoServerConfig(t *testing.T) {
	cfg := config.AppConfig{NNTPServer: ""}
	_, err := New(cfg)
	if err == nil {
		t.Error("New() with empty server config should return an error, got nil")
	}
}

func TestNNTPClient_Close(t *testing.T) {
	quitCalled := false
	mockConn := &MockNNTPConnection{
		QuitFunc: func() error {
			quitCalled = true
			return nil
		},
	}
	client := &NNTPClient{Conn: mockConn, Config: config.AppConfig{}}
	client.Close()
	if !quitCalled {
		t.Error("Close() did not call Quit on the connection")
	}
	if client.Conn != nil {
		 t.Error("client.Conn should be nil after Close()")
	}
}

var _ NNTPConnection = (*MockNNTPConnection)(nil)
var _ NNTPConnection = (*nntpConnAdapter)(nil)
var _ = reflect.TypeOf(nntp.Article{})
var _ = fmt.Sprintf("") // For fmt import
// var _ = textproto.Header{} // Not used here
var _ = io.EOF
var _ = time.UTC

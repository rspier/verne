package mimeparser

import (
	"io"
	"strings"
	"testing"
)

func TestSanitizeHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"plain text", "Hello world", "Hello world"},
		{"simple html", "<p>Hello</p>", "<p>Hello</p>"},
		{"html with script", "<p>Hello <script>alert('XSS')</script></p>", "<p>Hello </p>"},
		{"html with onclick", `<p onclick="alert('XSS')">Hello</p>`, "<p>Hello</p>"},
		{"html with style", `<p style="color:red">Hello</p>`, `<p>Hello</p>`}, // UGCPolicy strips style by default
		{"html with allowed pre", "<pre>code block</pre>", "<pre>code block</pre>"},
		{"html with disallowed tag", "<div>Hello <foo>Bar</foo></div>", "<div>Hello Bar</div>"},
		{"html with image", `<img src="image.jpg" alt="test">`, `<img src="image.jpg" alt="test">`}, // UGCPolicy allows safe src attributes
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized := SanitizeHTML(tt.input)
			if sanitized != tt.expected {
				t.Errorf("SanitizeHTML('%s'): got '%s', want '%s'", tt.input, sanitized, tt.expected)
			}
		})
	}
}

func TestParseArticle(t *testing.T) {
	tests := []struct {
		name                 string
		rawArticle           string
		expectedTextBody     string
		expectedHTMLBody     string // original HTML before sanitization
		expectedSanitizedHTML string
		expectedPreferredBody string
		expectedIsHTML       bool
		expectedOtherParts   bool
		expectError          bool
	}{
		{
			name: "plain text only",
			rawArticle: "Content-Type: text/plain; charset=utf-8\r\n\r\nHello, world!",
			expectedTextBody:     "Hello, world!",
			expectedPreferredBody: "Hello, world!",
			expectedIsHTML:       false,
		},
		{
			name: "html only",
			rawArticle: "Content-Type: text/html; charset=utf-8\r\n\r\n<p>Hello, <b>world</b>!</p><script>alert('xss')</script>",
			expectedHTMLBody:     "<p>Hello, <b>world</b>!</p><script>alert('xss')</script>",
			expectedSanitizedHTML: "<p>Hello, <b>world</b>!</p>",
			expectedPreferredBody: "<p>Hello, <b>world</b>!</p>",
			expectedIsHTML:       true,
		},
		{
			name: "multipart alternative - html preferred",
			rawArticle: "Content-Type: multipart/alternative; boundary=boundary123\r\n\r\n" +
				"--boundary123\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nPlain text version.\r\n" +
				"--boundary123\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>HTML <b>version</b>.</p><script>void(0)</script>\r\n" +
				"--boundary123--\r\n",
			expectedTextBody:     "Plain text version.",
			expectedHTMLBody:     "<p>HTML <b>version</b>.</p><script>void(0)</script>",
			expectedSanitizedHTML: "<p>HTML <b>version</b>.</p>",
			expectedPreferredBody: "<p>HTML <b>version</b>.</p>",
			expectedIsHTML:       true,
		},
		{
			name: "multipart alternative - plain fallback",
			rawArticle: "Content-Type: multipart/alternative; boundary=boundary123\r\n\r\n" +
				"--boundary123\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nPlain text version.\r\n" +
				"--boundary123--\r\n",
			expectedTextBody:     "Plain text version.",
			expectedPreferredBody: "Plain text version.",
			expectedIsHTML:       false,
		},
		{
			name: "multipart mixed with attachment",
			rawArticle: "Content-Type: multipart/mixed; boundary=boundary123\r\n\r\n" +
				"--boundary123\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nHello there.\r\n" +
				"--boundary123\r\nContent-Type: image/jpeg\r\nContent-Disposition: attachment; filename=\"image.jpg\"\r\n\r\n[binary data]\r\n" +
				"--boundary123--\r\n",
			expectedTextBody:     "Hello there.",
			expectedPreferredBody: "Hello there.",
			expectedIsHTML:       false,
			expectedOtherParts:   true,
		},
		{
			name: "quoted printable encoding",
			rawArticle: "Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nHello,=\r\n world!=3DThis is a test.",
			expectedTextBody:     "Hello, world!=This is a test.",
			expectedPreferredBody: "Hello, world!=This is a test.",
			expectedIsHTML:       false,
		},
		{
			name: "no content type - fallback to plain text",
			rawArticle: "\r\nThis is some body content without a Content-Type header.",
			expectedTextBody: "This is some body content without a Content-Type header.",
			expectedPreferredBody: "This is some body content without a Content-Type header.",
			expectedIsHTML: false,
		},
		{
			name: "unparseable content type - fallback to plain text",
			rawArticle: "Content-Type: application\r\n\r\nBinary content that we will try to read as text.",
			// This will log a warning about unparseable type, then try to read body
			expectedTextBody: "Binary content that we will try to read as text.",
			expectedPreferredBody: "Binary content that we will try to read as text.",
			expectedIsHTML: false,
			expectedOtherParts: true, // Because main content type wasn't text/* or multipart/* initially
		},
		{
			name: "empty body",
			rawArticle: "Content-Type: text/plain\r\n\r\n",
			expectedTextBody: "",
			expectedPreferredBody: "",
			expectedIsHTML: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.rawArticle)
			result, err := ParseArticle(reader)

			if (err != nil) != tt.expectError {
				t.Fatalf("ParseArticle() error = %v, expectError %v", err, tt.expectError)
			}
			if tt.expectError {
				return
			}
			if result == nil {
				t.Fatalf("ParseArticle() result is nil, expected non-nil")
			}

			if result.TextBody != tt.expectedTextBody {
				t.Errorf("ParseArticle() TextBody = %q, want %q", result.TextBody, tt.expectedTextBody)
			}
			if result.HTMLBody != tt.expectedHTMLBody {
				t.Errorf("ParseArticle() HTMLBody = %q, want %q", result.HTMLBody, tt.expectedHTMLBody)
			}
			if result.SanitizedHTML != tt.expectedSanitizedHTML {
				t.Errorf("ParseArticle() SanitizedHTML = %q, want %q", result.SanitizedHTML, tt.expectedSanitizedHTML)
			}
			if result.PreferredBody != tt.expectedPreferredBody {
				t.Errorf("ParseArticle() PreferredBody = %q, want %q", result.PreferredBody, tt.expectedPreferredBody)
			}
			if result.IsHTML != tt.expectedIsHTML {
				t.Errorf("ParseArticle() IsHTML = %v, want %v", result.IsHTML, tt.expectedIsHTML)
			}
			if result.OtherPartsExist != tt.expectedOtherParts {
				t.Errorf("ParseArticle() OtherPartsExist = %v, want %v", result.OtherPartsExist, tt.expectedOtherParts)
			}
		})
	}
}

// Helper to create a reader from string for tests that might need io.ReadCloser
type stringReadCloser struct {
	io.Reader
}
func (s *stringReadCloser) Close() error { return nil }

func NewStringReadCloser(s string) io.ReadCloser {
	return &stringReadCloser{strings.NewReader(s)}
}

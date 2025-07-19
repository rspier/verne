package main

import "testing"

func TestSanitize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "<p>Hello, world!</p>",
			expected: "<p>Hello, world!</p>",
		},
		{
			input:    "<p>Hello, <b>world!</b></p>",
			expected: "<p>Hello, <b>world!</b></p>",
		},
		{
			input:    `<p>Hello, <script>alert("world!")</script></p>`,
			expected: "<p>Hello, </p>",
		},
		{
			input:    `<p>Hello, <a href="http://example.com">world!</a></p>`,
			expected: `<p>Hello, <a href="http://example.com" rel="nofollow">world!</a></p>`,
		},
	}

	for _, test := range tests {
		if Sanitize(test.input) != test.expected {
			t.Errorf("expected %q, got %q", test.expected, Sanitize(test.input))
		}
	}
}

package models

import (
	"reflect"
	"testing"
)

func TestParseReferencesString(t *testing.T) {
	tests := []struct {
		name     string
		rawRefs  string
		expected []string
	}{
		{
			name:     "empty string",
			rawRefs:  "",
			expected: nil,
		},
		{
			name:     "whitespace string",
			rawRefs:  "   ",
			expected: nil,
		},
		{
			name:     "single reference",
			rawRefs:  "<id1@example.com>",
			expected: []string{"id1@example.com"},
		},
		{
			name:     "multiple references",
			rawRefs:  "<id1@example.com> <id2@host.domain> <id3@another.net>",
			expected: []string{"id1@example.com", "id2@host.domain", "id3@another.net"},
		},
		{
			name:     "references with extra whitespace",
			rawRefs:  "  <id1@example.com>   <id2@host.domain>  ",
			expected: []string{"id1@example.com", "id2@host.domain"},
		},
		{
			name:     "references with no angle brackets (will not be matched by new regex)",
			rawRefs:  "id1@example.com id2@host.domain",
			expected: nil,
		},
		{
			name:     "mixed angle brackets and bare (only bracketed will be matched)",
			rawRefs:  "<id1@example.com> id2@host.domain <id3@another.net>",
			expected: []string{"id1@example.com", "id3@another.net"},
		},
		{
			name:     "complex string with other text (should only extract valid bracketed IDs)",
			rawRefs:  "References: <id1@foo.com> (Some comment) <id2@bar.net>\n\t<id3@baz.org> other text id4@bare.com",
			expected: []string{"id1@foo.com", "id2@bar.net", "id3@baz.org"},
		},
		{
			name:     "no valid message ids",
			rawRefs:  "Not a message ID string or bare@id",
			expected: nil,
		},
		{
			name:     "empty angle brackets",
			rawRefs:  "<id1@example.com> <> <id2@example.com>",
			expected: []string{"id1@example.com", "id2@example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseReferencesString(tt.rawRefs)
			if !reflect.DeepEqual(got, tt.expected) {
				// Handle nil vs empty slice for comparison robustness if necessary
				if len(got) == 0 && len(tt.expected) == 0 && (got == nil) != (tt.expected == nil) {
					// This means one is nil and other is empty slice, treat as equal for this test's purpose
				} else {
					t.Errorf("ParseReferencesString(%q) = %v, want %v", tt.rawRefs, got, tt.expected)
				}
			}
		})
	}
}

package utils

import "testing"

func TestObfuscateEmailInFromHeader(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		expected string
	}{
		{
			name:     "empty input",
			from:     "",
			expected: "",
		},
		{
			name:     "name and email",
			from:     "Alice Wonderland <alice.wonderland@example.com>",
			expected: "Alice Wonderland",
		},
		{
			name:     "name and email with quotes",
			from:     `"Alice Wonderland" <alice.wonderland@example.com>`,
			expected: "Alice Wonderland",
		},
		{
			name:     "email only - long local part",
			from:     "bob.the.builder@example.com",
			expected: "bob...@example.com",
		},
		{
			name:     "email only - short local part (3 chars)",
			from:     "rob@example.com",
			expected: "rob@example.com",
		},
		{
			name:     "email only - very short local part (2 chars)",
			from:     "ed@example.com",
			expected: "ed@example.com",
		},
		{
			name:     "email only - in angle brackets",
			from:     "<carol.danvers@example.com>",
			expected: "car...@example.com",
		},
		{
			name:     "malformed - no domain",
			from:     "justname",
			expected: "justname", // Returns original on parse error
		},
		{
			name:     "malformed - display name no email",
			from:     "Display Name Only",
			expected: "Display Name Only", // Returns original on parse error
		},
		{
			name:     "malformed - multiple @",
			from:     "test@test@example.com",
			expected: "tes...@test@example.com", // Current behavior obfuscates the first part before first @
		},
		{
			name:     "unicode name",
			from:     "““““Üşer” Nâme””” <user@example.com>",
			expected: "““““Üşer” Nâme”””",
		},
		{
			name:     "empty name, email present (unusual format, ParseAddress fails)",
			from:     "<> <user@example.com>",
			expected: "<> <user@example.com>",    // mail.ParseAddress fails, so original is returned by current logic.
		},
		{
			name:     "bare email, no name, no brackets",
			from:     "plainaddress@example.com",
			expected: "pla...@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ObfuscateEmailInFromHeader(tt.from); got != tt.expected {
				t.Errorf("ObfuscateEmailInFromHeader(%q) = %q, want %q", tt.from, got, tt.expected)
			}
		})
	}
}

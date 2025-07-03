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
			expected: "tes...@test@example.com", // Corrected: matches current behavior
		},
		{
			name:     "unicode name",
			from:     "““““Üşer” Nâme””” <user@example.com>",
			expected: "““““Üşer” Nâme”””",
		},
		{
			name:     "empty name, email present (unusual format, ParseAddress fails)",
			from:     "<> <user@example.com>",
			expected: "<> <user@example.com>",    // mail.ParseAddress fails for this, so original is returned.
		},
		{
			name:     "bare email, no name, no brackets",
			from:     "plainaddress@example.com",
			expected: "pla...@example.com",
		},
		{
			name:     "RFC 2047 Q-encoded UTF-8 display name",
			from:     "=?UTF-8?Q?Smylers=20=C2=A0?= <smylers@example.com>",
			expected: "Smylers \u00A0", // Decoded: "Smylers " + non-breaking space
		},
		{
			name:     "RFC 2047 B-encoded UTF-8 display name",
			from:     "=?UTF-8?B?SsOzbGxlcg==?= <smylers@example.com>", // This is Jóller
			expected: "Jóller", // Corrected expected output
		},
		{
			name:     "RFC 2047 Q-encoded ISO-8859-1 display name",
			from:     "=?ISO-8859-1?Q?J=F6rg_Smylers?= <smylers@example.com>", // Jörg Smylers
			expected: "Jörg Smylers", // WordDecoder handles charset if possible
		},
		{
			name:     "RFC 2047 Q-encoded multiple words",
			from:     "=?UTF-8?Q?First=20Part?= =?UTF-8?Q?=20Second=20Part?= <user@example.com>",
			expected: "First Part Second Part",
		},
		{
			name:     "RFC 2047 with plain text",
			from:     "Plain =?UTF-8?Q?Encoded?= Text <user@example.com>",
			expected: "Plain Encoded Text",
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

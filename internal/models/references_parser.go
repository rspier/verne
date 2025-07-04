package models

import (
	"regexp"
	"strings"
)

// referenceMsgIDRegex helps extract individual message IDs from a References header string.
// It looks for content within <...> that resembles an email address / message ID.
var referenceMsgIDRegex = regexp.MustCompile(`<([^<>@\s]+@[^<>@\s]+)>`)

// ParseReferencesString takes a raw "References" or "In-Reply-To" header string
// and splits it into a slice of individual message IDs.
// It primarily extracts Message-IDs enclosed in <angle brackets>.
func ParseReferencesString(rawRefs string) []string {
	if strings.TrimSpace(rawRefs) == "" {
		return nil
	}

	var refs []string
	// FindAllStringSubmatch will return matches where:
	// match[0] is the full substring (e.g., "<id@domain.com>")
	// match[1] is the content of the first capturing group (e.g., "id@domain.com")
	matches := referenceMsgIDRegex.FindAllStringSubmatch(rawRefs, -1)
	for _, match := range matches {
		if len(match) > 1 && match[1] != "" {
			refs = append(refs, strings.TrimSpace(match[1]))
		}
	}

	if len(refs) == 0 {
		return nil // Return nil if no valid bracketed IDs found
	}
	return refs
}

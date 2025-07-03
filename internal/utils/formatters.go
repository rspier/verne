package utils

import (
	"fmt"
	"net/mail"
	"strings"
)

// ObfuscateEmailInFromHeader processes a "From" header string.
// If a display name is present, it returns the display name.
// Otherwise, it obfuscates the local part of the email address if it's longer than 3 characters.
// e.g., "User Name <user.long.name@example.com>" -> "User Name"
// e.g., "user.long.name@example.com" -> "use...@example.com"
// e.g., "usr@example.com" -> "usr@example.com"
// e.g., "<user.long.name@example.com>" -> "use...@example.com"
func ObfuscateEmailInFromHeader(fromHeader string) string {
	if fromHeader == "" {
		return ""
	}

	addr, err := mail.ParseAddress(fromHeader)
	if err != nil {
		// If parsing fails, it might be a simple email without a name,
		// or a malformed header. Try to handle simple email case.
		// If it contains "@" and no typical display name chars like "<", ">", "\"",
		// treat it as a bare email.
		if strings.Contains(fromHeader, "@") &&
			!strings.ContainsAny(fromHeader, "<>\"") {
			return obfuscateEmailPart(fromHeader)
		}
		// Otherwise, return the original string as is, or a generic placeholder.
		// For now, return original on parse error if not clearly a bare email.
		return fromHeader
	}

	// If a display name exists and it's not just "<>", return it.
	// Otherwise, proceed to obfuscate the email address part.
	if addr.Name != "" && strings.TrimSpace(addr.Name) != "<>" && addr.Name != `""` {
		return addr.Name
	}

	// No valid display name, obfuscate the address part
	return obfuscateEmailPart(addr.Address)
}

// obfuscateEmailPart takes an email address string (e.g., user@example.com)
// and obfuscates the local part if it's longer than 3 characters.
func obfuscateEmailPart(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return email // Not a valid email format, return as is
	}
	localPart := parts[0]
	domainPart := parts[1]

	if len(localPart) > 3 {
		return fmt.Sprintf("%s...@%s", localPart[:3], domainPart)
	}
	return email // Return original if local part is 3 chars or less
}

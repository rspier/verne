package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// GroupConfig holds configuration for a single newsgroup.
type GroupConfig struct {
	Name      string
	Path      string // Filesystem path to archive
	Num       int    // Group ID number
	Mail      string // Email address for posting
	Moderated bool
	Hidden    bool
	Recommend bool
	Followup  string
	Desc      string // Description
}

// Config holds the overall application configuration.
type Config struct {
	ServerName     string
	DSN            string // Data Source Name for database
	DBUser         string
	DBPass         string
	Timeout        int // Seconds
	Disallow       map[string]struct{}
	MailInject     []string
	Groups         map[string]*GroupConfig // Map group name to its config
	OverviewFields []string
}

// LoadConfig reads the configuration file and populates the Config struct.
// The config file is expected to be in a simple key = value format.
// Group configurations are defined in sections like [group alt.test].
func LoadConfig(filePath string) (*Config, error) {
	cfg := &Config{
		Groups:         make(map[string]*GroupConfig),
		Disallow:       make(map[string]struct{}),
		MailInject:     []string{"/var/qmail/bin/qmail-inject", "-a"}, // Default from Perl
		OverviewFields: []string{"Subject", "From", "Date", "Message-ID", "References", "Bytes", "Lines"}, // Default
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file %s: %w", filePath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var currentGroup *GroupConfig
	lineNumber := 0

	// stripEOLComment is a local helper for LoadConfig
	// It removes an end-of-line comment starting with '#'.
	// It does NOT handle ';' as an EOL comment marker to avoid issues with DSNs for now.
	stripEOLComment := func(s string) string {
		if commentIdx := strings.Index(s, "#"); commentIdx != -1 {
			// This simple version assumes '#' always starts an EOL comment if not at the start of the original line.
			// Values containing '#' not intended as comments would be truncated.
			return strings.TrimSpace(s[:commentIdx])
		}
		return s // Return s as is; TrimSpace will be applied by the caller.
	}

	for scanner.Scan() {
		lineNumber++
		originalText := scanner.Text()
		trimmedLine := strings.TrimSpace(originalText)

		// Skip full-line comments (starting with # or ;) or empty lines
		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "#") || strings.HasPrefix(trimmedLine, ";") {
			continue
		}

		// Now that we know it's not a full comment line, strip any EOL comment for actual parsing
		lineForParsing := stripEOLComment(trimmedLine) // Apply to the whole effective line content

		// If the line became empty after stripping EOL comment (e.g. "  # comment only"), skip
		if lineForParsing == "" {
			continue
		}


		// Check for group section first
		if strings.HasPrefix(lineForParsing, "[group ") && strings.HasSuffix(lineForParsing, "]") {
			groupNamePart := strings.TrimSuffix(strings.TrimPrefix(lineForParsing, "[group "), "]")
			groupName := strings.TrimSpace(groupNamePart) // Group name itself shouldn't have internal comments
			if groupName == "" {
				return nil, fmt.Errorf("line %d: empty group name from '%s'", lineNumber, originalText)
			}
			currentGroup = &GroupConfig{Name: groupName}
			cfg.Groups[groupName] = currentGroup
			continue
		}

		// Try to parse as key = value
		parts := strings.SplitN(lineForParsing, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("line %d: invalid format in '%s', expected key = value", lineNumber, originalText)
		}

		key := strings.TrimSpace(parts[0])
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key from '%s'", lineNumber, originalText)
		}

		value := strings.TrimSpace(parts[1]) // Value is already comment-stripped as lineForParsing was used

		if currentGroup != nil { // Inside a [group ...] section
			switch strings.ToLower(key) {
			case "path":
				currentGroup.Path = value
			case "num":
				num, err := strconv.Atoi(value)
				if err != nil {
					return nil, fmt.Errorf("line %d: invalid group num '%s' for key '%s': %w", lineNumber, value, key, err)
				}
				currentGroup.Num = num
			case "mail":
				currentGroup.Mail = value
			case "moderated":
				currentGroup.Moderated = strings.ToLower(value) == "true" || value == "1" || strings.ToLower(value) == "yes"
			case "hidden":
				currentGroup.Hidden = strings.ToLower(value) == "true" || value == "1" || strings.ToLower(value) == "yes"
			case "recommend":
				currentGroup.Recommend = strings.ToLower(value) == "true" || value == "1" || strings.ToLower(value) == "yes"
			case "followup":
				currentGroup.Followup = value
			case "desc":
				currentGroup.Desc = value
			default:
				// Log unknown group key?
				// log.Printf("Warning: line %d: unknown key '%s' in group section '%s'", lineNumber, key, currentGroup.Name)
			}
		} else { // Global configuration
			switch strings.ToLower(key) {
			case "servername":
				cfg.ServerName = value
			case "dsn":
				cfg.DSN = value
			case "dbuser":
				cfg.DBUser = value
			case "dbpass":
				cfg.DBPass = value
			case "timeout":
				timeout, err := strconv.Atoi(value)
				if err != nil {
					return nil, fmt.Errorf("line %d: invalid timeout value '%s' for key '%s': %w", lineNumber, value, key, err)
				}
				cfg.Timeout = timeout
			case "disallow":
				commands := strings.Split(value, ",")
				for _, cmd := range commands {
					trimmedCmd := strings.TrimSpace(cmd)
					if trimmedCmd != "" {
						cfg.Disallow[strings.ToLower(trimmedCmd)] = struct{}{}
					}
				}
			case "mailinject":
				cfg.MailInject = strings.Fields(value) // Split by space
			default:
				// Log unknown global key?
				// log.Printf("Warning: line %d: unknown global key '%s'", lineNumber, key)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	// Set defaults if not provided
	if cfg.DSN == "" {
		cfg.DSN = "DBI:mysql:database=colobus;host=localhost"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 3600
	}

	return cfg, nil
}

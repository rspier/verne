package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadConfig_ValidSimple(t *testing.T) {
	content := `
servername = news.example.com
dsn = DBI:mysql:database=colobus_test;host=localhost
dbuser = testuser
dbpass = testpass
timeout = 600
disallow = xpat, newnews
mailinject = /usr/sbin/sendmail -t -oi

[group example.test]
path = /var/spool/news/example/test
num = 1
mail = test@example.com
moderated = true
desc = Test group description
hidden = false
recommend = true
followup = example.followup

[group example.another]
path = /tmp/another
# num will be auto-assigned if not in DB during sync
mail = another@example.com
moderated = false
desc = Another group
hidden = true
recommend = false
# followup is empty
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test_config.ini")
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.ServerName != "news.example.com" {
		t.Errorf("Expected ServerName 'news.example.com', got '%s'", cfg.ServerName)
	}
	if cfg.DSN != "DBI:mysql:database=colobus_test;host=localhost" {
		t.Errorf("Expected DSN 'DBI:mysql:database=colobus_test;host=localhost', got '%s'", cfg.DSN)
	}
	if cfg.DBUser != "testuser" {
		t.Errorf("Expected DBUser 'testuser', got '%s'", cfg.DBUser)
	}
	if cfg.DBPass != "testpass" {
		t.Errorf("Expected DBPass 'testpass', got '%s'", cfg.DBPass)
	}
	if cfg.Timeout != 600 {
		t.Errorf("Expected Timeout 600, got %d", cfg.Timeout)
	}

	expectedDisallow := map[string]struct{}{"xpat": {}, "newnews": {}}
	if !reflect.DeepEqual(cfg.Disallow, expectedDisallow) {
		t.Errorf("Expected Disallow %v, got %v", expectedDisallow, cfg.Disallow)
	}

	expectedMailInject := []string{"/usr/sbin/sendmail", "-t", "-oi"}
	if !reflect.DeepEqual(cfg.MailInject, expectedMailInject) {
		t.Errorf("Expected MailInject %v, got %v", expectedMailInject, cfg.MailInject)
	}

	// Check group example.test
	group1, ok := cfg.Groups["example.test"]
	if !ok {
		t.Fatalf("Expected group 'example.test' not found")
	}
	if group1.Path != "/var/spool/news/example/test" {
		t.Errorf("Group 'example.test': Expected Path '/var/spool/news/example/test', got '%s'", group1.Path)
	}
	if group1.Num != 1 {
		t.Errorf("Group 'example.test': Expected Num 1, got %d", group1.Num)
	}
	if group1.Mail != "test@example.com" {
		t.Errorf("Group 'example.test': Expected Mail 'test@example.com', got '%s'", group1.Mail)
	}
	if !group1.Moderated {
		t.Errorf("Group 'example.test': Expected Moderated true, got false")
	}
	if group1.Desc != "Test group description" {
		t.Errorf("Group 'example.test': Expected Desc 'Test group description', got '%s'", group1.Desc)
	}
	if group1.Hidden {
		t.Errorf("Group 'example.test': Expected Hidden false, got true")
	}
	if !group1.Recommend {
		t.Errorf("Group 'example.test': Expected Recommend true, got false")
	}
	if group1.Followup != "example.followup" {
		t.Errorf("Group 'example.test': Expected Followup 'example.followup', got '%s'", group1.Followup)
	}

	// Check group example.another
	group2, ok := cfg.Groups["example.another"]
	if !ok {
		t.Fatalf("Expected group 'example.another' not found")
	}
	if group2.Path != "/tmp/another" {
		t.Errorf("Group 'example.another': Expected Path '/tmp/another', got '%s'", group2.Path)
	}
	if group2.Num != 0 { // Num is not set in this config, should be 0 before DB sync
		t.Errorf("Group 'example.another': Expected Num 0 (before DB sync), got %d", group2.Num)
	}
	if group2.Moderated {
		t.Errorf("Group 'example.another': Expected Moderated false, got true")
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	content := `
servername = minimal.example.com
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test_config_defaults.ini")
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.ServerName != "minimal.example.com" {
		t.Errorf("Expected ServerName 'minimal.example.com', got '%s'", cfg.ServerName)
	}
	// Check default DSN (from Perl script)
	if cfg.DSN != "DBI:mysql:database=colobus;host=localhost" {
		t.Errorf("Expected default DSN, got '%s'", cfg.DSN)
	}
	// Check default Timeout (Perl script doesn't specify one in config loading, we added one)
	if cfg.Timeout != 3600 {
		t.Errorf("Expected default Timeout 3600, got %d", cfg.Timeout)
	}
	// Check default MailInject
	expectedMailInject := []string{"/var/qmail/bin/qmail-inject", "-a"}
	if !reflect.DeepEqual(cfg.MailInject, expectedMailInject) {
		t.Errorf("Expected default MailInject %v, got %v", expectedMailInject, cfg.MailInject)
	}
	// Check default OverviewFields
	expectedOverview := []string{"Subject", "From", "Date", "Message-ID", "References", "Bytes", "Lines"}
	if !reflect.DeepEqual(cfg.OverviewFields, expectedOverview) {
		t.Errorf("Expected default OverviewFields %v, got %v", expectedOverview, cfg.OverviewFields)
	}
}

func TestLoadConfig_InvalidLine(t *testing.T) {
	content := `
servername = news.example.com
invalidline
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test_config_invalid.ini")
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	_, err := LoadConfig(configFile)
	if err == nil {
		t.Fatalf("Expected LoadConfig to fail on invalid line, but it succeeded")
	}
	if !strings.Contains(err.Error(), "invalid format") {
		t.Errorf("Expected error message to contain 'invalid format', got: %v", err)
	}
}

func TestLoadConfig_InvalidGroupNum(t *testing.T) {
	content := `
[group example.test]
num = notanumber
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test_config_badnum.ini")
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	_, err := LoadConfig(configFile)
	if err == nil {
		t.Fatalf("Expected LoadConfig to fail on invalid group num, but it succeeded")
	}
	if !strings.Contains(err.Error(), "invalid group num") {
		t.Errorf("Expected error message to contain 'invalid group num', got: %v", err)
	}
}

func TestLoadConfig_EmptyGroupName(t *testing.T) {
	content := `
[group ]
path = /tmp/foo
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test_config_emptygroup.ini")
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	_, err := LoadConfig(configFile)
	if err == nil {
		t.Fatalf("Expected LoadConfig to fail on empty group name, but it succeeded")
	}
	if !strings.Contains(err.Error(), "empty group name") {
		t.Errorf("Expected error message to contain 'empty group name', got: %v", err)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("nonexistent_config_file.ini")
	if err == nil {
		t.Fatalf("Expected LoadConfig to fail for non-existent file, but it succeeded")
	}
	if !os.IsNotExist(err) && !strings.Contains(err.Error(), "no such file or directory") { // os.IsNotExist might be wrapped
		t.Errorf("Expected a file not found error, got: %v", err)
	}
}

func TestLoadConfig_BooleanParsing(t *testing.T) {
	testCases := []struct {
		name          string
		moderatedVal  string
		expectedBool  bool
	}{
		{"true string", "true", true},
		{"TRUE string", "TRUE", true},
		{"yes string", "yes", true},
		{"YES string", "YES", true},
		{"1 string", "1", true},
		{"false string", "false", false},
		{"no string", "no", false},
		{"0 string", "0", false},
		{"other string", "anythingelse", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			content := fmt.Sprintf(`
[group testbool]
path = /tmp
moderated = %s
`, tc.moderatedVal)
			tmpDir := t.TempDir()
			configFile := filepath.Join(tmpDir, "test_config_bool.ini")
			if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to write test config file: %v", err)
			}
			cfg, err := LoadConfig(configFile)
			if err != nil {
				t.Fatalf("LoadConfig failed: %v", err)
			}
			group, ok := cfg.Groups["testbool"]
			if !ok {
				t.Fatal("Group 'testbool' not loaded")
			}
			if group.Moderated != tc.expectedBool {
				t.Errorf("For Moderated value '%s', expected %t, got %t", tc.moderatedVal, tc.expectedBool, group.Moderated)
			}
		})
	}
}

func TestLoadConfig_CommentsAndEmptyLines(t *testing.T) {
	content := `
# This is a comment
servername = test.server

; Another comment style
dsn = mydsn

[group test.comments] # Comment on group line
path = /path/test # Comment on path line
# mail = foo@bar.com
desc = A group with comments
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test_config_comments.ini")
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.ServerName != "test.server" {
		t.Errorf("Expected ServerName 'test.server', got '%s'", cfg.ServerName)
	}
	if cfg.DSN != "mydsn" {
		t.Errorf("Expected DSN 'mydsn', got '%s'", cfg.DSN)
	}
	group, ok := cfg.Groups["test.comments"]
	if !ok {
		t.Fatal("Group 'test.comments' not loaded")
	}
	if group.Path != "/path/test" {
		t.Errorf("Group Path incorrect, expected '/path/test', got '%s'", group.Path)
	}
	if group.Desc != "A group with comments" {
		t.Errorf("Group Desc incorrect, expected 'A group with comments', got '%s'", group.Desc)
	}
	if group.Mail != "" { // Mail was commented out
		t.Errorf("Expected empty Mail for commented out line, got '%s'", group.Mail)
	}
}

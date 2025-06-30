package config

// AppConfig holds the application configuration.
type AppConfig struct {
	NNTPServer string // Address (host:port) of the NNTP server
	ServerPort int    // Port for the backend HTTP server to listen on
	// Add other configurations like username, password if needed for NNTP
}

// LoadConfig would typically load configuration from a file or environment variables.
// For now, it's simplified in main.go.
// func LoadConfig() (*AppConfig, error) {
//	 // Implementation would go here
//	 return &AppConfig{}, nil
// }

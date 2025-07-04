package nntpclient

// NNTPClientInterface defines the operations an NNTP client must support.
type NNTPClientInterface interface {
	FetchRawArticle(groupName string, articleNum uint32) ([]byte, error)
	Close() error
	// Add other methods like Connect() or IsConnected() if needed for more granular control by server
}

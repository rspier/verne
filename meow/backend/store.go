package main

import (
	"database/sql"
)

type Store struct {
	db *sql.DB
}

type Message struct {
	ID        int64  `json:"id"`
	ChannelID int64  `json:"channel_id"`
	UserID    int64  `json:"user_id"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// User methods
func (s *Store) CreateUser(googleID, name, avatarURL string) (int64, error) {
	result, err := s.db.Exec("INSERT INTO users (google_id, name, avatar_url) VALUES (?, ?, ?)", googleID, name, avatarURL)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) FindOrCreateUser(userInfo *UserInfo) (int64, error) {
	var userID int64
	err := s.db.QueryRow("SELECT id FROM users WHERE google_id = ?", userInfo.ID).Scan(&userID)
	if err == sql.ErrNoRows {
		return s.CreateUser(userInfo.ID, userInfo.Name, userInfo.Picture)
	}
	return userID, err
}

func (s *Store) GetUserByID(id int64) (*UserInfo, error) {
	var user UserInfo
	err := s.db.QueryRow("SELECT google_id, name, avatar_url FROM users WHERE id = ?", id).Scan(&user.ID, &user.Name, &user.Picture)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// Channel methods
func (s *Store) CreateChannel(name string) (int64, error) {
	result, err := s.db.Exec("INSERT INTO channels (name) VALUES (?)", name)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// Message methods
func (s *Store) CreateMessage(channelID, userID int64, content string) (int64, error) {
	result, err := s.db.Exec("INSERT INTO messages (channel_id, user_id, content) VALUES (?, ?, ?)", channelID, userID, content)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) GetMessagesByChannelID(channelID int64) ([]*Message, error) {
	rows, err := s.db.Query("SELECT id, channel_id, user_id, content, timestamp FROM messages WHERE channel_id = ?", channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*Message
	for rows.Next() {
		var msg Message
		err := rows.Scan(&msg.ID, &msg.ChannelID, &msg.UserID, &msg.Content, &msg.Timestamp)
		if err != nil {
			return nil, err
		}
		messages = append(messages, &msg)
	}
	return messages, nil
}

// Direct Message methods
func (s *Store) CreateDirectMessage(senderID, receiverID int64, content string) (int64, error) {
	result, err := s.db.Exec("INSERT INTO direct_messages (sender_id, receiver_id, content) VALUES (?, ?, ?)", senderID, receiverID, content)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

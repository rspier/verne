import React, { useState, useEffect } from 'react';
import { Link, useParams } from 'react-router-dom';
import './MessageList.css';

const MessageList = () => {
  const { groupName } = useParams();
  const [messages, setMessages] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  // TODO: Add state for pagination (page, hasMore) and filters (month)

  useEffect(() => {
    setLoading(true);
    setError(null);
    // Fetch from backend API
    // Note: groupName comes from useParams and should be safe for URL path.
    // If groupName could contain special characters that need encoding for a URL path segment,
    // ensure it's encoded if not already handled by react-router. Typically, useParams provides decoded values.
    fetch(`http://localhost:8080/api/groups/${groupName}/messages`) // Add query params for pagination/filters later
      .then(response => {
        if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
        return response.json();
      })
      .then(data => {
        // The backend's mock message IDs are like "<msg1@example.com>"
        // For use in URLs, these should be URL encoded. The MessageList component already does this
        // when creating links: `/groups/${groupName}/messages/${encodeURIComponent(msg.id)}`
        // So, the `id` field from the backend can be used directly here.
        // If the backend already URL-encodes them, ensure consistency.
        // The current mock backend sends them as plain strings.
        setMessages(data);
        setLoading(false);
      })
      .catch(err => {
        setError(err.message);
        setLoading(false);
      });
  }, [groupName]); // Reload if groupName changes

  if (loading) return <p>Loading messages for {groupName}...</p>;
  if (error) return <p>Error loading messages: {error}</p>;

  return (
    <div className="message-list-container">
      <h2>Messages in {groupName}</h2>
      <Link to="/" className="back-link">&larr; Back to Group List</Link>
      {/* TODO: Add month filter and jump controls here */}
      {messages.length === 0 ? (
        <p>No messages found in this group.</p>
      ) : (
        <ul className="message-list">
          {messages.map(msg => (
            <li key={msg.id} className="message-list-item">
              <Link to={`/groups/${groupName}/messages/${encodeURIComponent(msg.id)}`}>
                <div className="message-header">
                  <span className="message-subject">{msg.subject}</span>
                  <span className="message-from">by: {msg.from}</span>
                </div>
                <div className="message-meta">
                  <span className="message-date">{new Date(msg.date).toLocaleString()}</span>
                  <span className="message-thread-count">({msg.messageCountInThread} in thread)</span>
                </div>
              </Link>
            </li>
          ))}
        </ul>
      )}
      {/* TODO: Add infinite scroll trigger here */}
    </div>
  );
};

export default MessageList;

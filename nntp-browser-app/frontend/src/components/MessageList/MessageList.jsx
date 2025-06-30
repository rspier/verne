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
    // Mock data for now
    const mockMessages = [
      { id: "msg1@example.com", group: groupName, number: 1, subject: "First message in " + groupName, from: "User1 <user1@example.com>", date: new Date(Date.now() - 86400000).toISOString(), messageCountInThread: 1 },
      { id: "msg2@example.com", group: groupName, number: 2, subject: "Another interesting topic", from: "User2 <user2@example.com>", date: new Date(Date.now() - 172800000).toISOString(), messageCountInThread: 3 },
      { id: "msg3@example.com", group: groupName, number: 3, subject: "Re: Another interesting topic", from: "User1 <user1@example.com>", date: new Date(Date.now() - 170000000).toISOString(), messageCountInThread: 3 },
    ];
    setMessages(mockMessages);
    setLoading(false);

    // Actual API call will be like:
    // fetch(`/api/groups/${groupName}/messages`) // Add query params for pagination/filters
    //   .then(response => {
    //     if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
    //     return response.json();
    //   })
    //   .then(data => {
    //     setMessages(data);
    //     setLoading(false);
    //   })
    //   .catch(err => {
    //     setError(err.message);
    //     setLoading(false);
    //   });
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

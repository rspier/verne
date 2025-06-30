import React, { useState, useEffect } from 'react';
import { Link, useParams } from 'react-router-dom';
import './MessageView.css';

const MessageView = () => {
  const { groupName, messageId: encodedMessageId } = useParams();
  const [message, setMessage] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    setLoading(true);
    setError(null);
    const messageId = decodeURIComponent(encodedMessageId);

    // Mock data for now
    const mockMessage = {
      id: messageId,
      group: groupName,
      number: 123, // Example article number
      subject: "Subject of: " + messageId,
      from: "Sender <sender@example.com>",
      date: new Date().toISOString(),
      references: ["<ref1@example.com>", "<ref2@example.com>"],
      body: `This is the body of message ${messageId} in group ${groupName}.\n\nIt might contain multiple paragraphs and interesting information.\n\nNewsgroups are a classic way to discuss topics online.`,
      // Mocked thread context
      parent: "parent-id@example.com", // ID of parent message
      children: [ // List of child message objects (or just their IDs/subjects for links)
        { id: "child1@example.com", subject: "Re: Subject of: " + messageId, from: "Child Sender 1" },
        { id: "child2@example.com", subject: "Re: Subject of: " + messageId, from: "Child Sender 2" },
      ],
      nextMessage: "next-in-thread@example.com", // ID of next message in this thread
      prevMessage: "prev-in-thread@example.com", // ID of previous message in this thread
    };
    setMessage(mockMessage);
    setLoading(false);

    // Actual API call will be like:
    // fetch(`/api/groups/${groupName}/messages/${encodedMessageId}`)
    //   .then(response => {
    //     if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
    //     return response.json();
    //   })
    //   .then(data => {
    //     setMessage(data);
    //     setLoading(false);
    //   })
    //   .catch(err => {
    //     setError(err.message);
    //     setLoading(false);
    //   });
  }, [groupName, encodedMessageId]);

  if (loading) return <p>Loading message {decodeURIComponent(encodedMessageId)}...</p>;
  if (error) return <p>Error loading message: {error}</p>;
  if (!message) return <p>Message not found.</p>;

  // Basic function to render thread links - can be expanded
  const renderThreadLink = (msgId, label, prefix = "") => {
    if (!msgId) return null;
    return (
      <Link to={`/groups/${groupName}/messages/${encodeURIComponent(msgId)}`}>
        {prefix}{label || msgId}
      </Link>
    );
  };


  return (
    <div className="message-view-container">
      <div className="message-navigation">
        <Link to={`/groups/${groupName}/messages`} className="back-link">&larr; Back to Message List ({groupName})</Link>
      </div>

      <div className="message-header-details">
        <h1>{message.subject}</h1>
        <p><strong>From:</strong> {message.from}</p>
        <p><strong>Date:</strong> {new Date(message.date).toLocaleString()}</p>
        <p><strong>Group:</strong> {message.group}</p>
        <p><strong>Message ID:</strong> &lt;{message.id}&gt;</p>
        {message.references && message.references.length > 0 && (
          <p><strong>References:</strong> {message.references.map(ref => `<${ref}>`).join(' ')}</p>
        )}
      </div>

      <div className="message-body">
        {/* Assuming body is plain text for now. HTML sanitization will be backend's job.
            If backend sends pre-sanitized HTML, use dangerouslySetInnerHTML={{ __html: message.body }}
            but that requires trust and proper sanitization.
            For plain text, <pre> preserves whitespace.
        */}
        <pre>{message.body}</pre>
      </div>

      <div className="message-thread-navigation">
        <h3>Thread Navigation</h3>
        <div className="thread-links">
            {message.prevMessage && <div className="thread-link-prev">{renderThreadLink(message.prevMessage, "Previous in Thread")}</div>}
            {message.parent && <div className="thread-link-parent">{renderThreadLink(message.parent, "Parent Message")}</div>}
            {message.nextMessage && <div className="thread-link-next">{renderThreadLink(message.nextMessage, "Next in Thread")}</div>}
        </div>

        {message.children && message.children.length > 0 && (
          <div className="thread-children">
            <h4>Replies to this message:</h4>
            <ul>
              {message.children.map(child => (
                <li key={child.id}>
                  {renderThreadLink(child.id, `${child.subject} (by ${child.from})`)}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </div>
  );
};

export default MessageView;

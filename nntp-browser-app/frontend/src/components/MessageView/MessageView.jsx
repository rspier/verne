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
    // `encodedMessageId` from `useParams` is the value from the URL path segment.
    // This segment was created in MessageList.jsx using encodeURIComponent(msg.id).
    // So, `encodedMessageId` is the URL-encoded version of the message ID.
    // The backend's GetMessageHandler expects the message ID as it was passed in the path.
    // For example, if msg.id was "foo@bar", link is ".../foo%40bar",
    // `encodedMessageId` from useParams is "foo@bar" (router decodes path segments).
    // If msg.id was "<foo@bar>", link is ".../%3Cfoo%40bar%3E",
    // `encodedMessageId` from useParams is "<foo@bar>".
    // The fetch URL needs the segment to be properly encoded if it contains characters
    // that would break the URL structure if not part of a path segment.
    // However, since `encodedMessageId` IS the segment from the URL, we use it directly.
    // The `GetMessageHandler` in Go backend splits path and uses the segment as is.
    // If the original message ID was `<special/char>`, `encodeURIComponent` makes it safe for a path segment.
    // `useParams` gives the decoded version of that segment.
    // So, if the original ID was `<foo#bar>`, Link to is `.../%3Cfoo%23bar%3E`.
    // `encodedMessageId` via `useParams` would be `<foo#bar>`.
    // For the API call, this needs to be re-encoded to form a valid path segment:
    // `.../messages/${encodeURIComponent(encodedMessageId)}` if `encodedMessageId` can contain `/` etc.
    // But our current mock backend path parsing is simpler. Let's assume `encodedMessageId` as received
    // from `useParams` is suitable for being the last segment of the URL passed to fetch.
    // The key is that the backend expects the ID as it was in the original message-id header or similar unique identifier.
    // Our mock backend GetMessageHandler takes the path segment as the messageID directly.
    // Links in MessageList do `encodeURIComponent(msg.id)`.
    // So, if `msg.id` is `<foo@bar.com>`, `encodedMessageId` from params is `<foo@bar.com>`.
    // The fetch URL should be `.../messages/${encodeURIComponent(encodedMessageId)}` to be robust,
    // as the `encodedMessageId` (decoded from path) could still contain chars like `#` or `?` if the original ID had them
    // (though message-ids usually don't).
    // Given the backend mock handler's current simple path splitting, it expects the segment as is.
    // The crucial part for the API call is that the `messageId` segment in the URL
    // must match what the backend expects to identify the message.
    // Let's use `encodedMessageId` directly as it's what `useParams` gives from the path segment.

    fetch(`http://localhost:8080/api/groups/${groupName}/messages/${encodedMessageId}`)
      .then(response => {
        if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
        return response.json();
      })
      .then(data => {
        setMessage(data);
        setLoading(false);
      })
      .catch(err => {
        setError(err.message);
        setLoading(false);
      });
  }, [groupName, encodedMessageId]);

  // Display decoded messageId if needed in UI, but use encodedMessageId from params for API/keys if it's the identifier used in paths.
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

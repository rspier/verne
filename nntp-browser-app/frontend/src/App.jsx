import React from 'react';
import { Routes, Route, Link } from 'react-router-dom';
import GroupList from './components/GroupList/GroupList';
import MessageList from './components/MessageList/MessageList';
import MessageView from './components/MessageView/MessageView';
import './App.css'; // For global styles and layout

// A simple page component for the home route, if needed, or GroupList can be the default.
const HomePage = () => (
  <div>
    {/* The GroupList component already has an <h2>Newsgroups</h2> title */}
    {/* So, we might not need another H1 here if GroupList is the main content of homepage */}
    {/* <p>Welcome! Select a group to view messages.</p> */}
    <GroupList />
  </div>
);

// Placeholder for a Not Found page
const NotFoundPage = () => (
  <div style={{ textAlign: 'center', marginTop: '50px' }}>
    <h2>404 - Page Not Found</h2>
    <p>Sorry, the page you are looking for does not exist.</p>
    <Link to="/">Go to Homepage</Link>
  </div>
);


function App() {
  return (
    <div className="app-container">
      <header className="app-header">
        <Link to="/" className="header-title-link">
          <h1>NNTP React Browser</h1>
        </Link>
        {/* Could add a small nav bar here if needed later */}
      </header>
      <main className="app-main-content">
        <Routes>
          {/* Route for listing groups, typically the homepage */}
          <Route path="/" element={<GroupList />} />

          {/* Route for listing messages in a specific group */}
          <Route path="/groups/:groupName/messages" element={<MessageList />} />

          {/* Route for viewing a single message.
              Ensure messageId is URL encoded when linking (already handled in MessageList)
              and decoded by MessageView using useParams (also handled).
          */}
          <Route path="/groups/:groupName/messages/:messageId" element={<MessageView />} />

          {/* Catch-all route for 404 Not Found pages */}
          <Route path="*" element={<NotFoundPage />} />
        </Routes>
      </main>
      <footer className="app-footer">
        <p>&copy; 2024 NNTP Browser App</p>
      </footer>
    </div>
  );
}

export default App;

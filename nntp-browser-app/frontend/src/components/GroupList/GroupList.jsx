import React, { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import './GroupList.css'; // We'll create this later for basic styling

const GroupList = () => {
  const [groups, setGroups] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    setLoading(true);
    setError(null);
    // Fetch from backend API (default port 8080 for backend)
    fetch('http://localhost:8080/api/groups')
      .then(response => {
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`);
        }
        return response.json();
      })
      .then(data => {
        setGroups(data);
        setLoading(false);
      })
      .catch(err => {
        setError(err.message);
        setLoading(false);
      });
  }, []);

  if (loading) return <p>Loading groups...</p>;
  if (error) return <p>Error loading groups: {error}</p>;

  return (
    <div className="group-list-container">
      <h2>Newsgroups</h2>
      {groups.length === 0 ? (
        <p>No groups found.</p>
      ) : (
        <ul className="group-list">
          {groups.map(group => (
            <li key={group.name} className="group-list-item">
              <Link to={`/groups/${group.name}/messages`}>
                <span className="group-name">{group.name}</span>
                <span className="group-count">({group.count} articles)</span>
                {group.description && <span className="group-description">{group.description}</span>}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};

export default GroupList;

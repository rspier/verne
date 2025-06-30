import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import MessageView from './MessageView';
import { describe, it, expect, vi } from 'vitest';

// Mock useParams to provide groupName and messageId
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    useParams: () => ({
      groupName: 'test.group',
      messageId: encodeURIComponent('test-message-id@example.com'), // Ensure it's encoded as it would be in URL
    }),
  };
});

describe('MessageView Component', () => {
  it('renders loading state and then the message details', async () => {
    render(
      <MemoryRouter initialEntries={['/groups/test.group/messages/test-message-id%40example.com']}>
        <Routes>
            <Route path="/groups/:groupName/messages/:messageId" element={<MessageView />} />
        </Routes>
      </MemoryRouter>
    );

    // Check for loading text (might be quick)
    // We will directly check for content post-loading, assuming mock data is synchronous.
    // The messageId in the text should be the decoded one.
    expect(screen.getByText('Loading message test-message-id@example.com...')).toBeInTheDocument();

    // Wait for mock data to load and component to re-render
    await waitFor(() => {
      expect(screen.getByText('Subject of: test-message-id@example.com')).toBeInTheDocument();
    });

    expect(screen.getByText(/From: Sender <sender@example.com>/i)).toBeInTheDocument();
    expect(screen.getByText(/This is the body of message test-message-id@example.com/i)).toBeInTheDocument();

    // Check for thread navigation links based on mock data
    expect(screen.getByText('Parent Message')).toHaveAttribute('href', '/groups/test.group/messages/parent-id%40example.com');
    expect(screen.getByText('Next in Thread')).toHaveAttribute('href', '/groups/test.group/messages/next-in-thread%40example.com');
    expect(screen.getByText('Previous in Thread')).toHaveAttribute('href', '/groups/test.group/messages/prev-in-thread%40example.com');

    // Check for child messages
    expect(screen.getByText(/Re: Subject of: test-message-id@example.com \(by Child Sender 1\)/i)).toBeInTheDocument();
  });

  it('renders "Back to Message List" link', async () => {
    render(
        <MemoryRouter initialEntries={['/groups/test.group/messages/test-message-id%40example.com']}>
            <Routes>
                <Route path="/groups/:groupName/messages/:messageId" element={<MessageView />} />
            </Routes>
      </MemoryRouter>
    );
    await waitFor(() => {
        expect(screen.getByText('Subject of: test-message-id@example.com')).toBeInTheDocument();
    });

    const backLink = screen.getByRole('link', { name: /Back to Message List \(test.group\)/i });
    expect(backLink).toBeInTheDocument();
    expect(backLink).toHaveAttribute('href', '/groups/test.group/messages');
  });
});

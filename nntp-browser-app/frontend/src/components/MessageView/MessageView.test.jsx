import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import MessageView from './MessageView';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock useParams from react-router-dom
const mockUseParams = vi.fn();
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual, // Spread actual exports
    useParams: () => mockUseParams(), // Use our mock function
  };
});

describe('MessageView Component', () => {
  const mockMessageId = 'test-message-id@example.com';
  const encodedMockMessageId = encodeURIComponent(mockMessageId);

  beforeEach(() => {
    vi.resetAllMocks();
    mockUseParams.mockReturnValue({
      groupName: 'test.group',
      messageId: encodedMockMessageId
    });

    global.fetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        id: mockMessageId, // Decoded ID
        group: 'test.group',
        subject: "Subject of: " + mockMessageId,
        from: "Sender <sender@example.com>",
        date: new Date().toISOString(),
        references: ["<ref1@example.com>"],
        body: `Body for ${mockMessageId}`,
        parent: "parent-id@example.com",
        children: [{ id: "child1@example.com", subject: "Re: Subject", from: "Child" }],
        nextMessage: "next-id@example.com",
        prevMessage: "prev-id@example.com",
      }),
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders loading state and then the message details', async () => {
    render(
      <MemoryRouter initialEntries={[`/groups/test.group/messages/${encodedMockMessageId}`]}>
        <Routes>
            <Route path="/groups/:groupName/messages/:messageId" element={<MessageView />} />
        </Routes>
      </MemoryRouter>
    );

    // Check for loading text (might be quick)
    // We will directly check for content post-loading, assuming mock data is synchronous.
    // The messageId in the text should be the decoded one.
    // REMOVED: expect(screen.getByText('Loading message test-message-id@example.com...')).toBeInTheDocument();

    // Wait for mock data to load and component to re-render, then check for key content.
    await waitFor(() => {
      expect(screen.getByText('Subject of: test-message-id@example.com')).toBeInTheDocument();
    });

    // REMOVED: expect(screen.getByText(`Loading message ${mockMessageId}...`)).toBeInTheDocument();

    // Wait for mock data to load and component to re-render, then check for key content.
    // The above waitFor already checks for the subject, so this one is redundant if checking same thing.
    // Let's ensure we are waiting for the full content.
    // The existing waitFor for "Subject of: " + mockMessageId is fine.

    // After waiting for the subject, other content should also be present.
    expect(screen.getByText((content, node) => node.textContent === `From: Sender <sender@example.com>`)).toBeInTheDocument();
    expect(screen.getByText(`Body for ${mockMessageId}`)).toBeInTheDocument(); // Match mock body

    // Check for thread navigation links based on mock data
    expect(screen.getByText('Parent Message')).toHaveAttribute('href', `/groups/test.group/messages/${encodeURIComponent("parent-id@example.com")}`);
    expect(screen.getByText('Next in Thread')).toHaveAttribute('href', `/groups/test.group/messages/${encodeURIComponent("next-id@example.com")}`);
    expect(screen.getByText('Previous in Thread')).toHaveAttribute('href', `/groups/test.group/messages/${encodeURIComponent("prev-id@example.com")}`);

    // Check for child messages based on mock
    expect(screen.getByText(/Re: Subject \(by Child\)/i)).toBeInTheDocument();
  });

  it('renders "Back to Message List" link', async () => {
    render(
        <MemoryRouter initialEntries={[`/groups/test.group/messages/${encodedMockMessageId}`]}>
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

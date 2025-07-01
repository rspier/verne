import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom'; // BrowserRouter not needed if using MemoryRouter
import MessageList from './MessageList';
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

describe('MessageList Component', () => {
  beforeEach(() => {
    vi.resetAllMocks(); // Reset all mocks

    // Setup mock for useParams for each test
    mockUseParams.mockReturnValue({ groupName: 'test.group' });

    // Mock successful fetch for messages
    global.fetch.mockResolvedValue({
      ok: true,
      json: async () => [
        { id: "msg1@example.com", group: "test.group", number: 1, subject: "First message in test.group", from: "User1", date: new Date().toISOString(), messageCountInThread: 1 },
        { id: "msg2@example.com", group: "test.group", number: 2, subject: "Another interesting topic", from: "User2", date: new Date(Date.now() - 86400000).toISOString(), messageCountInThread: 2 },
      ],
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders loading state initially and then messages for a group', async () => {
    render(
      <MemoryRouter initialEntries={['/groups/test.group/messages']}>
        <Routes>
            <Route path="/groups/:groupName/messages" element={<MessageList />} />
        </Routes>
      </MemoryRouter>
    );

    expect(screen.getByText('Loading messages for test.group...')).toBeInTheDocument();

    // After fetch mock resolves
    expect(await screen.findByText('Messages in test.group')).toBeInTheDocument();
    expect(await screen.findByText(/First message in test.group/i)).toBeInTheDocument();

    // Check for the specific non-reply message
    const originalTopic = await screen.findByText((content, element) =>
        content === "Another interesting topic" &&
        element.tagName.toLowerCase() === 'span' &&
        element.classList.contains('message-subject')
    );
    expect(originalTopic).toBeInTheDocument();

    const expectedEncodedId = encodeURIComponent("msg1@example.com");
    // Find link by its accessible name, which is a concatenation of its text content.
    // This might be tricky if subject and from are in separate spans.
    // A data-testid attribute on the link would be more robust.
    // For now, let's assume the link containing "First message..." can be found.
    const messageLink = await screen.findByRole('link', { name: /First message in test.group/i });
    expect(messageLink).toHaveAttribute('href', `/groups/test.group/messages/${expectedEncodedId}`);
  });

  it('renders "Back to Group List" link', async () => {
    render(
        <MemoryRouter initialEntries={['/groups/test.group/messages']}>
            <Routes>
                <Route path="/groups/:groupName/messages" element={<MessageList />} />
            </Routes>
        </MemoryRouter>
    );
    // Wait for content to load
    expect(await screen.findByText(/First message in test.group/i)).toBeInTheDocument();

    const backLink = screen.getByRole('link', { name: /Back to Group List/i });
    expect(backLink).toBeInTheDocument();
    expect(backLink).toHaveAttribute('href', '/');
  });
});

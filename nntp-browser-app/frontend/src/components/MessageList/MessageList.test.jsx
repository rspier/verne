import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { BrowserRouter, MemoryRouter, Routes, Route } from 'react-router-dom';
import MessageList from './MessageList';
import { describe, it, expect, vi } from 'vitest';

// Mock useParams
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    useParams: () => ({
      groupName: 'test.group', // Provide a mock groupName
    }),
  };
});

describe('MessageList Component', () => {
  it('renders loading state initially and then messages for a group', async () => {
    render(
      <MemoryRouter initialEntries={['/groups/test.group/messages']}>
        <Routes>
            <Route path="/groups/:groupName/messages" element={<MessageList />} />
        </Routes>
      </MemoryRouter>
    );

    // Check for loading text initially (might be too fast to catch reliably without async mocks)
    // For now, we'll check for the main title
    expect(screen.getByText('Messages in test.group')).toBeInTheDocument();

    // Mock data is used in useEffect. Wait for it to render.
    // Check for one of the mock message subjects
    await waitFor(() => {
        expect(screen.getByText(/First message in test.group/i)).toBeInTheDocument();
    });

    // Specifically find the message subject "Another interesting topic" (not the "Re:" one)
    // by ensuring the text is exactly "Another interesting topic"
    expect(screen.getByText((content, element) => {
        // Check that the text content is exactly "Another interesting topic"
        // and that it's a span with the class 'message-subject' to be more specific
        return content === "Another interesting topic" &&
               element.tagName.toLowerCase() === 'span' &&
               element.classList.contains('message-subject');
      })).toBeInTheDocument();

    // Check for links to messages (ensure IDs are handled)
    // The mock messages have IDs like "msg1@example.com"
    // The Link component in MessageList uses encodeURIComponent for these IDs.
    const expectedEncodedId = encodeURIComponent("msg1@example.com");
    const messageLink = screen.getByRole('link', { name: /First message in test.group/i });
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
    await waitFor(() => {
        expect(screen.getByText(/First message in test.group/i)).toBeInTheDocument();
    });
    const backLink = screen.getByRole('link', { name: /Back to Group List/i });
    expect(backLink).toBeInTheDocument();
    expect(backLink).toHaveAttribute('href', '/');
  });
});

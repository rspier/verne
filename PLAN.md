# Meow Chat App - Development Plan

This document outlines the plan for building the Meow chat application.

## I. Project Setup

1.  **Initialize Project Structure:**
    *   Create a root directory named `meow`.
    *   Inside `meow`, create `frontend` and `backend` directories.
2.  **Backend Setup (Go):**
    *   Initialize a Go module in the `backend` directory.
    *   Set up a basic HTTP server using the standard library or a lightweight framework like Gin.
    *   Implement structured logging (e.g., using `log/slog`).
3.  **Frontend Setup (Angular):**
    *   Initialize a new Angular application in the `frontend` directory using the Angular CLI.
    *   Set up a responsive layout using CSS Flexbox or Grid to ensure mobile, tablet, and desktop friendliness.

## II. Core Backend Features & Testing

1.  **Database Integration (Sqlite):**
    *   Integrate `mattn/go-sqlite3` to connect to a Sqlite database.
    *   Define the database schema for users, channels, messages, and direct messages.
    *   Implement data access objects (DAOs) for database operations.
    *   *Write unit tests for all database functions.*
2.  **User Authentication (Google OAuth):**
    *   Integrate a Go OAuth2 library (e.g., `golang.org/x/oauth2`).
    *   Implement Google login and callback handlers.
    *   Manage user sessions.
    *   *Write tests for the authentication flow.*
3.  **Real-time Communication (Server-Sent Events):**
    *   Implement an SSE handler on the backend to stream messages to clients.
    *   Manage client connections and channels.
    *   *Write tests for the SSE handler.*
4.  **API Endpoints:**
    *   Create RESTful API endpoints for:
        *   User authentication (login, logout).
        *   Fetching channel and direct message history.
        *   Sending messages.
        *   Image uploads.
    *   *Write integration tests for all API endpoints.*

## III. Core Frontend Features & Testing

1.  **Component Structure:**
    *   Create Angular components for:
        *   Login page.
        *   Main chat view (sidebar with channels/DMs, message display area, message input).
        *   Message component.
    *   *Write component tests for each component.*
2.  **Authentication Flow:**
    *   Implement a login page that redirects to Google for authentication.
    *   Create an authentication service to manage user sessions and tokens.
    *   Protect routes to ensure only authenticated users can access the chat.
    *   *Write tests for the authentication service and route guards.*
3.  **Real-time Messaging (Server-Sent Events):**
    *   Implement an `EventSource` service to connect to the backend's SSE endpoint.
    *   Receive and display incoming messages in real-time.
    *   *Write tests for the SSE service.*
4.  **Message Display:**
    *   Display messages with user avatars, names, and timestamps.
    *   Render formatted text (bold, italic, etc.).
    *   Linkify URLs.
    *   Display images.
    *   *Write tests for message rendering.*

## IV. Advanced Features & Testing

1.  **WYSIWYG Editor:**
    *   Integrate a lightweight WYSIWYG editor library (e.g., Quill.js, TinyMCE) into the message input component.
    *   Configure the editor for basic formatting options (bold, italic, underline, strikethrough).
    *   *Write tests for the editor component.*
2.  **HTML Sanitization:**
    *   On the backend, use a library like `microcosm-cc/bluemonday` to sanitize all incoming message content before storing or broadcasting it.
    *   *Write tests to ensure sanitization is effective.*
3.  **Image Uploads:**
    *   Implement file upload functionality on the frontend.
    *   Create a backend endpoint to handle image uploads, store them, and return a URL.
    *   Display uploaded images in the chat.
    *   *Write tests for the image upload feature.*
4.  **Notifications:**
    *   Use the browser's Notification API to display desktop notifications for new messages.
    *   Play a sound when a new message is received.
    *   *Write tests for the notification service.*
5.  **Channels and Direct Messages:**
    *   Implement logic to switch between different channels and direct message conversations.
    *   Highlight the active channel/DM.
    *   Display unread message indicators.
    *   *Write tests for channel and DM functionality.*

## V. Deployment

1.  **Containerization:**
    *   Containerize the frontend and backend applications using Docker.
    *   Create a `docker-compose.yml` for easy local development.
2.  **Deployment:**
    *   Deploy to a cloud platform (e.g., Heroku, AWS, Google Cloud).

This plan will be executed in the order presented, focusing on delivering a functional core before adding more advanced features. Each major section will be a milestone. Testing is integrated into each step of the development process.

# Meow Chat App - Development Plan

This document outlines the plan for building the Meow chat application.

## I. Project Setup

1.  **Initialize Project Structure:**
    *   Create a root directory named `meow`.
    *   Inside `meow`, create `frontend` and `backend` directories.
2.  **Backend Setup (Go):**
    *   Initialize a Go module in the `backend` directory.
    *   Set up a basic HTTP server using the standard library or a lightweight framework like Gin.
3.  **Frontend Setup (Angular):**
    *   Initialize a new Angular application in the `frontend` directory using the Angular CLI.

## II. Core Backend Features

1.  **Database Integration (Sqlite):**
    *   Integrate `mattn/go-sqlite3` to connect to a Sqlite database.
    *   Define the database schema for users, channels, messages, and direct messages.
    *   Implement data access objects (DAOs) for database operations.
2.  **User Authentication (Google OAuth):**
    *   Integrate a Go OAuth2 library (e.g., `golang.org/x/oauth2`).
    *   Implement Google login and callback handlers.
    *   Manage user sessions.
3.  **WebSocket for Real-time Communication:**
    *   Integrate a WebSocket library (e.g., `gorilla/websocket`).
    *   Implement a WebSocket handler to manage client connections.
    *   Broadcast messages to connected clients in real-time.
4.  **API Endpoints:**
    *   Create RESTful API endpoints for:
        *   User authentication (login, logout).
        *   Fetching channel and direct message history.
        *   Sending messages.
        *   Image uploads.

## III. Core Frontend Features

1.  **Component Structure:**
    *   Create Angular components for:
        *   Login page.
        *   Main chat view (sidebar with channels/DMs, message display area, message input).
        *   Message component.
2.  **Authentication Flow:**
    *   Implement a login page that redirects to Google for authentication.
    *   Create an authentication service to manage user sessions and tokens.
    *   Protect routes to ensure only authenticated users can access the chat.
3.  **Real-time Messaging:**
    *   Implement a WebSocket service to connect to the backend.
    *   Receive and display incoming messages in real-time.
4.  **Message Display:**
    *   Display messages with user avatars, names, and timestamps.
    *   Render formatted text (bold, italic, etc.).
    *   Linkify URLs.
    *   Display images.

## IV. Advanced Features

1.  **WYSIWYG Editor:**
    *   Integrate a lightweight WYSIWYG editor library (e.g., Quill.js, TinyMCE) into the message input component.
    *   Configure the editor for basic formatting options (bold, italic, underline, strikethrough).
2.  **HTML Sanitization:**
    *   On the backend, use a library like `microcosm-cc/bluemonday` to sanitize all incoming message content before storing or broadcasting it.
3.  **Image Uploads:**
    *   Implement file upload functionality on the frontend.
    *   Create a backend endpoint to handle image uploads, store them, and return a URL.
    *   Display uploaded images in the chat.
4.  **Notifications:**
    *   Use the browser's Notification API to display desktop notifications for new messages.
    *   Play a sound when a new message is received.
5.  **Channels and Direct Messages:**
    *   Implement logic to switch between different channels and direct message conversations.
    *   Highlight the active channel/DM.
    *   Display unread message indicators.

## V. Testing and Deployment

1.  **Backend Testing:**
    *   Write unit tests for Go backend logic (e.g., database operations, API handlers).
2.  **Frontend Testing:**
    *   Write unit and component tests for Angular components and services.
3.  **Deployment:**
    *   Containerize the frontend and backend applications using Docker.
    *   Create a `docker-compose.yml` for easy local development and deployment.
    *   Deploy to a cloud platform (e.g., Heroku, AWS, Google Cloud).

This plan will be executed in the order presented, focusing on delivering a functional core before adding more advanced features. Each major section will be a milestone.

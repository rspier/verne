# NNTP Web Frontend

This project is a web-based frontend for browsing NNTP (Network News Transfer Protocol) newsgroups.
It aims to provide a user-friendly interface for reading articles and navigating discussions.

## Features (Implemented)

*   List all available newsgroups (`/group/`).
*   View messages within a newsgroup, paginated by month, with smarter navigation (only links to months with content).
    *   Default view (`/group/{groupname}/`) shows the latest month with messages.
    *   Specific month: `/group/{groupname}/{year}/{month}.html`.
*   Display individual articles (`/group/{groupname}/{year}/{month}/msg{id}.html`):
    *   Fetches full raw article from NNTP server (basic reconnect logic implemented).
    *   Parses MIME content, preferring sanitized HTML (via `bluemonday`) over plain text.
    *   Indicates if other non-text MIME parts exist.
    *   Displays only Subject, From, Date, and MessageID prominently.
*   Show other messages in the same thread (flat list) at the bottom of an article page. (Advanced JWZ threading is prepared in models but deferred in display).
*   Redirects for canonical article URLs (by Message-ID or correct date, with `;.msgid=` as a path parameter).

## Tech Stack

*   Go (using standard library `html/template`, `net/http`, `net/textproto` for basic NNTP, `net/mail`, `mime/multipart` for MIME parsing)
*   MySQL (for caching/indexing NNTP data, schema based on [Colobus3 SQL](https://github.com/perlorg/cnntp/blob/main/sql/colobus3.sql))
*   `github.com/go-sql-driver/mysql` for MySQL database connectivity.
*   `github.com/DATA-DOG/go-sqlmock` for database testing.
*   `github.com/microcosm-cc/bluemonday` for HTML sanitization.
*   `github.com/gatherstars-com/jwz` (dependency added; full hierarchical threading display deferred due to build issues with library).
*   Docker (for containerized deployment)
*   (Note on `safehtml`: Using `html/template` for parsing; `safehtml` types would be for data if needed).

## Prerequisites

*   Go (version 1.18+ recommended for `embed.FS`)
*   A running MySQL instance.
*   The database schema applied to your MySQL instance (see `https://github.com/perlorg/cnntp/blob/main/sql/colobus3.sql`).
*   Data populated in the `groups` and `articles` tables.

## Setup and Running

1.  **Clone the repository (if applicable) or ensure all code is present.**
2.  **Database Configuration**: Ensure your MySQL server is running and you have a database created. Apply the schema from the link above.
3.  **Build the application (optional):**
    ```bash
    go build -o nntp-web-app cmd/nntp-web/main.go
    ```
4.  **Run the application:**
    Replace the placeholder values with your actual database credentials and desired ports.
    ```bash
    # If built:
    ./nntp-web-app -db-host=localhost -db-port=3306 -db-user=youruser -db-pass=yourpassword -db-name=yourdbname -server-port=8080 -nntp-server=news.example.com:119

    # Or run directly:
    go run cmd/nntp-web/main.go -db-host=localhost -db-port=3306 -db-user=youruser -db-pass=yourpassword -db-name=yourdbname -server-port=8080 -nntp-server=news.example.com:119
    ```
5.  **Access the application:** Open your web browser and go to `http://localhost:8080/group/` (or your configured server port).

### Command-line Flags
*   `-db-host`: Database host (default: `localhost`)
*   `-db-port`: Database port (default: `3306`)
*   `-db-user`: Database user (default: `user`)
*   `-db-pass`: Database password (default: `password`)
*   `-db-name`: Database name (default: `nntp_cache`)
*   `-server-port`: HTTP server port (default: `8080`)
*   `-nntp-server`: Backend NNTP server address (host:port) (default: `news.example.com:119`) - Used by the internal NNTP client to fetch article bodies.
```

## Docker Build and Run

1.  **Build the Docker image:**
    ```bash
    docker build -t nntp-web-app .
    ```

2.  **Run the Docker container:**
    Make sure to replace placeholder values for database connection and the NNTP server.
    The application inside the container listens on port 8080 by default (or as specified by `-server-port`).
    ```bash
    docker run -p 8080:8080 --rm nntp-web-app \
        -db-host=your_mysql_host \
        -db-port=3306 \
        -db-user=your_db_user \
        -db-pass=your_db_password \
        -db-name=your_db_name \
        -nntp-server=your_nntp_server:119
    ```
    *   `your_mysql_host`: If MySQL is running on your host machine, you might use `host.docker.internal` (on Docker Desktop) or your machine's IP address. If it's another Docker container, use its container name and ensure they are on the same Docker network.
    *   `-p 8080:8080`: Maps port 8080 of your host to port 8080 in the container.
    *   `--rm`: Automatically removes the container when it exits.
```

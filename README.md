# NNTP Web Frontend

This project is a web-based frontend for browsing NNTP (Network News Transfer Protocol) newsgroups.
It aims to provide a user-friendly interface for reading articles and navigating discussions.

## Features (Implemented)

*   List all available newsgroups (`/group/`).
*   View messages within a newsgroup, paginated by month.
    *   Current month: `/group/{groupname}/`
    *   Specific month: `/group/{groupname}/{year}/{month}.html`
    *   Navigation links for previous/next month.
*   Display individual articles (`/group/{groupname}/{year}/{month}/msg{id}.html`).
    *   Prominent display of Subject, From, Date, Message-ID.
    *   Placeholder for article body (to be fetched from NNTP server).
*   Show other messages in the same thread at the bottom of an article page.
*   Redirects for canonical article URLs:
    *   Lookup by Message-ID: `/group/{groupname}/;.msgid={messageid}` redirects to the canonical article path.
    *   If an article is accessed with an incorrect year/month in its path, it redirects to the path with the correct date.

## Tech Stack

*   Go (using standard library `html/template` and `net/http`)
*   MySQL (for caching/indexing NNTP data, schema based on [Colobus3 SQL](https://github.com/perlorg/cnntp/blob/main/sql/colobus3.sql))
*   `github.com/google/safehtml` for secure HTML generation.
*   `github.com/go-sql-driver/mysql` for MySQL database connectivity.
*   `github.com/DATA-DOG/go-sqlmock` for database testing.

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
*   `-nntp-server`: Backend NNTP server address (host:port) (default: `news.example.com:119`) - Currently used by a placeholder client.
```

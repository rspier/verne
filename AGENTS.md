## Agent Guidelines for NNTP-Web Project

When working on this Go project, please adhere to the following guidelines:

1.  **HTML Generation**:
    *   All HTML is generated using the standard Go `html/template` package. This package provides context-aware auto-escaping, which is the primary defense against XSS.
    *   For HTML content that originates from external sources (like article bodies from NNTP), it **must** be sanitized using `bluemonday.UGCPolicy()` (or a similarly strict policy) before being rendered into a template as `html/template.HTML`.
    *   The `github.com/google/safehtml` library is not directly used for template construction or parsing in this project.

2.  **Database Interaction**:
    *   Prefer data from the MySQL database when available.
    *   Fall back to direct NNTP server queries only for data not present in the database or when real-time freshness is explicitly required for a feature and the database might be stale.
    *   Use prepared statements for all SQL queries to prevent SQL injection.

3.  **Error Handling**:
    *   Handle errors gracefully. Do not let the application panic unless it's a truly unrecoverable situation during startup (e.g., cannot bind to port).
    *   Provide informative error messages to the user where appropriate, without exposing sensitive internal details.

4.  **Code Style**:
    *   Follow standard Go formatting (`gofmt`).
    *   Write clear, commented code, especially for complex logic or non-obvious decisions.

5.  **Testing**:
    *   Write unit tests for new functionality, especially for database queries, business logic, and HTTP handlers.
    *   Aim for good test coverage.

6.  **Dependencies**:
    *   Minimize external dependencies. When adding a new one, discuss its necessity.

7.  **Security**:
    *   Be mindful of security implications in all aspects of development (XSS, SQLi, CSRF, etc.).
    *   The use of `safehtml` (or `html/template`'s default auto-escaping) is a key part of XSS prevention.

8.  **Pre-Submission Checks**:
    *   All tests (`go test ./...`) must pass.
    *   The project must build successfully (`go build ./...`).
    *   Run `go mod tidy` to ensure `go.mod` and `go.sum` are up-to-date and consistent.
    *   Never commit compiled binaries (e.g., executables, `.o` files) into the version control system. Ensure your `.gitignore` is configured appropriately.

These guidelines may be updated as the project evolves.

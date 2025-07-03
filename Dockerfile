# ---- Build Stage ----
FROM golang:1.22-alpine AS builder

# Set working directory
WORKDIR /app

# Install build tools if necessary (e.g., git for private modules, though not needed here)
# RUN apk add --no-cache git

# Copy go.mod and go.sum first to leverage Docker cache for dependencies
COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

# Copy the entire source code
COPY . .

# Build the application
# CGO_ENABLED=0 for static linking & smaller images, suitable for Alpine.
# -ldflags="-w -s" for smaller binary (strips debug info)
# Output binary to /app/nntp-web (or a name of your choice)
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-w -s" -installsuffix cgo -o /app/nntp-web-server cmd/nntp-web/main.go

# ---- Runtime Stage ----
FROM alpine:latest

# It's good practice to run as a non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
USER appuser

WORKDIR /home/appuser/app

# Copy the binary from the builder stage
COPY --from=builder /app/nntp-web-server /home/appuser/app/nntp-web-server

# The templates are embedded in the Go binary via web/templates/templates.go using //go:embed
# So, no need to copy the web/templates directory separately if they are fully embedded.

# Expose the default port the application listens on.
# This should match the -server-port flag's default or be configurable.
# The default in config.go is 8080.
EXPOSE 8080

# Command to run the application.
# Users can override command-line flags when running 'docker run'.
# Example: docker run -p 8080:8080 my-nntp-app -db-host=my-db-host ...
ENTRYPOINT ["/home/appuser/app/nntp-web-server"]

# Default command line arguments (can be overridden)
# These are just examples; actual values should be provided at runtime.
CMD ["-server-port=8080", "-db-host=database", "-db-name=nntp_cache", "-nntp-server=news.example.com:119"]

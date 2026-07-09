# Multi-stage Dockerfile for Go API production deployment
# Creates a minimal final image by separating build and runtime stages
# Note: Grafana Alloy runs in a separate container in Docker Compose

# Stage 1: Build the Go application
FROM golang:1.25-alpine AS builder
WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application, migration tool and background worker
# CGO_ENABLED=0 creates a statically linked binary
# -ldflags="-w -s" strips debug information to reduce binary size
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags="-w -s" \
    -o /app/bin/openmentor-api \
    ./cmd/api/main.go && \
    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags="-w -s" \
    -o /app/bin/migrate \
    ./cmd/migrate/main.go && \
    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags="-w -s" \
    -o /app/bin/worker \
    ./cmd/worker/main.go

# Stage 2: Production runtime image
# Using Debian for better compatibility with various dependencies
FROM debian:bookworm-slim AS runner
WORKDIR /app

# Install runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user for security
RUN groupadd -g 1001 appgroup && \
    useradd -u 1001 -g appgroup -m -s /bin/bash appuser

# Create necessary directories
RUN mkdir -p /app/logs && \
    chown -R appuser:appgroup /app

# Copy Go binaries from builder
COPY --from=builder /app/bin/openmentor-api /app/openmentor-api
COPY --from=builder /app/bin/migrate /app/migrate
COPY --from=builder /app/bin/worker /app/worker
RUN chmod +x /app/openmentor-api /app/migrate /app/worker

# Copy migrations directory
COPY --chown=appuser:appgroup migrations /app/migrations

# Set proper ownership
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose application ports
# 8081: API HTTP server (internal in Docker Compose, no public exposure)
# 8090: Worker HTTP server (internal only; used when running CMD ["/app/worker"])
EXPOSE 8081 8090

# Set environment variables
ENV PORT=8081
ENV GIN_MODE=release
ENV LOG_DIR=/app/logs

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8081/api/healthcheck || exit 1

# Run the Go application directly.
# The same image ships three binaries; the compose service picks the command:
#   api container:    /app/openmentor-api (default)
#   migrate runner:   /app/migrate
#   worker container: /app/worker (healthcheck: curl -f http://localhost:8090/healthz)
CMD ["/app/openmentor-api"]

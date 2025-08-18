# Build stage
FROM golang:1.21-alpine AS builder

# Install git and ca-certificates for dependency management and HTTPS
RUN apk add --no-cache git ca-certificates

# Set the working directory
WORKDIR /app

# Copy go.mod and go.sum for dependency caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version info
ARG VERSION="dev"
ARG COMMIT="unknown"
ARG DATE="unknown"

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -a -installsuffix cgo \
    -o claude-code-lb .

# Create data directory structure
RUN mkdir -p /tmp/data && chown 65534:65534 /tmp/data

# Final stage - minimal image
FROM scratch

# Import CA certificates from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the compiled binary
COPY --from=builder /app/claude-code-lb /claude-code-lb

# Create data directory for configuration
COPY --from=builder --chown=65534:65534 /tmp/data /data

# Create a non-root user (scratch doesn't have adduser, so we use numeric UID)
USER 65534:65534

# Create volume mount point for configuration and future extensions
VOLUME ["/data"]

# Expose port
EXPOSE 3000

# Labels for better container management
LABEL org.opencontainers.image.title="Claude Code Load Balancer" \
      org.opencontainers.image.description="A high-performance load balancer for Claude API endpoints" \
      org.opencontainers.image.source="https://github.com/your-username/claude-code-lb" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${DATE}"

# Run the application with config file in /data
ENTRYPOINT ["/claude-code-lb", "-c", "/data/config.json"]
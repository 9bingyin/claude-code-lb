# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a high-performance Claude API load balancer written in Go, supporting multiple load balancing strategies, failover, and health checking. The architecture follows a modular design with clear separation of concerns.

## Key Architecture Components

### Core Architecture Pattern
The system uses a **Selector Pattern** where different load balancing strategies implement a common `ServerSelector` interface:

- `LoadBalancerSelector`: Implements round-robin, weighted round-robin, and random algorithms
- `FallbackSelector`: Implements priority-based failover with automatic priority assignment
- The `Balancer` acts as a wrapper around selectors, chosen by the factory pattern in `internal/selector/factory.go`

### Request Flow
1. **Authentication**: `internal/auth/middleware.go` validates API keys if auth is enabled
2. **Server Selection**: `Balancer` uses the appropriate selector to choose an upstream server
3. **Proxy Handling**: `internal/proxy/handler.go` forwards requests with header management and streaming support
4. **Statistics & Health**: `internal/stats/reporter.go` tracks metrics while `internal/health/checker.go` manages passive health checking

### Configuration System
Two configuration templates support different use cases:
- `config.example.json`: Load balancing mode for traffic distribution
- `config.fallback.example.json`: Failover mode for primary/backup scenarios

The configuration supports both modes through the `mode` field, with automatic priority assignment for servers when priorities are not explicitly set.

### Debug Logging System
The `internal/logger` package provides structured logging with:
- Color-coded log levels and timestamps
- Multi-line content formatting with indentation
- JSON auto-formatting for request/response bodies
- Stream chunk display for real-time debugging

## Common Development Commands

### Build and Test
```bash
# Build for current platform
go build -o claude-code-lb .

# Run all tests
go test ./...

# Run tests for specific package
go test ./internal/selector/...

# Run with race detection
go test -race ./...

# Format code
go fmt ./...
```

### Local Development
```bash
# Run with default config
go run main.go

# Run with custom config
go run main.go -c /path/to/config.json

# Enable debug mode (set "debug": true in config)
```

### Cross-compilation
```bash
# Linux AMD64
GOOS=linux GOARCH=amd64 go build -o claude-code-lb-linux-amd64 .

# macOS ARM64
GOOS=darwin GOARCH=arm64 go build -o claude-code-lb-darwin-arm64 .
```

## Configuration Modes

### Load Balance Mode (`"mode": "load_balance"`)
- Uses `algorithm` field: `"round_robin"`, `"weighted_round_robin"`, `"random"`
- All healthy servers participate in traffic distribution
- Priority fields are ignored

### Fallback Mode (`"mode": "fallback"`)
- Uses `priority` field for server ordering (lower numbers = higher priority)
- Only uses next server when current fails
- Algorithm field is ignored
- Supports auto-priority assignment when `priority: 0`

## Important Implementation Details

### Thread Safety
- All selectors use mutexes for concurrent access
- Server state is managed with `serverDownUntil` timestamps
- Health recovery uses `sync.Once` to prevent race conditions

### Streaming Support
- Full streaming response support with `io.TeeReader` for statistics collection
- Debug mode shows real-time stream chunks without disrupting flow
- Token usage parsing from streaming responses

### Header Management
- Automatic `Authorization` header replacement with server tokens
- Hop-by-hop header filtering per RFC 2616
- Trusted proxy configuration for real client IP detection

### Balance Checking
Optional shell command execution for server balance monitoring with configurable intervals and thresholds.

## Testing Approach

Tests use table-driven patterns with mock implementations in `internal/testutil/`. The test suite covers:
- Algorithm correctness with deterministic scenarios
- Concurrent access safety
- Configuration validation
- Error handling paths
- Recovery mechanisms

When writing tests, use the existing mock patterns and ensure both success and failure scenarios are covered.
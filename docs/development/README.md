# Development Documentation

Complete guide for SIPREC Server development.

## Development Overview

- [Getting Started](GETTING_STARTED.md) - Development environment setup
- [Testing Guide](TESTING.md) - Testing procedures and frameworks
- [Error Handling](ERROR_HANDLING.md) - Error handling patterns
- [Contributing](CONTRIBUTING.md) - Contribution guidelines
- [API Development](API_DEVELOPMENT.md) - API development guide
- [Performance](PERFORMANCE.md) - Performance optimization

## Quick Development Setup

### Prerequisites

- Go 1.22+ 
- Docker (optional)
- Git

### Local Development

```bash
# Clone repository
git clone https://github.com/loreste/siprec.git
cd siprec

# Install dependencies  
go mod download

# Copy environment file
cp .env.development .env

# Configure STT provider
echo "STT_PROVIDER=mock" >> .env

# Run server
go run cmd/siprec/main.go
```

### Development Environment

```bash
# Development with auto-reload
go install github.com/cosmtrek/air@latest
air

# Debug mode
DEBUG=true go run cmd/siprec/main.go

# With profiling
PPROF_ENABLED=true go run cmd/siprec/main.go
```

## Code Structure

```
pkg/
├── audio/          # Audio processing pipeline
├── config/         # Configuration management
├── encryption/     # Encryption services
├── errors/         # Error handling
├── http/           # HTTP server and WebSocket
├── media/          # Media handling (RTP/codec)
├── messaging/      # Message queue integration
├── metrics/        # Monitoring and metrics
├── sip/            # SIP protocol implementation
├── stt/            # Speech-to-text providers
└── util/           # Utilities and helpers
```

## Development Workflow

1. **Create Feature Branch**
   ```bash
   git checkout -b feature/new-feature
   ```

2. **Develop & Test**
   ```bash
   # Run tests
   go test ./...
   
   # Run linting
   golangci-lint run
   ```

3. **Commit Changes**
   ```bash
   git add .
   git commit -m "Add new feature"
   ```

4. **Create Pull Request**
   - Push branch to GitHub
   - Create PR with description
   - Wait for review and CI

## Development Tools

### Recommended Tools

- **IDE**: VS Code with Go extension
- **Linting**: golangci-lint
- **Testing**: Built-in testing + testify
- **Debugging**: Delve (dlv)
- **Profiling**: pprof

### VS Code Configuration

```json
{
  "go.useLanguageServer": true,
  "go.lintTool": "golangci-lint",
  "go.testFlags": ["-v"],
  "go.buildTags": "integration"
}
```

## Code Standards

### Go Standards
- Follow effective Go guidelines
- Use gofmt for formatting
- Write comprehensive tests
- Document public APIs
- Handle errors explicitly

### Project Standards
- Use structured logging
- Implement graceful shutdown
- Add metrics for monitoring
- Follow security best practices
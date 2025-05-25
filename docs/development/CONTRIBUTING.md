# Contributing to SIPREC Server

Guidelines for contributing to the SIPREC Server project.

## Getting Started

### Prerequisites

- Go 1.22 or later
- Git
- GitHub account
- Basic understanding of SIP/RTP protocols

### Development Setup

1. **Fork the Repository**
   ```bash
   # Fork on GitHub, then clone your fork
   git clone https://github.com/YOUR_USERNAME/siprec.git
   cd siprec
   
   # Add upstream remote
   git remote add upstream https://github.com/loreste/siprec.git
   ```

2. **Set Up Development Environment**
   ```bash
   # Install dependencies
   go mod download
   
   # Install development tools
   go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
   go install github.com/cosmtrek/air@latest
   
   # Copy development config
   cp .env.development .env
   ```

3. **Verify Setup**
   ```bash
   # Run tests
   go test ./...
   
   # Run linting
   golangci-lint run
   
   # Start development server
   air
   ```

## Development Workflow

### 1. Create Feature Branch

```bash
# Sync with upstream
git fetch upstream
git checkout main
git merge upstream/main

# Create feature branch
git checkout -b feature/your-feature-name
```

### 2. Make Changes

- Write clean, readable code
- Follow Go conventions
- Add tests for new functionality
- Update documentation as needed

### 3. Test Your Changes

```bash
# Run unit tests
go test ./...

# Run integration tests
go test -tags=integration ./test/integration/...

# Run linting
golangci-lint run

# Test manually
./test_siprec_invite.sh
```

### 4. Commit Changes

```bash
# Stage changes
git add .

# Commit with descriptive message
git commit -m "feat: add support for new STT provider

- Implement Azure Speech Services integration
- Add configuration options
- Include comprehensive tests
- Update documentation"
```

### 5. Push and Create PR

```bash
# Push to your fork
git push origin feature/your-feature-name

# Create pull request on GitHub
```

## Contribution Guidelines

### Code Style

#### Go Style
- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` for formatting
- Use meaningful variable names
- Keep functions small and focused
- Handle errors explicitly

#### Project Conventions
```go
// Good: Structured logging
logger.WithFields(logrus.Fields{
    "session_id": sessionID,
    "method": "INVITE",
}).Info("Processing SIP request")

// Good: Error handling
if err != nil {
    return fmt.Errorf("failed to parse SDP: %w", err)
}

// Good: Context usage
func ProcessAudio(ctx context.Context, audio []byte) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
        // Process audio
    }
}
```

### Testing Requirements

#### Unit Tests
- Test all public functions
- Use table-driven tests when appropriate
- Mock external dependencies
- Aim for >80% code coverage

```go
func TestSIPHandler_HandleInvite(t *testing.T) {
    tests := []struct {
        name     string
        request  *sip.Request
        wantCode int
        wantErr  bool
    }{
        {
            name:     "valid SIPREC invite",
            request:  createValidInvite(),
            wantCode: 200,
            wantErr:  false,
        },
        // More test cases...
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

#### Integration Tests
- Test complete workflows
- Use real protocols (SIP/RTP)
- Verify end-to-end functionality

### Documentation

#### Code Documentation
```go
// AudioProcessor handles audio processing pipeline including
// VAD, noise reduction, and format conversion.
type AudioProcessor struct {
    // vad performs voice activity detection
    vad *VAD
    // noiseReducer applies noise reduction algorithms
    noiseReducer *NoiseReducer
}

// ProcessChunk processes a single audio chunk through the pipeline.
// It returns the processed audio data or an error if processing fails.
func (p *AudioProcessor) ProcessChunk(ctx context.Context, chunk []byte) ([]byte, error) {
    // Implementation...
}
```

#### README Updates
- Update feature lists
- Add configuration examples
- Include usage instructions

### Commit Message Format

Use conventional commits:

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes
- `refactor`: Code refactoring
- `test`: Test additions/changes
- `chore`: Maintenance tasks

Examples:
```
feat(stt): add Deepgram provider support

- Implement Deepgram API integration
- Add streaming transcription support
- Include configuration options
- Add comprehensive tests

Closes #123
```

## Review Process

### Pull Request Requirements

1. **Description**
   - Clear description of changes
   - Link to related issues
   - Breaking changes noted

2. **Testing**
   - All tests pass
   - New tests for new functionality
   - Manual testing performed

3. **Documentation**
   - Code comments updated
   - README updated if needed
   - API documentation current

### Review Criteria

Reviewers will check:
- Code quality and style
- Test coverage
- Performance impact
- Security considerations
- Documentation completeness

### Addressing Review Comments

```bash
# Make requested changes
# Stage and commit
git add .
git commit -m "address review comments: improve error handling"

# Push to update PR
git push origin feature/your-feature-name
```

## Issue Guidelines

### Bug Reports

Include:
- SIPREC server version
- Operating system
- Go version
- Reproduction steps
- Expected vs actual behavior
- Relevant logs

### Feature Requests

Include:
- Use case description
- Proposed solution
- Alternative solutions considered
- Willingness to implement

### Questions

- Check existing documentation first
- Search existing issues
- Provide context about your use case

## Release Process

### Version Numbering

Follow semantic versioning (semver):
- `MAJOR.MINOR.PATCH`
- MAJOR: Breaking changes
- MINOR: New features (backward compatible)
- PATCH: Bug fixes

### Release Preparation

1. Update CHANGELOG.md
2. Update VERSION file
3. Update documentation
4. Create release PR
5. Tag release after merge

## Community

### Communication

- GitHub Issues: Bug reports, feature requests
- GitHub Discussions: Questions, ideas
- Pull Requests: Code contributions

### Code of Conduct

- Be respectful and inclusive
- Focus on constructive feedback
- Help newcomers
- Follow project guidelines

## Recognition

Contributors are recognized in:
- CHANGELOG.md
- GitHub contributors list
- Release notes

Thank you for contributing to SIPREC Server!
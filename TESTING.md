# SIPREC Server Testing Documentation

This document describes the testing approach for the SIPREC server, with a focus on session redundancy, environment configuration, and end-to-end testing.

## Testing Overview

The SIPREC server uses multiple levels of testing to ensure quality and correctness:

1. **Unit Tests**: Test individual components in isolation (packages)
2. **Integration Tests**: Test interactions between components
3. **End-to-End Tests**: Test the complete system flow
4. **Environment Tests**: Validate configuration and environment setup
5. **Redundancy Tests**: Verify session resilience during failures
6. **Security Tests**: Verify TLS and SRTP implementation

## Getting Started with Testing

### Prerequisites

Before running tests, ensure you have:

1. A properly configured `.env` file (see `.env.example` for reference)
2. Go 1.22 or later installed
3. Required test dependencies installed

### Quick Start

Run all tests with:

```bash
make test
```

## Unit Tests

Unit tests are located in each package directory alongside the code they test. For example:

- `pkg/stt/*.go` → Unit tests in `pkg/stt/*_test.go`
- `pkg/sip/*.go` → Unit tests in `pkg/sip/*_test.go`

To run unit tests for a specific package:

```bash
make test-package PKG=./pkg/stt
```

### Writing Unit Tests

When writing unit tests:

1. Use table-driven tests for thorough coverage
2. Mock external dependencies
3. Test edge cases and error handling
4. Keep tests fast and independent

Example unit test structure:

```go
func TestMyFunction(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
        wantErr  bool
    }{
        {"valid input", "test data", "expected result", false},
        {"invalid input", "", "", true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := MyFunction(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("MyFunction() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if result != tt.expected {
                t.Errorf("MyFunction() = %v, want %v", result, tt.expected)
            }
        })
    }
}
```

## Integration Tests

Integration tests verify that components work together correctly. These tests are located in the `/test` directory.

To run integration tests:

```bash
go test ./test/...
```

### Key Integration Test Areas

1. **SIP Messaging**: Tests for SIP message processing
2. **RTP Handling**: Tests for RTP packet processing
3. **STT Integration**: Tests for speech-to-text processing
4. **Storage**: Tests for session storage and retrieval

## Environment Tests

Environment tests verify that the server can correctly load configuration from environment variables and .env files. These are crucial for ensuring the server starts correctly in different environments.

### Running Environment Tests

```bash
make env-test
```

Or run the environment check tool directly:

```bash
go run ./cmd/testenv/main.go
```

For validation of critical configurations:

```bash
go run ./cmd/testenv/main.go --validate
```

### Environment Test Components

1. **LoadEnvironment**: Finds and loads the .env file using multiple strategies
2. **GetEnvWithDefault**: Retrieves environment variables with fallback defaults
3. **RunEnvironmentCheck**: Performs full validation of environment configuration
4. **Directory Verification**: Checks required directories exist and are accessible

### Environment Test Architecture

The environment tests use a robust approach to configuration:

1. **Multiple Location Search**: Tries multiple strategies to find .env file
2. **Path Resolution**: Handles both relative and absolute paths 
3. **Default Fallbacks**: Provides sensible defaults when configuration is missing
4. **Configuration Validation**: Validates that critical config values are reasonable

## Session Redundancy Tests

Session redundancy tests verify the server's ability to maintain call continuity during network failures or server restarts, as specified in RFC 7245.

### Running Redundancy Tests

```bash
make test-redundancy
```

Or run specific redundancy tests:

```bash
go test ./test/e2e -run TestSiprecRedundancyFlow
```

### Redundancy Test Scenarios

1. **TestSiprecRedundancyFlow**: Tests the complete session recovery flow
   - Simulates initial call establishment
   - Creates failover metadata according to RFC 7245
   - Triggers network disconnect
   - Verifies session recovery works correctly
   - Checks call metadata continuity

2. **TestConcurrentSessionsRedundancy**: Tests redundancy with multiple concurrent sessions
   - Creates multiple simultaneous recording sessions
   - Simulates random session failures
   - Verifies all sessions can be recovered correctly
   - Ensures no cross-session interference

3. **TestStreamContinuityAfterFailover**: Tests RTP stream continuity during failover
   - Establishes RTP streams with sequence numbers
   - Simulates network failure during active streaming
   - Verifies stream continuity after reconnection
   - Validates no packet loss during recovery

### Redundancy Test Components

1. **CreateFailoverMetadata**: Generates RFC 7245 compliant metadata for session failover
2. **ParseFailoverMetadata**: Extracts session information from rs-metadata
3. **RecoverSession**: Creates a new session based on failover metadata
4. **ProcessStreamRecovery**: Restores stream information during recovery
5. **ValidateSessionContinuity**: Validates session integrity post-recovery

### Redundancy Test Architecture

The redundancy tests verify:

1. **Session Identity Preservation**: Original and recovered sessions maintain the same logical identity
2. **Participant Continuity**: All participants are correctly preserved during failover
3. **Dialog Replacement**: SIP dialog replacement occurs correctly per RFC 7245
4. **Media Continuity**: RTP streams continue with proper sequencing
5. **Metadata Preservation**: All required metadata is preserved across reconnections

## Security Tests

Security tests verify the TLS and SRTP implementation to ensure secure communication for both signaling and media.

### Running Security Tests

To test TLS functionality:

```bash
chmod +x test_tls.sh
./test_tls.sh
```

To test a complete secure session with both TLS and SRTP:

```bash
cd test_tls
go build -o mock_invite_tls mock_invite_tls.go
./mock_invite_tls
```

### Security Test Scenarios

1. **TLS Connection Tests**:
   - Verifies TLS server setup and certificate loading
   - Tests TLS handshake and connection establishment
   - Validates server identity through certificates
   - Tests various TLS client connection patterns

2. **SRTP Session Tests**:
   - Tests SRTP key generation and exchange
   - Verifies proper SDP crypto attribute generation
   - Tests SRTP packet encryption and decryption
   - Validates SRTP session setup and teardown

3. **Security Shutdown Tests**:
   - Ensures secure shutdown of TLS connections
   - Verifies proper cleanup of security resources
   - Tests graceful termination of encrypted sessions

### Security Test Components

1. **OpenSSL Client**: Tests basic TLS connectivity
2. **Custom TLS Test Client**: Tests SIP over TLS scenarios
3. **Mock SIP INVITE with SRTP**: Tests media encryption setup
4. **TLS Connection Verification**: Ensures ports are properly listening

### Best Practices for Security Tests

1. **Certificate Validation**: Always verify certificate validation works correctly
2. **Protocol Compliance**: Ensure TLS and SRTP implementations follow relevant RFCs
3. **Key Testing**: Test key generation, exchange, and management processes
4. **Connection Lifecycle**: Test the complete lifecycle of secure connections
5. **Error Handling**: Test how security components handle invalid data or attacks

## End-to-End Tests

End-to-end tests verify the complete SIPREC call flow from SIP signaling through RTP processing to speech recognition and transcription delivery. These tests are located in the `/test/e2e` directory.

### Running End-to-End Tests

To run all end-to-end tests:

```bash
make test-e2e
```

To run a specific end-to-end test:

```bash
make test-e2e-case TEST=TestSimulatedSiprecFullFlow
```

### End-to-End Test Flow

The end-to-end tests follow this general flow:

1. **Setup**: Initialize environment and components
2. **Call Establishment**: Create SIP dialogs and sessions
3. **Media Exchange**: Simulate RTP packet transmission 
4. **Process Audio**: Process audio through STT pipeline
5. **Generate Transcriptions**: Produce and validate transcriptions
6. **Simulate Failures** (for redundancy tests): Create network/server failures
7. **Recover Sessions**: Test session recovery mechanisms  
8. **Validate Results**: Verify expected outcomes
9. **Teardown**: Clean up resources

### End-to-End Test Components

The end-to-end tests use several key components:

1. **Mock SIP Client**: Simulates SIP signaling
2. **RTP Generator**: Creates realistic RTP packets
3. **Mock STT Provider**: Simulates transcription
4. **Session Manager**: Tracks call sessions
5. **Failure Injector**: Creates controlled failures for redundancy testing

### Test Scenarios

1. **Simple Transcription Flow**:
   - Focuses on the transcription pipeline
   - Verifies text extraction from audio

2. **Complete Call Flow**:
   - Tests the entire SIPREC signaling and media flow
   - Verifies all components work together

3. **Redundancy Flow**:
   - Tests session recovery after failures
   - Verifies RFC 7245 compliance

4. **Concurrent Call Handling**:
   - Tests system under multiple simultaneous calls
   - Verifies resource isolation between calls

### Best Practices for End-to-End Tests

1. **Keep Tests Independent**: Each test should run independently
2. **Use Timeouts**: Always use context timeouts to prevent tests from hanging
3. **Verify Resources**: Check that all resources are properly released
4. **Log Meaningful Information**: Include detailed logs for debugging
5. **Assert Real Behavior**: Verify correct behavior, not just function calls
6. **Control Test Environment**: Use controlled test data and configurations
7. **Test Both Success and Failure Paths**: Verify proper error handling

## Test Coverage

To generate a test coverage report:

```bash
make test-coverage
```

This will generate a coverage report in HTML format at `coverage.html`.

### Coverage Goals

- **Core Functionality**: Aim for >80% coverage
- **Error Handling**: Test all error paths
- **Configuration**: Test all configuration options
- **Session Redundancy**: 100% coverage of redundancy code

## Continuous Integration

All tests are automatically run in the CI pipeline when changes are pushed. The pipeline:

1. Runs environment validation
2. Runs unit tests for all packages
3. Runs integration tests
4. Runs redundancy tests
5. Runs end-to-end tests
6. Generates coverage reports

## Debugging Tests

### Common Issues and Solutions

1. **Timeouts**: Increase context timeout duration for slow tests
2. **Port Conflicts**: Use dynamic port allocation or ensure cleanup
3. **State Leakage**: Ensure tests clean up resources between runs
4. **Race Conditions**: Use the race detector (`-race` flag)
5. **Environment Dependencies**: Check that .env is properly loaded

### Debugging Tools

1. **Verbose Logging**: Use `t.Logf()` for detailed test logs
2. **Race Detector**: Run tests with `-race` flag
3. **Packet Capture**: Use Wireshark to inspect network traffic
4. **Memory Profiling**: Use pprof to find memory leaks
5. **Environment Tool**: Use the environment check tool to validate configuration

## Future Test Improvements

Future improvements to the testing framework include:

1. **Load Testing**: Testing the system under high call volume
2. **Chaos Testing**: Simulating network failures and component crashes
3. **Long-Running Tests**: Testing stability over extended periods
4. **Real Device Testing**: Testing with actual SIP phones and PBXs
5. **Security Testing**: Comprehensive security testing for TLS, SRTP, authentication, and authorization
6. **Benchmark Tests**: Performance testing for scaling

## Contributing New Tests

When contributing new tests:

1. Follow existing test patterns and naming conventions
2. Document the purpose and scenarios being tested
3. Include both positive and negative test cases
4. Ensure tests are deterministic and reliable
5. Add appropriate test coverage for new features
6. Verify tests pass in isolation and as part of the suite
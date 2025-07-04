# Changelog

All notable changes to the SIPREC server project will be documented in this file.

## [0.3.0] - 2025-07-01

### Added
- **Pause/Resume Control API**: Comprehensive REST API for controlling recording and transcription
  - Real-time pause and resume of individual sessions or all active sessions
  - Granular control: pause recording, transcription, or both independently
  - Secure API key authentication with configurable access control
  - Per-session and global pause/resume operations
  - Status monitoring with pause duration metrics
- **PII Detection & Redaction**: Automatic detection and redaction of personally identifiable information
  - Real-time detection of SSNs, credit card numbers, phone numbers, and email addresses
  - Advanced validation using Luhn algorithm for credit cards and SSN format validation
  - Configurable redaction with format preservation options
  - Transcription filtering with real-time PII redaction
  - Audio timeline marking for post-processing PII redaction
  - Thread-safe processing with race condition prevention
  - Integration with WebSocket and AMQP transcription delivery
- **Thread-Safe Stream Control**: Implementation of pausable I/O streams
  - PausableWriter for recording streams that drops data when paused
  - PausableReader for transcription streams that blocks reads when paused
  - Mutex-protected operations for concurrent safety
- **Enhanced Session Management**: Extended RTPForwarder with pause/resume state and PII audio marking
  - Thread-safe pause/resume methods with proper synchronization
  - Real-time status tracking with pause timestamps and duration
  - Seamless integration with existing STT providers
  - PII audio marker integration for timeline-based redaction
- **Monitoring and Metrics**: Comprehensive observability for pause/resume and PII operations
  - Prometheus metrics for pause/resume events and durations
  - PII detection metrics and performance monitoring
  - Structured logging with session context and operation details
  - Real-time status endpoints for monitoring active sessions
- **Configuration System**: Full environment variable and JSON configuration support
  - Configurable authentication, timeouts, and default behaviors
  - Optional maximum pause duration with auto-resume capability
  - Granular control over recording vs transcription pause behavior
  - Comprehensive PII detection configuration options

### Enhanced
- **API Architecture**: Extended HTTP server with new REST endpoints
- **Session Store**: Enhanced ShardedMap with Keys() method for session enumeration
- **Documentation**: Comprehensive API documentation with usage examples
- **Testing**: Complete unit and integration test coverage for all pause/resume functionality

### Technical
- **Thread Safety**: All operations use proper mutex synchronization
- **Performance**: Minimal overhead with immediate effect pause/resume operations
- **Reliability**: Non-blocking operations that don't affect other session functionality
- **Security**: Optional API key authentication with audit logging

## [0.2.0] - 2025-05-23

### Added
- **End-to-End Encryption**: Optional AES-256-GCM and ChaCha20-Poly1305 encryption for audio recordings and session metadata
- **Automatic Key Management**: Secure key generation, rotation, and storage with configurable intervals
- **Multiple Key Stores**: File-based persistent storage and memory-based volatile storage options
- **Encrypted File Format**: Custom `.siprec` format with encryption headers and chunk-based storage
- **Session Isolation**: Independent encryption contexts for each recording session
- **Key Rotation Service**: Automated background service for key rotation with configurable intervals
- **Comprehensive Encryption Tests**: Unit, integration, and performance tests for all encryption functionality
- **Encryption Documentation**: Complete guide with usage examples, security considerations, and best practices
- **Configuration Integration**: Full environment variable configuration with validation and defaults
- **Docker Integration**: Enhanced Docker containerization with multi-stage builds and testing
- **STT Provider Integration**: Comprehensive testing for Amazon Transcribe, Azure Speech, Google Speech, and Mock providers

### Enhanced
- **Security**: Added forward secrecy through automatic key rotation
- **Testing Suite**: Expanded with integration tests for STT providers and comprehensive unit tests
- **Documentation**: Updated with encryption capabilities and configuration options
- **Docker Support**: Production-ready multi-stage builds with security hardening
- **Build System**: Enhanced Makefile with cross-platform support and quality assurance

### Security
- **Authenticated Encryption**: AEAD modes prevent tampering with encrypted data
- **Secure Defaults**: Strong cryptographic parameters and secure key generation
- **Session Security**: Per-session encryption contexts with audit logging capabilities

## [0.1.0] - 2025-04-17

### Added
- Initial release with core SIPREC server functionality
- RFC 7245 compliant session redundancy implementation
- SIP Dialog replacement support via Replaces header
- TLS and SRTP security for signaling and media
- Concurrent session handling with sharded maps
- Memory optimization with RTP buffer pooling
- Production-ready Prometheus metrics
- Basic health check API
- Docker and docker-compose support
- Comprehensive testing suite

### Optimized
- Concurrent session handling using sharded maps in `pkg/sip/handler.go`
- Reduced lock contention for better scaling with many concurrent calls
- Efficient memory usage with buffer pooling for RTP packets
- Better performance under high load with concurrent processing

## [Unreleased]

### Added - Resource Optimization & Advanced Features
- **Advanced Resource Optimization**: Comprehensive memory and CPU optimization for 1000+ concurrent sessions
- **Memory Pool Management**: Intelligent buffer pooling reducing GC pressure by 50-70%
- **Worker Pool Architecture**: Dynamic goroutine scaling with category-specific pools
- **Session Caching**: LRU cache with TTL for frequently accessed sessions
- **Sharded Data Structures**: 64-shard maps for reduced lock contention
- **Enhanced Port Management**: RTP port reuse optimization with allocation statistics
- **Optimized Audio Processing**: Frame-based processing with multi-channel support
- **Advanced Session Manager**: High-performance session management with asynchronous operations

### Added - SIPREC Protocol Enhancements
- **Complete SIPREC Metadata Handling**: Full RFC 7865/7866 metadata schema support
- **Advanced Participant Management**: Complex participant configurations with roles and AORs
- **Stream Configuration**: Audio/video stream management with mixing support
- **Session State Transitions**: Pause/resume with sequence tracking and validation
- **Comprehensive Validation**: Detailed metadata validation with error reporting
- **Response Generation**: RFC-compliant multipart MIME response creation
- **Session Control Operations**: PauseRecording(), ResumeRecording(), and state management

### Added - Codec & Media Support
- **Enhanced Codec Support**: Full Opus and EVS codec implementations with decoding
- **Media Quality Metrics**: ITU-T G.107 E-model for MOS score calculation
- **Adaptive Bitrate Control**: Network-aware dynamic bitrate adjustment
- **Multi-Channel Audio**: Stereo enhancement, channel separation, and mixing
- **Audio Processing Pipeline**: Noise reduction, echo cancellation, and VAD

### Added - Infrastructure & Monitoring
- **Real-time transcription streaming to AMQP message queues**
- **Production-ready AMQP implementation** with fault tolerance and graceful degradation
- **Comprehensive Performance Metrics**: Memory, session, worker pool, and cache statistics
- **Resource Utilization Monitoring**: Real-time tracking of system resources
- **Health Check APIs**: Detailed health and performance monitoring endpoints
- **Enhanced Testing Suite**: Resource optimization and SIPREC metadata testing

### Optimized
- **Concurrent Session Handling**: 64-shard session storage for reduced lock contention
- **Memory Efficiency**: Object pooling and buffer reuse for zero-allocation hot paths
- **Audio Processing**: Frame-based processing reducing memory pressure
- **Network Operations**: Enhanced RTP port management with reuse tracking
- **Session Lookup**: Sub-millisecond session access through intelligent caching

### Fixed
- Fixed potential nil pointer dereference in SDP handler when no SDP content is provided
- Added generateDefaultSDP function to properly handle nil SDP cases
- Updated prepareSdpResponse function to handle empty or nil SDP inputs
- Simplified SDP generation code by consolidating to the generateSDPAdvanced function
- Resolved import cycles in resource optimization modules

### Changed
- **Refactored session management** for high-concurrency optimization
- **Enhanced SIPREC parser** with comprehensive metadata validation
- **Improved SDP handling** code for better maintainability and robustness
- **Updated project structure** to include resource optimization utilities
- **Enhanced documentation** with performance optimization guide
# Changelog

All notable changes to the SIPREC server project will be documented in this file.

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

### Added
- Real-time transcription streaming to AMQP message queues
- AMQPTranscriptionListener that implements the TranscriptionListener interface
- Automatic routing of transcriptions to AMQP when connection is available
- Production-ready AMQP implementation with timeouts, fault tolerance, and graceful degradation
- Message expiration to prevent queue overflow in AMQP
- Comprehensive AMQP guide with production best practices

### Fixed
- Fixed potential nil pointer dereference in SDP handler when no SDP content is provided
- Added generateDefaultSDP function to properly handle nil SDP cases
- Updated prepareSdpResponse function to handle empty or nil SDP inputs
- Simplified SDP generation code by consolidating to the generateSDPAdvanced function

### Changed
- Refactored SDP handling code for better maintainability and robustness
- Improved error logging for SDP parsing failures
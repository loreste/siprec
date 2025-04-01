# Changelog

All notable changes to the SIPREC server project will be documented in this file.

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
# Features

Comprehensive overview of SIPREC Server features and capabilities.

## Core Features

- [SIPREC Protocol Support](SIPREC.md) - Full RFC 7865/7866 compliance
- [Audio Processing Pipeline](AUDIO_PROCESSING_GUIDE.md) - Comprehensive audio enhancement with noise suppression, AGC, echo cancellation
- [Speech-to-Text Integration](STT_PROVIDERS.md) - Multiple STT provider support with automatic failover
- [Provider Health Monitoring](HEALTH_MONITORING.md) - Circuit breakers, health checks, and intelligent failover
- [PII Detection & Redaction](PII_DETECTION.md) - Automatic PII detection and redaction for transcriptions and audio
- [WebSocket API](WEBSOCKET_API.md) - Real-time streaming transcriptions
- [Message Queue Integration](AMQP_GUIDE.md) - AMQP/RabbitMQ integration with multi-endpoint support
- [Pause/Resume Control](PAUSE_RESUME_API.md) - Real-time session control via REST API

## Protocol Support

### SIP Features
- RFC 3261 compliant SIP stack
- UDP and TCP transport
- TLS support for secure signaling
- Automatic NAT traversal
- Session timers (RFC 4028)

### SIPREC Features
- RFC 7865 (SIPREC Protocol) compliant
- RFC 7866 (SIPREC Metadata) compliant
- Multiple participant support
- Selective recording
- Mid-call recording control

### Media Handling
- RTP/RTCP support
- Multiple codec support (PCMU, PCMA, G.722)
- Dynamic RTP port allocation
- SRTP support (optional)
- Silence suppression

## Audio Processing Pipeline

### Core Processing Features
1. **Noise Suppression**
   - Spectral subtraction with adaptive learning
   - Musical noise reduction
   - Voice Activity Detection (VAD) integration
   - Configurable suppression levels (0-1)

2. **Automatic Gain Control (AGC)**
   - Dynamic level adjustment
   - Attack/release control
   - Noise gate integration
   - Target level configuration

3. **Echo Cancellation**
   - Adaptive NLMS algorithm
   - Double-talk detection
   - Residual echo suppression
   - Comfort noise generation

4. **Multi-channel Recording**
   - Stereo and multi-channel support
   - Channel synchronization
   - Telephony separation (caller/callee)
   - Configurable mixing options

5. **Audio Fingerprinting**
   - Duplicate detection
   - Landmark-based algorithm
   - Configurable retention
   - Match confidence scoring

6. **Additional Processing**
   - Parametric equalizer with presets
   - Dynamic range compression
   - De-esser for sibilance reduction
   - High-pass filtering

## STT Provider Integration

### Supported Providers
- **OpenAI Whisper** – Local processing with high accuracy
- **Google Cloud Speech-to-Text** – Enhanced streaming with speaker diarization
- **Amazon Transcribe** – Real-time streaming with custom vocabulary
- **Azure Speech Services** – Multi-language support with confidence scoring
- **Deepgram** – WebSocket streaming with advanced features
- **ElevenLabs STT** – Timestamped transcripts with advanced formatting controls
- **Speechmatics** – Enterprise transcription with diarization and channel separation

### Advanced STT Features
- **Real-time Streaming**: Live transcription with WebSocket and gRPC protocols
- **Speaker Diarization**: Multi-speaker identification and separation
- **Circuit Breaker Pattern**: Automatic failover and service resilience
- **Retry Logic**: Exponential backoff with configurable retry policies
- **Custom Vocabulary**: Domain-specific term recognition and phrase hints
- **Multi-language Support**: Automatic language detection and switching
- **Performance Optimization**: Connection pooling and memory-efficient processing
- **Enhanced Metadata**: Word-level timestamps, confidence scores, and speaker tags

## Real-time Streaming

- WebSocket API for live transcriptions
- Server-sent events support
- Low latency streaming
- Automatic reconnection
- Binary and text frame support

## Scalability Features

- Horizontal scaling support
- Session sharding
- Connection pooling
- Resource pool management
- Graceful shutdown

## Monitoring & Observability

### Metrics & Monitoring
- Prometheus metrics integration
- Provider health metrics
- Audio quality metrics
- Circuit breaker status
- Performance profiling

### Health Management
- Automatic health checks
- Provider scoring system
- Circuit breaker patterns
- Intelligent failover
- Warning system with severity levels

### Observability Features
- Structured logging
- Debug endpoints
- Real-time status monitoring
- Configuration validation
- Runtime diagnostics

## Security Features

- TLS/SRTP support
- Rate limiting
- API authentication
- Encryption at rest
- Key rotation
- **PII Detection & Redaction** - Automatic detection and redaction of sensitive data (SSN, credit cards, phone numbers, emails)

## Integration Features

- RESTful API
- WebSocket API
- AMQP message queue
- Webhook notifications
- Custom middleware support

## Session Control Features

- **Real-time Pause/Resume** - API-driven control of recording and transcription
- **Granular Control** - Independent pause/resume for recording and transcription
- **Per-session Management** - Individual session control with status monitoring
- **Global Operations** - Bulk pause/resume for all active sessions
- **Authentication** - Secure API access with configurable authentication
- **Metrics & Monitoring** - Comprehensive metrics for pause/resume operations

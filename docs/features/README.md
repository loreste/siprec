# Features

Comprehensive overview of SIPREC Server features and capabilities.

## Core Features

- [SIPREC Protocol Support](SIPREC.md) - Full RFC 7865/7866 compliance
- [Audio Processing](AUDIO_PROCESSING.md) - Advanced audio processing pipeline
- [Speech-to-Text Integration](STT_PROVIDERS.md) - Multiple STT provider support
- [WebSocket API](WEBSOCKET_API.md) - Real-time streaming transcriptions
- [Message Queue Integration](AMQP_GUIDE.md) - AMQP/RabbitMQ integration

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

1. **Voice Activity Detection (VAD)**
   - Energy-based detection
   - Configurable thresholds
   - Reduces processing overhead

2. **Noise Reduction**
   - Spectral subtraction
   - Adaptive filtering
   - Preserves speech quality

3. **Audio Buffering**
   - Adaptive jitter buffer
   - Packet loss concealment
   - Reordering support

4. **Multi-channel Support**
   - Separate participant channels
   - Channel synchronization
   - Stereo output option

## STT Provider Integration

Supported providers:
- OpenAI Whisper
- Google Cloud Speech-to-Text
- Amazon Transcribe
- Azure Speech Services
- Deepgram

Features:
- Automatic failover
- Provider-specific optimizations
- Language detection
- Custom vocabulary support

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

- Prometheus metrics
- Health check endpoints
- Structured logging
- Performance profiling
- Debug endpoints

## Security Features

- TLS/SRTP support
- Rate limiting
- API authentication
- Encryption at rest
- Key rotation

## Integration Features

- RESTful API
- WebSocket API
- AMQP message queue
- Webhook notifications
- Custom middleware support
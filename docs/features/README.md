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

### Supported Providers
- **OpenAI Whisper** - Local processing with high accuracy
- **Google Cloud Speech-to-Text** - Enhanced streaming with speaker diarization
- **Amazon Transcribe** - Real-time streaming with custom vocabulary
- **Azure Speech Services** - Multi-language support with confidence scoring
- **Deepgram** - WebSocket streaming with advanced features

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
# Speech-to-Text Provider Integration

Comprehensive guide to STT provider integration and configuration with advanced streaming capabilities.

## Overview

The SIPREC server supports multiple speech-to-text providers with both basic and enhanced implementations. Enhanced providers include real-time streaming, speaker diarization, circuit breaker patterns, and advanced error handling.

## Provider Architecture

### Basic vs Enhanced Providers

| Feature | Basic Providers | Enhanced Providers |
|---------|----------------|-------------------|
| Streaming Protocol | HTTP API | WebSocket/gRPC |
| Speaker Diarization | ❌ | ✅ |
| Circuit Breaker | ❌ | ✅ |
| Retry Logic | ❌ | ✅ |
| Connection Management | ❌ | ✅ |
| Performance Metrics | ❌ | ✅ |
| Memory Optimization | ❌ | ✅ |

## Supported Providers

### Google Cloud Speech-to-Text (Enhanced)

**Features:**
- Real-time gRPC streaming with interim results
- Advanced speaker diarization with word-level speaker tags
- Multiple model variants optimized for different use cases
- Custom vocabulary and phrase hints
- Multi-language support with automatic detection
- Word-level timestamps and confidence scores
- Circuit breaker pattern for service resilience
- Automatic retry with exponential backoff

**Configuration:**
```bash
STT_PROVIDER=google-enhanced
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
GOOGLE_PROJECT_ID=your-gcp-project-id
```

**Enhanced Features:**
```go
config := &stt.GoogleConfig{
    Model:                    "latest_long",     // Best for accuracy
    LanguageCode:            "en-US",
    EnableSpeakerDiarization: true,              // Multi-speaker support
    EnableWordTimeOffsets:    true,              // Word-level timing
    EnableWordConfidence:     true,              // Per-word confidence
    InterimResults:           true,              // Real-time streaming
    UseEnhanced:              true,              // Premium models
    MaxSpeakerCount:          6,                 // Up to 6 speakers
    PhraseHints:             []string{"SIPREC", "VoIP"}, // Custom terms
}
```

**Available Models:**
- `latest_long` - Highest accuracy for longer audio segments
- `latest_short` - Optimized for short audio clips and low latency
- `command_and_search` - Voice commands and search queries
- `phone_call` - Optimized for telephony audio (8kHz)
- `video` - Multi-speaker video content
- `enhanced` - Premium enhanced models (billing required)

**Best for:**
- Real-time call transcription
- Multi-speaker conversations
- High-accuracy requirements
- Production environments requiring reliability

### Deepgram (Enhanced)

**Features:**
- WebSocket streaming with real-time transcription
- Advanced speaker diarization and speaker switching
- Voice activity detection and automatic endpointing
- Custom vocabulary and keyword boosting
- PII redaction and data privacy features
- Circuit breaker and failover to HTTP API
- Memory-efficient buffer pooling

**Configuration:**
```bash
STT_PROVIDER=deepgram-enhanced
DEEPGRAM_API_KEY=your-api-key
```

**Enhanced Features:**
```go
config := &stt.DeepgramConfig{
    Model:           "nova-2",                   // Latest model
    Language:        "en",
    Diarize:         true,                       // Speaker identification
    SmartFormat:     true,                       // Intelligent formatting
    InterimResults:  true,                       // Real-time results
    VAD:             true,                       // Voice activity detection
    Endpointing:     true,                       // Automatic segmentation
    Confidence:      true,                       // Confidence scores
    Timestamps:      true,                       // Word timestamps
    Keywords:        []string{"SIPREC", "VoIP"}, // Custom keywords
    ProfanityFilter: false,                      // Content filtering
    Redact:          []string{"pii"},            // PII redaction
}
```

**Available Models:**
- `nova-2` - Latest and most accurate model
- `nova` - Previous generation nova model
- `enhanced` - Enhanced accuracy model
- `base` - Standard accuracy model
- `general` - General purpose model

**Best for:**
- Real-time voice applications
- Privacy-sensitive environments (PII redaction)
- Cost-effective high-accuracy transcription
- Applications requiring low latency

### OpenAI Whisper

**Features:**
- State-of-the-art accuracy across 99 languages
- Automatic language detection
- Robust noise handling
- Punctuation and capitalization
- Local or API-based processing

**Configuration:**
```bash
STT_PROVIDER=openai
OPENAI_API_KEY=sk-your-api-key
OPENAI_MODEL=whisper-1
OPENAI_LANGUAGE=en  # Optional, auto-detected if not specified
```

**Best for:**
- Multi-language environments
- Noisy audio conditions
- Offline processing requirements
- High accuracy needs

### Amazon Transcribe

**Features:**
- Real-time streaming
- Custom vocabulary
- Automatic language identification
- Channel identification
- Content redaction

**Configuration:**
```bash
STT_PROVIDER=amazon
AWS_ACCESS_KEY_ID=your-access-key
AWS_SECRET_ACCESS_KEY=your-secret-key
AWS_REGION=us-east-1
TRANSCRIBE_LANGUAGE_CODE=en-US
```

**Best for:**
- AWS ecosystem integration
- Streaming applications
- Custom vocabulary requirements

### Azure Speech Services

**Features:**
- Real-time speech recognition
- Custom speech models
- Pronunciation assessment
- Intent recognition
- Translation capabilities

**Configuration:**
```bash
STT_PROVIDER=azure
AZURE_SPEECH_KEY=your-speech-key
AZURE_SPEECH_REGION=eastus
AZURE_LANGUAGE=en-US
```

**Best for:**
- Microsoft ecosystem integration
- Custom model training
- Multi-modal AI applications


manager.RegisterProvider(googleProvider)
manager.RegisterProvider(deepgramProvider)

// Use specific provider
err := manager.StreamToProvider(ctx, "google-enhanced", audioStream, callUUID)
```

### Failover Configuration

```go
// Primary provider with fallback
config := ProviderConfig{
    Primary:   "google-enhanced",
    Fallback:  []string{"deepgram-enhanced", "openai"},
    Timeout:   30 * time.Second,
    RetryCount: 3,
}
```

## Performance Optimization

### Memory Efficiency

Enhanced providers implement memory optimization features:

- **Buffer Pooling**: Reuse audio buffers to reduce garbage collection
- **Connection Pooling**: Efficient connection reuse for HTTP clients
- **Resource Cleanup**: Proper goroutine and channel cleanup

### CPU Efficiency

- **Circuit Breaker**: Avoid wasted CPU on failing services
- **Connection Management**: Persistent connections with keep-alive
- **Batch Processing**: Efficient audio chunk processing

### Thread Safety

- **Mutex Protection**: All shared state properly synchronized
- **Atomic Operations**: Lock-free counters and metrics
- **Goroutine Management**: Proper cancellation and cleanup

## Monitoring and Metrics

### Provider Metrics

```go
metrics := provider.GetMetrics()
fmt.Printf("Total Requests: %d\n", metrics.TotalRequests)
fmt.Printf("Success Rate: %.2f%%\n", 
    float64(metrics.SuccessfulRequests)/float64(metrics.TotalRequests)*100)
fmt.Printf("Average Latency: %v\n", metrics.AverageLatency)
fmt.Printf("Active Connections: %d\n", metrics.ActiveConnections)
```

### Health Monitoring

```go
// Check provider health
health := provider.HealthCheck()
if !health.Healthy {
    log.Printf("Provider unhealthy: %s", health.Error)
}

// Monitor circuit breaker state
if !provider.CircuitBreaker.CanExecute() {
    log.Println("Circuit breaker is open")
}
```

## Error Handling

### Circuit Breaker Pattern

Enhanced providers implement circuit breaker pattern for resilience:

- **Closed State**: Normal operation
- **Open State**: Service failure detected, requests blocked
- **Half-Open State**: Testing service recovery

### Retry Logic

Configurable retry policies with exponential backoff:

```go
retryConfig := &stt.RetryConfig{
    MaxRetries:      5,
    InitialDelay:    100 * time.Millisecond,
    MaxDelay:        5 * time.Second,
    BackoffFactor:   2.0,
    RetryableErrors: []string{"unavailable", "timeout", "rate_limit"},
}
```

### Graceful Degradation

- Automatic failover between providers
- Quality-based provider selection
- Fallback to simpler processing modes

## Provider Manager & Fallback

### Configuration

The provider manager coordinates registration, metrics, and fallback between vendors. The order passed to `NewProviderManager` controls failover behaviour.

```go
fallbackOrder := []string{"google-enhanced", "deepgram-enhanced", "openai"}
manager := stt.NewProviderManager(logger, "google-enhanced", fallbackOrder)

googleProvider := stt.NewGoogleProviderEnhanced(logger)
deepgramProvider := stt.NewDeepgramProviderEnhanced(logger)
openaiProvider := stt.NewOpenAIProvider(logger, transcriptionSvc, openaiConfig)

manager.RegisterProvider(googleProvider)
manager.RegisterProvider(deepgramProvider)
manager.RegisterProvider(openaiProvider)
```

### Fallback & Telemetry

- If the requested vendor fails, the manager tries the default vendor and then walks the `fallbackOrder` list.
- For seekable audio (files, buffers), the manager rewinds between attempts. Live RTP streams typically cannot be rewound and therefore stop after the first failure.
- Every attempt records metrics via `stt_requests_total` and `stt_latency_seconds`; successful fallbacks are also logged at `WARN` level with the vendor that ultimately succeeded.
- Use `SUPPORTED_VENDORS` and `STT_DEFAULT_VENDOR` environment variables to keep the runtime order in sync with configuration files.

### Usage

```go
ctx := context.Background()
audioStream := getAudioStream()
callUUID := "call-123"

if err := manager.StreamToProvider(ctx, "google-enhanced", audioStream, callUUID); err != nil {
    logger.WithError(err).Error("STT transcription failed")
}
```

## Best Practices

### Provider Selection

1. **Google Enhanced**: Best for production environments requiring high accuracy and real-time processing
2. **Deepgram Enhanced**: Optimal for cost-effective real-time transcription with advanced features
3. **OpenAI Whisper**: Ideal for multi-language support and offline processing
4. **Amazon Transcribe**: Suitable for AWS-integrated environments
5. **Azure Speech**: Best for Microsoft ecosystem integration

### Configuration Guidelines

1. **Authentication**: Use environment variables for API keys
2. **Timeouts**: Configure appropriate timeouts for your use case
3. **Languages**: Specify language codes for better accuracy
4. **Models**: Choose models based on audio characteristics
5. **Features**: Enable only required features to optimize performance

### Production Deployment

1. **Monitoring**: Implement comprehensive metrics and alerting
2. **Scaling**: Use horizontal scaling for high-volume deployments
3. **Caching**: Implement result caching for duplicate content
4. **Backup**: Configure multiple providers for redundancy
5. **Testing**: Perform load testing with realistic audio data

## Migration Guide

### From Basic to Enhanced Providers

1. **Update Configuration**: Change provider name to enhanced version
2. **Add Features**: Configure enhanced features as needed
3. **Update Callbacks**: Handle additional metadata fields
4. **Test Performance**: Validate improved performance and reliability

### Example Migration

```go
// Before: Basic Google provider
provider := stt.NewGoogleProvider(logger, transcriptionSvc)

// After: Enhanced Google provider
provider := stt.NewGoogleProviderEnhanced(logger)
config := stt.DefaultGoogleConfig()
config.EnableSpeakerDiarization = true
config.UseEnhanced = true
provider = stt.NewGoogleProviderEnhancedWithConfig(logger, config)
```

## Troubleshooting

### Common Issues

1. **Authentication Failures**: Verify API keys and credentials
2. **Network Timeouts**: Check connectivity and firewall rules
3. **Poor Accuracy**: Verify audio quality and language settings
4. **Rate Limiting**: Implement proper rate limiting and backoff
5. **Memory Issues**: Monitor memory usage and connection pooling

### Debug Mode

Enable debug logging for troubleshooting:

```go
logger.SetLevel(logrus.DebugLevel)
provider := stt.NewGoogleProviderEnhanced(logger)
```

### Performance Monitoring

Monitor key metrics for performance optimization:

- Request latency and throughput
- Error rates and retry counts
- Memory usage and connection counts
- Audio quality metrics

For detailed provider-specific documentation, see:
- [Deepgram Enhanced Guide](DEEPGRAM_ENHANCED.md)
- [Google Enhanced Guide](GOOGLE_ENHANCED.md)

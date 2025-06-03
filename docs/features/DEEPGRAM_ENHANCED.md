# Enhanced Deepgram Integration

This document covers the enhanced Deepgram speech-to-text integration with real-time WebSocket streaming, advanced features, and production-ready reliability patterns.

## Overview

The enhanced Deepgram provider offers:

- **Real-time WebSocket Streaming**: Live transcription with interim results
- **Advanced AI Features**: Speaker diarization, smart formatting, PII redaction
- **Production Reliability**: Circuit breaker, retry logic, connection pooling
- **Rich Metadata**: Word-level timestamps, confidence scores, speaker identification
- **Flexible Configuration**: Customizable models, languages, and performance tuning

## Features

### Core Streaming Capabilities

| Feature | Basic Provider | Enhanced Provider |
|---------|---------------|-------------------|
| HTTP API Support | ✅ | ✅ |
| WebSocket Streaming | ❌ | ✅ |
| Interim Results | ❌ | ✅ |
| Connection Pooling | ❌ | ✅ |
| Circuit Breaker | ❌ | ✅ |
| Retry Logic | ❌ | ✅ |
| Graceful Shutdown | ❌ | ✅ |

### Advanced AI Features

- **Speaker Diarization**: Identify different speakers in audio
- **Smart Formatting**: Automatic formatting of dates, times, numbers
- **PII Redaction**: Remove sensitive information (SSN, credit cards, etc.)
- **Custom Vocabulary**: Domain-specific keyword recognition
- **Voice Activity Detection**: Detect speech vs. silence
- **Utterance Detection**: Natural speech boundaries
- **Word-level Timestamps**: Precise timing for each word

### Supported Models

- **nova-2**: Latest and most accurate model
- **nova**: High-accuracy general model
- **enhanced**: Balanced accuracy and speed
- **base**: Fast processing for real-time use
- **custom models**: Your trained models

### Supported Languages

The enhanced provider supports all Deepgram languages including:
- English (en, en-US, en-GB, en-AU, etc.)
- Spanish (es, es-ES, es-MX, etc.)
- French (fr, fr-FR, fr-CA)
- German (de)
- Italian (it)
- Portuguese (pt, pt-BR)
- And many more...

## Configuration

### Basic Configuration

```go
// Default configuration
config := stt.DefaultDeepgramConfig()

// Or customize specific options
config := &stt.DeepgramConfig{
    Model:          \"nova-2\",
    Language:       \"en-US\",
    SampleRate:     16000,
    Channels:       1,
    Encoding:       \"linear16\",
    InterimResults: true,
    Diarize:        true,
}
```

### Configuration Options

#### Model and Language
```go
config := &stt.DeepgramConfig{
    Model:    \"nova-2\",      // Model selection
    Language: \"en-US\",       // Language variant
    Version:  \"latest\",      // Model version
    Tier:     \"nova\",        // Pricing tier
}
```

#### Audio Processing
```go
config := &stt.DeepgramConfig{
    Encoding:   \"linear16\",   // Audio encoding format
    SampleRate: 16000,         // Audio sample rate (Hz)
    Channels:   1,             // Number of audio channels
}
```

#### Advanced Features
```go
config := &stt.DeepgramConfig{
    // AI Features
    Punctuate:       true,     // Add punctuation
    Diarize:         true,     // Speaker identification
    SmartFormat:     true,     // Smart number/date formatting
    ProfanityFilter: false,    // Filter profanity
    
    // Real-time Features
    InterimResults:  true,     // Enable interim results
    VAD:             true,     // Voice activity detection
    Endpointing:     true,     // Auto endpoint detection
    
    // Metadata
    Confidence:      true,     // Include confidence scores
    Timestamps:      true,     // Word-level timestamps
    Utterances:      true,     // Utterance boundaries
    Paragraphs:      true,     // Paragraph detection
    Sentences:       true,     // Sentence boundaries
}
```

#### Custom Vocabulary
```go
config := &stt.DeepgramConfig{
    Keywords: []string{
        \"SIPREC\", \"WebRTC\", \"transcription\",
        \"custom-term-1\", \"domain-specific-word\",
    },
    CustomModel: \"your-custom-model-id\",
}
```

#### PII Redaction
```go
config := &stt.DeepgramConfig{
    Redact: []string{
        \"pci\",        // Credit card numbers
        \"ssn\",        // Social security numbers
        \"numbers\",    // All numbers
        \"emails\",     // Email addresses
    },
}
```

#### Performance Tuning
```go
config := &stt.DeepgramConfig{
    KeepAlive:     true,                    // Keep connections alive
    BufferSize:    8192,                    // Audio buffer size
    FlushInterval: 50 * time.Millisecond,   // Processing frequency
}
```

## Usage Examples

### Basic Usage

```go
// Create provider
logger := logrus.New()
provider := stt.NewDeepgramProviderEnhanced(logger)

// Set callback for results
provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
    fmt.Printf(\"Transcription: %s (Final: %t)\\n\", transcription, isFinal)
})

// Initialize
err := provider.Initialize()
if err != nil {
    log.Fatal(err)
}

// Stream audio
audioStream := getAudioStream() // Your audio source
ctx := context.Background()
err = provider.StreamToText(ctx, audioStream, \"call-123\")
```

### Real-time WebSocket Streaming

```go
// Configure for real-time processing
config := stt.DefaultDeepgramConfig()
config.InterimResults = true
config.FlushInterval = 25 * time.Millisecond
config.VAD = true

provider := stt.NewDeepgramProviderEnhancedWithConfig(logger, config)

// Handle both interim and final results
provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
    if isFinal {
        fmt.Printf(\"FINAL: %s\\n\", transcription)
    } else {
        fmt.Printf(\"INTERIM: %s\\n\", transcription)
    }
})
```

### Speaker Diarization

```go
config := &stt.DeepgramConfig{
    Model:      \"nova-2\",
    Diarize:    true,
    Utterances: true,
    Timestamps: true,
}

provider := stt.NewDeepgramProviderEnhancedWithConfig(logger, config)

provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
    if words, ok := metadata[\"words\"].([]interface{}); ok {
        for _, wordData := range words {
            if word, ok := wordData.(map[string]interface{}); ok {
                text := word[\"word\"].(string)
                speaker := word[\"speaker\"].(int)
                start := word[\"start\"].(float64)
                end := word[\"end\"].(float64)
                
                fmt.Printf(\"Speaker %d: %s (%.2f-%.2fs)\\n\", 
                    speaker, text, start, end)
            }
        }
    }
})
```

### Error Handling and Resilience

```go
// Configure retry behavior
retryConfig := &stt.RetryConfig{
    MaxRetries:      5,
    InitialDelay:    100 * time.Millisecond,
    MaxDelay:        5 * time.Second,
    BackoffFactor:   2.0,
    RetryableErrors: []string{\"connection\", \"timeout\"},
}

provider := stt.NewDeepgramProviderEnhanced(logger)
provider.SetRetryConfig(retryConfig)

// Circuit breaker is automatically configured
// It will open after repeated failures and recover automatically
```

### Provider Manager Integration

```go
// Create manager
manager := stt.NewProviderManager(logger, \"deepgram-enhanced\")

// Register enhanced Deepgram
deepgramProvider := stt.NewDeepgramProviderEnhanced(logger)
err := manager.RegisterProvider(deepgramProvider)

// Use through manager
audioStream := getAudioStream()
ctx := context.Background()
err = manager.StreamToProvider(ctx, \"deepgram-enhanced\", audioStream, \"call-123\")
```

## Metadata Structure

The enhanced provider returns rich metadata with each transcription:

```go
metadata := map[string]interface{}{
    // Provider information
    \"provider\":     \"deepgram\",
    \"model_name\":   \"nova-2\",
    \"model_uuid\":   \"model-uuid-123\",
    \"request_id\":   \"request-123\",
    
    // Quality metrics
    \"confidence\":   0.95,              // Overall confidence
    \"duration\":     2.5,               // Audio duration (seconds)
    \"start\":        0.0,               // Start time
    \"speech_final\": true,              // Speech finality
    
    // Word-level details
    \"words\": []map[string]interface{}{
        {
            \"word\":       \"Hello\",
            \"start\":      0.0,
            \"end\":        0.5,
            \"confidence\": 0.99,
            \"speaker\":    1,           // Speaker ID (if diarization enabled)
        },
        // ... more words
    },
    
    // Utterance information (if enabled)
    \"utterances\": []map[string]interface{}{
        {
            \"start\":      0.0,
            \"end\":        2.5,
            \"confidence\": 0.95,
            \"transcript\": \"Hello, how are you?\",
            \"speaker\":    1,
        },
    },
}
```

## Performance Optimization

### Connection Management

The enhanced provider includes:

- **Connection Pooling**: Reuses HTTP connections
- **WebSocket Keep-Alive**: Maintains persistent connections
- **Graceful Shutdown**: Properly closes all connections

### Buffer Management

```go
config := &stt.DeepgramConfig{
    BufferSize:    8192,                  // Larger buffer for high-volume
    FlushInterval: 50 * time.Millisecond, // More frequent processing
}
```

### Memory Efficiency

- Streaming processing (no full audio buffering)
- Connection pooling reduces overhead
- Automatic cleanup of inactive connections

## Monitoring and Metrics

### Active Connection Monitoring

```go
activeConnections := provider.GetActiveConnections()
fmt.Printf(\"Active WebSocket connections: %d\\n\", activeConnections)
```

### Configuration Inspection

```go
config := provider.GetConfig()
fmt.Printf(\"Current model: %s, Language: %s\\n\", config.Model, config.Language)
```

### Dynamic Configuration Updates

```go
newConfig := &stt.DeepgramConfig{
    Model:    \"enhanced\",
    Language: \"es\",
}

err := provider.UpdateConfig(newConfig)
if err != nil {
    log.Printf(\"Config update failed: %v\", err)
}
```

## Error Handling

### Common Error Scenarios

1. **Authentication Failures**
   ```
   Error: DEEPGRAM_API_KEY is not set in the environment
   ```
   **Solution**: Set the `DEEPGRAM_API_KEY` environment variable

2. **WebSocket Connection Failures**
   ```
   Error: failed to dial WebSocket: connection refused
   ```
   **Solution**: Provider automatically falls back to HTTP API

3. **Rate Limiting**
   ```
   Error: deepgram API returned status 429
   ```
   **Solution**: Circuit breaker automatically handles rate limits

4. **Invalid Configuration**
   ```
   Error: invalid model: invalid-model
   ```
   **Solution**: Use `provider.validateConfig()` to check configuration

### Circuit Breaker States

- **Closed**: Normal operation
- **Open**: Failures exceeded threshold, requests blocked
- **Half-Open**: Testing if service recovered

The circuit breaker automatically recovers after the configured timeout.

## Best Practices

### Production Deployment

1. **Environment Variables**
   ```bash
   export DEEPGRAM_API_KEY=\"your-api-key\"
   ```

2. **Configuration**
   ```go
   config := &stt.DeepgramConfig{
       Model:          \"nova-2\",        // Use latest model
       InterimResults: true,            // Enable real-time
       KeepAlive:      true,            // Maintain connections
       BufferSize:     8192,            // Optimize for volume
   }
   ```

3. **Error Handling**
   ```go
   provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
       if transcription == \"\" {
           // Handle events (utterance_end, errors, etc.)
           return
       }
       
       // Process transcription
       processTranscription(callUUID, transcription, isFinal, metadata)
   })
   ```

4. **Graceful Shutdown**
   ```go
   // During application shutdown
   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
   defer cancel()
   
   err := provider.Shutdown(ctx)
   if err != nil {
       log.Printf(\"Shutdown failed: %v\", err)
   }
   ```

### Security Considerations

1. **API Key Management**
   - Use environment variables, not hardcoded keys
   - Rotate API keys regularly
   - Use different keys for different environments

2. **PII Handling**
   ```go
   config.Redact = []string{\"pci\", \"ssn\", \"emails\"}
   ```

3. **TLS/SSL**
   - WebSocket connections use WSS (secure WebSocket)
   - HTTP API uses HTTPS by default

### Performance Tuning

1. **For High Volume**
   ```go
   config := &stt.DeepgramConfig{
       BufferSize:    16384,
       FlushInterval: 25 * time.Millisecond,
       KeepAlive:     true,
   }
   ```

2. **For Low Latency**
   ```go
   config := &stt.DeepgramConfig{
       Model:         \"base\",            // Faster model
       FlushInterval: 10 * time.Millisecond,
       VAD:           true,              // Immediate speech detection
   }
   ```

3. **For Accuracy**
   ```go
   config := &stt.DeepgramConfig{
       Model:       \"nova-2\",           // Most accurate model
       Diarize:     true,               // Speaker identification
       SmartFormat: true,               // Enhanced formatting
   }
   ```

## Troubleshooting

### Common Issues

1. **No Transcription Results**
   - Check audio format matches encoding configuration
   - Verify API key is valid
   - Check network connectivity

2. **Poor Accuracy**
   - Use `nova-2` model for best accuracy
   - Add domain-specific keywords
   - Check audio quality (sample rate, noise)

3. **High Latency**
   - Reduce `FlushInterval`
   - Use WebSocket instead of HTTP
   - Consider `base` model for speed

4. **Connection Issues**
   - Check firewall settings for WebSocket connections
   - Verify DNS resolution for `api.deepgram.com`
   - Monitor circuit breaker state

### Debugging

Enable debug logging:
```go
logger := logrus.New()
logger.SetLevel(logrus.DebugLevel)
provider := stt.NewDeepgramProviderEnhanced(logger)
```

Monitor provider state:
```go
fmt.Printf(\"Active connections: %d\\n\", provider.GetActiveConnections())
fmt.Printf(\"Configuration: %+v\\n\", provider.GetConfig())
```

## Migration from Basic Provider

### Step 1: Replace Provider Creation
```go
// Before
provider := stt.NewDeepgramProvider(logger)

// After
provider := stt.NewDeepgramProviderEnhanced(logger)
```

### Step 2: Update Callback Handling
```go
// Enhanced callback with rich metadata
provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
    // Access new metadata fields
    confidence := metadata[\"confidence\"].(float64)
    words := metadata[\"words\"]
    
    // Handle interim results
    if !isFinal {
        updateInterimDisplay(transcription)
    } else {
        saveFinalTranscription(transcription, metadata)
    }
})
```

### Step 3: Add Configuration (Optional)
```go
config := stt.DefaultDeepgramConfig()
config.Diarize = true
config.InterimResults = true

provider := stt.NewDeepgramProviderEnhancedWithConfig(logger, config)
```

### Step 4: Add Graceful Shutdown
```go
// During application shutdown
defer func() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    provider.Shutdown(ctx)
}()
```

## API Reference

See the [STT API Reference](../reference/STT_API.md) for complete method documentation.

## Examples

- [Basic Integration](../../examples/deepgram_integration_example.go)
- [Real-time Streaming](../../examples/realtime_transcription.go)
- [Speaker Diarization](../../examples/speaker_diarization.go)
- [Provider Manager Usage](../../examples/stt_provider_manager.go)
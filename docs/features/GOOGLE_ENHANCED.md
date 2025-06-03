# Enhanced Google Speech-to-Text Integration

This document covers the enhanced Google Speech-to-Text integration with real-time gRPC streaming, advanced AI features, and enterprise-grade reliability patterns.

## Overview

The enhanced Google Speech-to-Text provider offers:

- **Real-time gRPC Streaming**: Live transcription with interim results using Google's streaming API
- **Advanced AI Features**: Speaker diarization, automatic punctuation, word-level timestamps
- **Production Reliability**: Circuit breaker, retry logic, connection management
- **Rich Metadata**: Word-level confidence, speaker identification, timing information
- **Multi-language Support**: Primary and alternative language detection
- **Custom Models**: Enhanced models, phrase hints, speech adaptation

## Features

### Core Streaming Capabilities

| Feature | Basic Provider | Enhanced Provider |
|---------|---------------|-------------------|
| gRPC Streaming API | ✅ | ✅ |
| Interim Results | ✅ | ✅ |
| Connection Management | ❌ | ✅ |
| Circuit Breaker | ❌ | ✅ |
| Retry Logic | ❌ | ✅ |
| Performance Metrics | ❌ | ✅ |
| Graceful Shutdown | ❌ | ✅ |

### Advanced AI Features

- **Speaker Diarization**: Identify different speakers with configurable speaker counts
- **Automatic Punctuation**: Professional-grade punctuation and capitalization
- **Word-level Timestamps**: Precise timing for each word
- **Word Confidence Scores**: Individual confidence scores per word
- **Spoken Punctuation**: Recognition of spoken punctuation marks
- **Spoken Emojis**: Recognition of spoken emoji descriptions
- **Enhanced Models**: Premium models for higher accuracy
- **Multi-language Detection**: Automatic language detection and switching

### Supported Models

- **latest_long**: Optimized for long-form audio (best overall accuracy)
- **latest_short**: Optimized for short audio clips (fastest processing)
- **command_and_search**: Optimized for voice commands and search queries
- **phone_call**: Optimized for telephony audio (8kHz sample rate)
- **video**: Optimized for video content
- **enhanced**: Premium enhanced models (requires billing)

### Supported Languages

The enhanced provider supports all Google Speech-to-Text languages including:
- English (en-US, en-GB, en-AU, en-CA, etc.)
- Spanish (es-ES, es-MX, es-AR, etc.)
- French (fr-FR, fr-CA)
- German (de-DE)
- Italian (it-IT)
- Portuguese (pt-BR, pt-PT)
- Japanese (ja-JP)
- Korean (ko-KR)
- Chinese (zh-CN, zh-TW)
- And 100+ more languages...

## Configuration

### Basic Configuration

```go
// Default configuration
config := stt.DefaultGoogleConfig()

// Or customize specific options
config := &stt.GoogleConfig{
    Model:                    "latest_long",
    LanguageCode:            "en-US",
    SampleRateHertz:         16000,
    AudioChannelCount:       1,
    Encoding:                speechpb.RecognitionConfig_LINEAR16,
    EnableSpeakerDiarization: true,
    InterimResults:          true,
}
```

### Configuration Options

#### Model and Language
```go
config := &stt.GoogleConfig{
    Model:                "latest_long",     // Model selection
    LanguageCode:         "en-US",          // Primary language
    AlternativeLanguages: []string{"es-ES", "fr-FR"}, // Alternative languages
    UseEnhanced:          true,             // Premium enhanced models
}
```

#### Audio Processing
```go
config := &stt.GoogleConfig{
    Encoding:          speechpb.RecognitionConfig_LINEAR16, // Audio encoding
    SampleRateHertz:   16000,                              // Sample rate
    AudioChannelCount: 1,                                  // Channel count
    MaxAlternatives:   3,                                  // Alternative transcriptions
}
```

#### Speaker Diarization
```go
config := &stt.GoogleConfig{
    EnableSpeakerDiarization: true,   // Enable speaker identification
    MinSpeakerCount:         1,       // Minimum expected speakers
    MaxSpeakerCount:         6,       // Maximum expected speakers
    DiarizationSpeakerCount: 2,       // Expected number of speakers
}
```

#### Advanced Features
```go
config := &stt.GoogleConfig{
    // Punctuation and Formatting
    EnableAutomaticPunctuation: true,  // Add punctuation
    EnableSpokenPunctuation:    false, // Recognize spoken punctuation
    EnableSpokenEmojis:         false, // Recognize spoken emojis
    
    // Timing and Confidence
    EnableWordTimeOffsets:      true,  // Word-level timestamps
    EnableWordConfidence:       true,  // Word-level confidence
    
    // Quality Features
    EnableProfanityFilter:      false, // Filter profanity
    
    // Real-time Features
    InterimResults:             true,  // Enable interim results
    SingleUtterance:            false, // Multi-utterance mode
    VoiceActivityTimeout:       5 * time.Second, // VAD timeout
}
```

#### Custom Vocabulary and Adaptation
```go
config := &stt.GoogleConfig{
    // Phrase hints for better recognition
    PhraseHints: []string{
        "SIPREC", "Google Speech", "transcription",
        "your-custom-terms", "domain-specific-words",
    },
    BoostValue: 10.0,  // Boost strength (1.0-20.0)
    
    // Speech adaptation phrase sets (if configured in Google Cloud)
    AdaptationPhraseSets: []string{
        "projects/your-project/locations/global/phraseSets/your-phrase-set",
    },
}
```

#### Performance Tuning
```go
config := &stt.GoogleConfig{
    BufferSize:        8192,                  // Audio buffer size
    FlushInterval:     50 * time.Millisecond, // Processing frequency
    ConnectionTimeout: 30 * time.Second,      // gRPC connection timeout
    RequestTimeout:    5 * time.Minute,       // Total request timeout
}
```

#### Authentication
```go
config := &stt.GoogleConfig{
    CredentialsFile: "/path/to/service-account.json", // Service account file
    ProjectID:       "your-gcp-project-id",           // Google Cloud Project ID
}
```

## Usage Examples

### Basic Usage

```go
// Create provider
logger := logrus.New()
provider := stt.NewGoogleProviderEnhanced(logger)

// Set callback for results
provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
    confidence := metadata["confidence"].(float32)
    speakerTag := metadata["speaker_tag"].(int32)
    fmt.Printf("Transcription: %s (Final: %t, Speaker: %d, Confidence: %.2f)\\n", 
        transcription, isFinal, speakerTag, confidence)
})

// Initialize (requires GOOGLE_APPLICATION_CREDENTIALS environment variable)
err := provider.Initialize()
if err != nil {
    log.Fatal(err)
}

// Stream audio
audioStream := getAudioStream() // Your audio source
ctx := context.Background()
err = provider.StreamToText(ctx, audioStream, "call-123")
```

### Real-time Streaming with Interim Results

```go
// Configure for real-time processing
config := stt.DefaultGoogleConfig()
config.InterimResults = true
config.FlushInterval = 25 * time.Millisecond
config.VoiceActivityTimeout = 1 * time.Second

provider := stt.NewGoogleProviderEnhancedWithConfig(logger, config)

// Handle both interim and final results
provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
    if isFinal {
        fmt.Printf("FINAL: %s\\n", transcription)
    } else {
        fmt.Printf("INTERIM: %s\\n", transcription)
    }
})
```

### Speaker Diarization

```go
config := &stt.GoogleConfig{
    Model:                    "latest_long",  // Best for diarization
    EnableSpeakerDiarization: true,
    MinSpeakerCount:         1,
    MaxSpeakerCount:         6,
    EnableWordTimeOffsets:    true,
    EnableWordConfidence:     true,
}

provider := stt.NewGoogleProviderEnhancedWithConfig(logger, config)

provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
    if !isFinal {
        return
    }
    
    speakerTag := metadata["speaker_tag"].(int32)
    fmt.Printf("Speaker %d: %s\\n", speakerTag, transcription)
    
    // Access word-level speaker information
    if words, ok := metadata["words"].([]map[string]interface{}); ok {
        for _, wordData := range words {
            word := wordData["word"].(string)
            wordSpeaker := wordData["speaker_tag"].(int32)
            startTime := wordData["start_time"].(time.Time)
            endTime := wordData["end_time"].(time.Time)
            
            fmt.Printf("  %s: Speaker %d (%.2fs-%.2fs)\\n", 
                word, wordSpeaker, startTime.Unix(), endTime.Unix())
        }
    }
})
```

### Multi-language Recognition

```go
config := &stt.GoogleConfig{
    Model:                "latest_short",
    LanguageCode:         "en-US",                    // Primary language
    AlternativeLanguages: []string{"es-ES", "fr-FR"}, // Alternative languages
    MaxAlternatives:      2,                          // Get alternatives
    UseEnhanced:          true,
}

provider := stt.NewGoogleProviderEnhancedWithConfig(logger, config)

provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
    if isFinal {
        languageCode := metadata["language_code"].(string)
        confidence := metadata["confidence"].(float32)
        fmt.Printf("Language: %s, Text: %s (Confidence: %.2f)\\n", 
            languageCode, transcription, confidence)
    }
})
```

### Custom Vocabulary and Phrase Hints

```go
config := &stt.GoogleConfig{
    Model: "enhanced",
    PhraseHints: []string{
        "SIPREC", "WebRTC", "transcription",
        "your-company-name", "technical-terms",
    },
    BoostValue: 15.0,  // Strong boost for custom terms
    UseEnhanced: true,
}

provider := stt.NewGoogleProviderEnhancedWithConfig(logger, config)

// The model will have improved recognition for your custom terms
```

### Error Handling and Resilience

```go
// Configure retry behavior
retryConfig := &stt.RetryConfig{
    MaxRetries:      5,
    InitialDelay:    100 * time.Millisecond,
    MaxDelay:        5 * time.Second,
    BackoffFactor:   2.0,
    RetryableErrors: []string{"unavailable", "deadline", "resource exhausted"},
}

provider := stt.NewGoogleProviderEnhanced(logger)
provider.SetRetryConfig(retryConfig)

// Circuit breaker is automatically configured
// It will open after repeated failures and recover automatically
```

### Provider Manager Integration

```go
// Create manager
manager := stt.NewProviderManager(logger, "google-enhanced")

// Register enhanced Google provider
googleProvider := stt.NewGoogleProviderEnhanced(logger)
err := manager.RegisterProvider(googleProvider)

// Use through manager
audioStream := getAudioStream()
ctx := context.Background()
err = manager.StreamToProvider(ctx, "google-enhanced", audioStream, "call-123")
```

## Metadata Structure

The enhanced provider returns comprehensive metadata with each transcription:

```go
metadata := map[string]interface{}{
    // Provider information
    "provider":         "google",
    "confidence":       float32(0.95),       // Overall confidence
    "is_final":         true,                // Result finality
    "speaker_tag":      int32(1),            // Speaker ID
    "language_code":    "en-US",             // Detected language
    "result_end_time":  time.Time{},         // Result end timestamp
    
    // Word-level details
    "words": []map[string]interface{}{
        {
            "word":        "Hello",
            "confidence":  float32(0.99),    // Word confidence
            "speaker_tag": int32(1),         // Word speaker
            "start_time":  time.Time{},      // Word start time
            "end_time":    time.Time{},      // Word end time
        },
        // ... more words
    },
    
    // Billing information (if available)
    "total_billed_time": time.Duration{},    // Billed processing time
}
```

## Performance Optimization

### Connection Management

The enhanced provider includes:

- **gRPC Connection Pooling**: Efficient connection reuse
- **Streaming Lifecycle Management**: Proper stream handling
- **Graceful Shutdown**: Clean connection termination

### Buffer Management

```go
config := &stt.GoogleConfig{
    BufferSize:    16384,                 // Larger buffer for high-volume
    FlushInterval: 25 * time.Millisecond, // More frequent processing
}
```

### Model Selection for Performance

```go
// For speed (real-time applications)
config.Model = "latest_short"

// For accuracy (batch processing)
config.Model = "latest_long"

// For telephony (8kHz audio)
config.Model = "phone_call"
```

## Monitoring and Metrics

### Performance Metrics

```go
metrics := provider.GetMetrics()
fmt.Printf("Total Requests: %d\\n", metrics.TotalRequests)
fmt.Printf("Success Rate: %.2f%%\\n", 
    float64(metrics.SuccessfulRequests)/float64(metrics.TotalRequests)*100)
fmt.Printf("Active Connections: %d\\n", metrics.ActiveConnections)
fmt.Printf("Average Latency: %v\\n", metrics.AverageLatency)
```

### Connection Monitoring

```go
activeConnections := provider.GetActiveConnections()
fmt.Printf("Active gRPC connections: %d\\n", activeConnections)
```

### Configuration Inspection

```go
config := provider.GetConfig()
fmt.Printf("Current model: %s, Language: %s\\n", config.Model, config.LanguageCode)
fmt.Printf("Enhanced models: %t\\n", config.UseEnhanced)
fmt.Printf("Speaker diarization: %t\\n", config.EnableSpeakerDiarization)
```

### Dynamic Configuration Updates

```go
newConfig := &stt.GoogleConfig{
    Model:        "enhanced",
    LanguageCode: "es-ES",
    UseEnhanced:  true,
}

err := provider.UpdateConfig(newConfig)
if err != nil {
    log.Printf("Config update failed: %v", err)
}
```

## Error Handling

### Common Error Scenarios

1. **Authentication Failures**
   ```
   Error: could not find default credentials
   ```
   **Solution**: Set `GOOGLE_APPLICATION_CREDENTIALS` environment variable

2. **Permission Errors**
   ```
   Error: permission denied: Cloud Speech API not enabled
   ```
   **Solution**: Enable the Speech-to-Text API in Google Cloud Console

3. **Quota Exceeded**
   ```
   Error: resource exhausted: quota exceeded
   ```
   **Solution**: Circuit breaker handles this automatically with backoff

4. **Invalid Audio Format**
   ```
   Error: invalid argument: audio encoding mismatch
   ```
   **Solution**: Ensure audio format matches configuration

### Circuit Breaker States

- **Closed**: Normal operation
- **Open**: Failures exceeded threshold, requests blocked
- **Half-Open**: Testing if service recovered

The circuit breaker automatically recovers after the configured timeout.

### gRPC Status Codes

The provider handles various gRPC status codes:

- `UNAVAILABLE`: Service temporarily unavailable (retryable)
- `DEADLINE_EXCEEDED`: Request timeout (retryable)
- `RESOURCE_EXHAUSTED`: Quota exceeded (retryable with backoff)
- `INVALID_ARGUMENT`: Invalid request (not retryable)
- `PERMISSION_DENIED`: Authentication/authorization error (not retryable)

## Best Practices

### Authentication Setup

1. **Service Account Method** (Recommended for production)
   ```bash
   export GOOGLE_APPLICATION_CREDENTIALS="/path/to/service-account.json"
   ```

2. **Application Default Credentials** (For Google Cloud environments)
   ```go
   // Uses metadata service in GCE/GKE
   provider := stt.NewGoogleProviderEnhanced(logger)
   ```

3. **Explicit Credentials File**
   ```go
   config := &stt.GoogleConfig{
       CredentialsFile: "/path/to/service-account.json",
       ProjectID:       "your-gcp-project-id",
   }
   ```

### Production Configuration

```go
config := &stt.GoogleConfig{
    Model:                    "latest_long",      // Best accuracy
    UseEnhanced:              true,               // Premium models
    EnableSpeakerDiarization: true,               // Speaker identification
    EnableWordTimeOffsets:    true,               // Precise timing
    EnableWordConfidence:     true,               // Quality metrics
    InterimResults:           true,               // Real-time results
    MaxSpeakerCount:          6,                  // Reasonable limit
    BufferSize:               8192,               // Optimized buffer
    ConnectionTimeout:        60 * time.Second,   // Stable connections
    RequestTimeout:           10 * time.Minute,   // Long audio support
}
```

### Security Considerations

1. **Credential Security**
   - Use environment variables, not hardcoded credentials
   - Rotate service account keys regularly
   - Use different credentials for different environments

2. **Data Privacy**
   - Google processes audio according to their privacy policy
   - Consider data residency requirements
   - Use appropriate profanity filtering settings

3. **Network Security**
   - gRPC connections use TLS by default
   - Consider VPC peering for enhanced security

### Performance Tuning

1. **For High Accuracy**
   ```go
   config := &stt.GoogleConfig{
       Model:                    "latest_long",
       UseEnhanced:              true,
       EnableSpeakerDiarization: true,
       MaxAlternatives:          3,
   }
   ```

2. **For Low Latency**
   ```go
   config := &stt.GoogleConfig{
       Model:                "latest_short",
       FlushInterval:        10 * time.Millisecond,
       VoiceActivityTimeout: 1 * time.Second,
       BufferSize:           16384,
   }
   ```

3. **For High Volume**
   ```go
   config := &stt.GoogleConfig{
       BufferSize:        32768,
       ConnectionTimeout: 120 * time.Second,
       RequestTimeout:    30 * time.Minute,
   }
   ```

## Troubleshooting

### Common Issues

1. **No Transcription Results**
   - Check audio format matches encoding configuration
   - Verify API is enabled in Google Cloud Console
   - Check service account permissions

2. **Poor Accuracy**
   - Use `latest_long` model for best accuracy
   - Enable enhanced models (`UseEnhanced: true`)
   - Add domain-specific phrase hints
   - Check audio quality (sample rate, noise level)

3. **High Latency**
   - Use `latest_short` model for speed
   - Reduce `FlushInterval`
   - Increase `BufferSize`
   - Consider network latency to Google Cloud

4. **Authentication Issues**
   - Verify `GOOGLE_APPLICATION_CREDENTIALS` path
   - Check service account key validity
   - Ensure Speech-to-Text API is enabled

### Debugging

Enable debug logging:
```go
logger := logrus.New()
logger.SetLevel(logrus.DebugLevel)
provider := stt.NewGoogleProviderEnhanced(logger)
```

Monitor provider state:
```go
fmt.Printf("Active connections: %d\\n", provider.GetActiveConnections())
fmt.Printf("Metrics: %+v\\n", provider.GetMetrics())
fmt.Printf("Configuration: %+v\\n", provider.GetConfig())
```

Test authentication:
```bash
gcloud auth application-default print-access-token
```

## Migration from Basic Provider

### Step 1: Replace Provider Creation
```go
// Before
provider := stt.NewGoogleProvider(logger, transcriptionSvc)

// After
provider := stt.NewGoogleProviderEnhanced(logger)
```

### Step 2: Update Callback Handling
```go
// Enhanced callback with rich metadata
provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
    // Access new metadata fields
    confidence := metadata["confidence"].(float32)
    speakerTag := metadata["speaker_tag"].(int32)
    words := metadata["words"]
    
    // Handle word-level information
    if wordList, ok := words.([]map[string]interface{}); ok {
        for _, word := range wordList {
            // Process word-level data
        }
    }
})
```

### Step 3: Add Configuration (Optional)
```go
config := stt.DefaultGoogleConfig()
config.EnableSpeakerDiarization = true
config.UseEnhanced = true

provider := stt.NewGoogleProviderEnhancedWithConfig(logger, config)
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

- [Basic Integration](../../examples/google_integration_example.go)
- [Speaker Diarization](../../examples/speaker_diarization_google.go)
- [Multi-language Recognition](../../examples/multilingual_transcription.go)
- [Provider Manager Usage](../../examples/stt_provider_manager.go)
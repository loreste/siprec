# Speech-to-Text Provider Integration

Comprehensive guide to STT provider integration and configuration.

## Supported Providers

### OpenAI Whisper

**Features:**
- High accuracy across languages
- Automatic language detection
- Punctuation and formatting
- Speaker diarization (beta)

**Configuration:**
```bash
STT_PROVIDER=openai
OPENAI_API_KEY=sk-your-api-key
OPENAI_MODEL=whisper-1
OPENAI_LANGUAGE=en  # Optional
```

**Best for:**
- General purpose transcription
- Multi-language support
- High accuracy requirements

### Google Cloud Speech-to-Text

**Features:**
- Real-time streaming
- Custom vocabulary
- Multiple model variants
- Profanity filtering

**Configuration:**
```bash
STT_PROVIDER=google
GOOGLE_CREDENTIALS_PATH=/path/to/credentials.json
GOOGLE_LANGUAGE_CODE=en-US
GOOGLE_MODEL=latest_long
```

**Models:**
- `latest_long` - Best for long audio
- `latest_short` - Optimized for short audio
- `phone_call` - Optimized for telephony
- `video` - Multiple speaker scenarios

**Best for:**
- Real-time transcription
- Phone call audio
- Custom vocabulary needs

### Amazon Transcribe

**Features:**
- Custom vocabulary
- Speaker identification
- Channel identification
- Medical/Call center models

**Configuration:**
```bash
STT_PROVIDER=aws
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=your-access-key
AWS_SECRET_ACCESS_KEY=your-secret-key
AWS_LANGUAGE_CODE=en-US
```

**Best for:**
- AWS ecosystem integration
- Call center applications
- Medical transcription

### Azure Speech Services

**Features:**
- Real-time and batch
- Custom models
- Phrase lists
- Profanity masking

**Configuration:**
```bash
STT_PROVIDER=azure
AZURE_SPEECH_KEY=your-key
AZURE_SPEECH_REGION=eastus
AZURE_LANGUAGE=en-US
```

**Best for:**
- Microsoft ecosystem
- Custom model training
- Enterprise deployments

### Deepgram

**Features:**
- Low latency streaming
- Diarization
- Sentiment analysis
- Topic detection

**Configuration:**
```bash
STT_PROVIDER=deepgram
DEEPGRAM_API_KEY=your-api-key
DEEPGRAM_MODEL=general
DEEPGRAM_LANGUAGE=en
```

**Models:**
- `general` - General purpose
- `phone_call` - Optimized for calls
- `meeting` - Meeting transcription
- `voicemail` - Voicemail optimization

**Best for:**
- Ultra-low latency
- Real-time applications
- Advanced analytics

## Provider Selection Guide

### Accuracy Comparison

| Provider | General | Phone | Medical | Languages |
|----------|---------|-------|---------|-----------|
| OpenAI   | ★★★★★   | ★★★★  | ★★★★    | ★★★★★     |
| Google   | ★★★★☆   | ★★★★★ | ★★★☆    | ★★★★☆     |
| AWS      | ★★★★☆   | ★★★★★ | ★★★★★   | ★★★☆☆     |
| Azure    | ★★★★☆   | ★★★★☆ | ★★★★    | ★★★★☆     |
| Deepgram | ★★★★☆   | ★★★★☆ | ★★★☆    | ★★★☆☆     |

### Latency Comparison

| Provider | Streaming | Batch | Start Time |
|----------|-----------|-------|------------|
| OpenAI   | ✗         | ★★★☆  | ~2s        |
| Google   | ★★★★★     | ★★★★  | <500ms     |
| AWS      | ★★★★☆     | ★★★★  | ~1s        |
| Azure    | ★★★★☆     | ★★★★  | ~1s        |
| Deepgram | ★★★★★     | ★★★★★ | <300ms     |

### Cost Comparison (per minute)

| Provider | Standard | Premium | Volume Discount |
|----------|----------|---------|-----------------|
| OpenAI   | $0.006   | N/A     | No              |
| Google   | $0.004   | $0.009  | Yes             |
| AWS      | $0.004   | $0.005  | Yes             |
| Azure    | $0.004   | $0.008  | Yes             |
| Deepgram | $0.0035  | $0.008  | Yes             |

## Implementation Details

### Provider Interface

All providers implement a common interface:

```go
type STTProvider interface {
    StartTranscription(ctx context.Context, config TranscriptionConfig) error
    ProcessAudio(audio []byte) error
    GetTranscript() (*Transcript, error)
    Stop() error
}
```

### Audio Format Requirements

All providers expect:
- Format: PCM
- Encoding: 16-bit signed
- Sample Rate: 8kHz or 16kHz
- Channels: Mono

The server automatically converts incoming audio to the required format.

### Error Handling

Providers implement automatic retry with exponential backoff:

```go
retryConfig := RetryConfig{
    MaxRetries:     3,
    InitialBackoff: 100 * time.Millisecond,
    MaxBackoff:     5 * time.Second,
    Multiplier:     2.0,
}
```

### Failover Support

Configure multiple providers for automatic failover:

```bash
STT_PROVIDER=openai
STT_FALLBACK_PROVIDER=google
GOOGLE_CREDENTIALS_PATH=/path/to/credentials.json
```

## Advanced Features

### Custom Vocabulary

Supported by Google, AWS, and Azure:

```json
{
  "phrases": [
    "SIPREC",
    "RTP",
    "SIP"
  ],
  "boost": 20
}
```

### Speaker Diarization

Enable speaker separation:

```bash
# Google
GOOGLE_ENABLE_DIARIZATION=true
GOOGLE_MAX_SPEAKERS=4

# AWS
AWS_ENABLE_SPEAKER_IDENTIFICATION=true
AWS_MAX_SPEAKERS=4
```

### Language Detection

Automatic language detection:

```bash
# OpenAI
OPENAI_LANGUAGE=auto

# Google
GOOGLE_LANGUAGE_CODE=auto
GOOGLE_ALTERNATIVE_LANGUAGES=es,fr,de
```

## Monitoring

### Metrics

Each provider exposes metrics:

- `stt_transcription_duration` - Processing time
- `stt_transcription_errors` - Error count
- `stt_transcription_cost` - Estimated cost
- `stt_audio_processed_bytes` - Audio volume

### Logging

Detailed logging for debugging:

```
INFO: Starting transcription provider=openai session=abc123
DEBUG: Audio chunk received size=8192 session=abc123
INFO: Transcript received words=42 confidence=0.95 session=abc123
ERROR: Provider error provider=openai error="rate limit exceeded"
INFO: Failing over to provider=google session=abc123
```

## Best Practices

1. **Provider Selection**
   - Consider accuracy requirements
   - Evaluate latency constraints
   - Calculate cost projections
   - Test with your audio

2. **Configuration**
   - Use appropriate models
   - Configure language correctly
   - Enable relevant features
   - Set reasonable timeouts

3. **Error Handling**
   - Implement failover
   - Monitor error rates
   - Log detailed errors
   - Alert on failures

4. **Optimization**
   - Use VAD to reduce costs
   - Batch when possible
   - Cache results
   - Monitor usage

## Testing Providers

Test different providers:

```bash
# Test with sample audio
./test_stt_provider.sh -provider openai -audio sample.wav

# Compare providers
./compare_providers.sh -audio sample.wav

# Benchmark latency
./benchmark_stt.sh -provider all -iterations 100
```
# Audio Processing Pipeline

## Overview

The SIPREC server includes a comprehensive audio processing pipeline that enhances audio quality for improved transcription accuracy and recording clarity. The pipeline processes audio in real-time with minimal latency while maintaining high quality.

## Features

### 1. Noise Suppression

Advanced spectral subtraction-based noise suppression that adapts to the noise profile of the audio stream.

**Key Capabilities:**
- Adaptive noise floor learning
- Voice Activity Detection (VAD) integration
- Musical noise reduction
- Configurable suppression levels

**Configuration:**
```env
NOISE_SUPPRESSION_ENABLED=true
NOISE_SUPPRESSION_LEVEL=0.7      # 0.0 (off) to 1.0 (maximum)
VAD_THRESHOLD=0.3                 # VAD sensitivity
SPECTRAL_SUBTRACTION_FACTOR=2.0  # Noise subtraction aggressiveness
NOISE_FLOOR_DB=-60               # Noise floor in dB
HIGH_PASS_CUTOFF=80              # High-pass filter cutoff frequency
NS_ADAPTIVE_MODE=true            # Enable adaptive noise profiling
NS_LEARNING_DURATION=0.5         # Initial learning period in seconds
```

### 2. Automatic Gain Control (AGC)

Normalizes audio levels to ensure consistent volume across different speakers and recording conditions.

**Features:**
- Target level control
- Attack and release time configuration
- Noise gate integration
- Maximum/minimum gain limits

**Configuration:**
```env
AGC_ENABLED=true
AGC_TARGET_LEVEL=-18      # Target level in dB
AGC_MAX_GAIN=24          # Maximum gain in dB
AGC_MIN_GAIN=-12         # Minimum gain in dB
AGC_ATTACK_TIME=10        # Attack time in ms
AGC_RELEASE_TIME=100      # Release time in ms
NOISE_GATE_THRESHOLD=-50  # Noise gate threshold in dB
AGC_HOLD_TIME=50          # Hold time in ms
```

### 3. Echo Cancellation

Removes echo and feedback from audio streams, particularly useful in conference call scenarios.

**Capabilities:**
- Adaptive filter with configurable length
- Double-talk detection
- Nonlinear processing for residual echo
- Comfort noise generation

**Configuration:**
```env
ECHO_CANCELLATION_ENABLED=true
ECHO_FILTER_LENGTH=200           # Filter length in ms
ECHO_ADAPTATION_RATE=0.5         # Adaptation rate (0-1)
ECHO_NONLINEAR_PROCESSING=0.3    # Nonlinear processing strength
DOUBLE_TALK_THRESHOLD=0.5        # Double-talk detection threshold
COMFORT_NOISE_LEVEL=-60          # Comfort noise level in dB
RESIDUAL_SUPPRESSION=0.5         # Residual echo suppression
```

### 4. Multi-Channel Recording

Supports recording and processing of multi-channel audio with channel synchronization.

**Features:**
- Stereo and multi-channel support
- Channel synchronization
- Telephony channel separation (caller/callee)
- Configurable channel mixing

**Configuration:**
```env
MULTI_CHANNEL_ENABLED=true
CHANNEL_CONFIGURATION=stereo     # Options: mono, stereo, multi
CHANNEL_COUNT=2                  # Number of channels
TELEPHONY_SEPARATION=true        # Separate caller/callee channels
STEREO_WIDENING=false           # Enable stereo widening
STEREO_WIDTH=1.0                # Stereo width factor
CHANNEL_MIXING=false            # Enable channel mixing
MAX_CHANNEL_DESYNC=50ms         # Maximum channel desynchronization
CHANNEL_RESYNC_INTERVAL=1s      # Resynchronization interval
CALLER_CHANNEL_ID=0             # Caller channel identifier
CALLEE_CHANNEL_ID=1             # Callee channel identifier
```

### 5. Audio Fingerprinting

Generates unique fingerprints for audio segments to detect duplicates and track audio content.

**Capabilities:**
- Spectral peak detection
- Landmark pair generation
- Duplicate detection with confidence scoring
- Configurable retention and matching thresholds

**Configuration:**
```env
FINGERPRINTING_ENABLED=true
FP_WINDOW_SIZE=2048              # FFT window size
FP_HOP_SIZE=512                  # Hop size between windows
FP_PEAK_NEIGHBORHOOD=5           # Peak detection neighborhood
FP_PEAK_THRESHOLD=0.1            # Peak detection threshold
FP_MAX_PEAKS_PER_FRAME=5         # Maximum peaks per frame
FP_MIN_MATCH_SCORE=0.7           # Minimum match score
FP_MIN_MATCHING_HASHES=10        # Minimum matching hashes
FP_TIME_TOLERANCE_MS=100         # Time tolerance in ms
FP_MAX_STORED=10000              # Maximum stored fingerprints
FP_RETENTION_PERIOD=24h          # Fingerprint retention period
FP_BLOCK_DUPLICATES=false        # Block duplicate recordings
FP_ALERT_ON_DUPLICATES=true      # Alert on duplicate detection
FP_TAG_DUPLICATES=true           # Tag duplicate recordings
```

### 6. Dynamic Range Compression

Reduces the dynamic range of audio to improve clarity and consistency.

**Features:**
- Configurable threshold and ratio
- Soft knee compression
- Attack and release control
- Makeup gain

**Configuration:**
```env
COMPRESSION_ENABLED=false        # Disabled by default
COMPRESSION_THRESHOLD=-20        # Threshold in dB
COMPRESSION_RATIO=4              # Compression ratio
COMPRESSION_KNEE=2               # Knee width in dB
COMPRESSION_ATTACK=5             # Attack time in ms
COMPRESSION_RELEASE=50           # Release time in ms
COMPRESSION_MAKEUP_GAIN=0        # Makeup gain in dB
```

### 7. Parametric Equalizer

Adjustable frequency response for voice clarity enhancement.

**Features:**
- Multiple configurable bands
- Preset configurations
- Pre-amplification control
- Q factor adjustment

**Configuration:**
```env
EQ_ENABLED=true
EQ_PRE_AMP=0                    # Pre-amplification in dB
EQ_USE_PRESET=true              # Use preset configuration
EQ_PRESET_NAME=voice_clarity    # Preset name
```

**Available Presets:**
- `voice_clarity` - Optimized for speech clarity
- `telephone` - Simulates telephone frequency response
- `conference` - Optimized for conference calls
- `podcast` - Enhanced for podcast recording
- `flat` - No equalization

**Custom Band Configuration:**
```json
{
  "bands": [
    {"frequency": 100, "gain": -2.0, "q": 0.7},
    {"frequency": 300, "gain": 1.0, "q": 0.7},
    {"frequency": 1000, "gain": 0.5, "q": 0.7},
    {"frequency": 3000, "gain": 1.5, "q": 0.7},
    {"frequency": 8000, "gain": -1.0, "q": 0.7}
  ]
}
```

### 8. De-esser

Reduces sibilance and harsh 's' sounds in speech.

**Configuration:**
```env
DEESSER_ENABLED=true
DEESSER_FREQ_MIN=4000           # Minimum frequency in Hz
DEESSER_FREQ_MAX=10000          # Maximum frequency in Hz
DEESSER_THRESHOLD=-30           # Detection threshold in dB
DEESSER_REDUCTION=0.5           # Reduction amount (0-1)
```

## Processing Pipeline Architecture

The audio processing pipeline is organized as a series of stages, each operating on the audio stream:

```
Input Audio → Noise Suppression → Echo Cancellation → AGC → 
Equalizer → De-esser → Compression → Fingerprinting → Output
```

### Processing Order

1. **Noise Suppression** - Removes background noise
2. **Echo Cancellation** - Eliminates echo and feedback
3. **AGC** - Normalizes audio levels
4. **Equalizer** - Enhances frequency response
5. **De-esser** - Reduces sibilance
6. **Compression** - Controls dynamic range
7. **Fingerprinting** - Generates audio fingerprint

## Performance Considerations

### Latency

Each processing stage adds minimal latency:
- Noise Suppression: ~10-20ms
- Echo Cancellation: ~20-30ms
- AGC: <5ms
- Equalizer: <5ms
- Compression: <5ms
- Total pipeline latency: ~50-70ms

### CPU Usage

Processing overhead depends on enabled features:
- Minimal: VAD only (~5% CPU per stream)
- Moderate: Noise suppression + AGC (~15% CPU per stream)
- Full: All features enabled (~25-30% CPU per stream)

### Memory Usage

Buffer requirements per stream:
- Basic processing: ~2MB
- Full pipeline: ~5-10MB
- Fingerprinting cache: ~100MB shared

## Best Practices

### For Call Centers

Enable these features for optimal call center recording:
```env
NOISE_SUPPRESSION_ENABLED=true
NOISE_SUPPRESSION_LEVEL=0.6
AGC_ENABLED=true
AGC_TARGET_LEVEL=-18
ECHO_CANCELLATION_ENABLED=true
MULTI_CHANNEL_ENABLED=true
TELEPHONY_SEPARATION=true
```

### For Conference Calls

Recommended settings for conference recording:
```env
NOISE_SUPPRESSION_ENABLED=true
NOISE_SUPPRESSION_LEVEL=0.7
AGC_ENABLED=true
ECHO_CANCELLATION_ENABLED=true
EQ_PRESET_NAME=conference
COMPRESSION_ENABLED=true
```

### For High-Quality Recording

Settings for maximum quality:
```env
NOISE_SUPPRESSION_ENABLED=true
NOISE_SUPPRESSION_LEVEL=0.5
AGC_ENABLED=true
AGC_TARGET_LEVEL=-16
ECHO_CANCELLATION_ENABLED=false
EQ_PRESET_NAME=flat
COMPRESSION_ENABLED=false
MULTI_CHANNEL_ENABLED=true
```

## Monitoring and Metrics

The audio processing pipeline provides detailed metrics via Prometheus:

- `siprec_audio_processing_latency_seconds` - Processing latency by stage
- `siprec_vad_events_total` - Voice activity detection events
- `siprec_noise_reduction_level_db` - Current noise reduction level
- `siprec_audio_fingerprint_matches` - Fingerprint match counts
- `siprec_audio_quality_score` - Estimated audio quality score

## Troubleshooting

### Common Issues

**Issue: Distorted Audio**
- Reduce `NOISE_SUPPRESSION_LEVEL`
- Lower `AGC_MAX_GAIN`
- Disable compression

**Issue: Echo Not Removed**
- Increase `ECHO_FILTER_LENGTH`
- Adjust `ECHO_ADAPTATION_RATE`
- Enable `ECHO_NONLINEAR_PROCESSING`

**Issue: Volume Too Low**
- Increase `AGC_TARGET_LEVEL`
- Raise `AGC_MAX_GAIN`
- Add `COMPRESSION_MAKEUP_GAIN`

**Issue: Background Noise Audible**
- Increase `NOISE_SUPPRESSION_LEVEL`
- Lower `NOISE_FLOOR_DB`
- Enable `NS_ADAPTIVE_MODE`

### Debug Logging

Enable debug logging for audio processing:
```env
LOG_LEVEL=debug
AUDIO_DEBUG=true
AUDIO_SAVE_SAMPLES=true         # Save audio samples for analysis
AUDIO_SAMPLE_DIR=/tmp/audio     # Directory for audio samples
```

## API Integration

### Programmatic Control

The audio processing pipeline can be controlled via API:

```bash
# Get current audio processing settings
curl http://localhost:8080/api/audio/settings

# Update processing parameters
curl -X POST http://localhost:8080/api/audio/settings \
  -H "Content-Type: application/json" \
  -d '{
    "noise_suppression": {
      "enabled": true,
      "level": 0.8
    },
    "agc": {
      "enabled": true,
      "target_level": -20
    }
  }'

# Get audio quality metrics
curl http://localhost:8080/api/audio/metrics
```

### WebSocket Events

Real-time audio events are available via WebSocket:
```javascript
ws.on('audio.quality', (data) => {
  console.log('Audio quality:', data.score);
});

ws.on('audio.vad', (data) => {
  console.log('Voice activity:', data.active);
});

ws.on('audio.duplicate', (data) => {
  console.log('Duplicate detected:', data.fingerprint);
});
```

## Integration with STT Providers

The audio processing pipeline automatically optimizes audio for each STT provider:

- **Google STT**: Emphasis on noise suppression
- **Deepgram**: Balanced processing with AGC
- **OpenAI Whisper**: Minimal processing to preserve details
- **Speechmatics**: Echo cancellation focus
- **ElevenLabs**: Voice clarity enhancement

Provider-specific optimizations are applied automatically based on the active STT provider.
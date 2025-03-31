# Audio Processing for SIPREC Server

This package implements real-time audio processing capabilities for the SIPREC server. It provides:

1. **Voice Activity Detection (VAD)** - Identifies speech vs. silence, allowing for silence suppression and intelligent stream handling
2. **Noise Reduction** - Reduces background noise in the audio stream to improve quality and transcription accuracy
3. **Multi-channel Support** - Handles multiple audio channels for conference calls and mixes them when needed

## Architecture

The audio processing system is built with a flexible pipeline architecture:

```
┌────────────────┐    ┌────────────────┐    ┌────────────────┐
│                │    │                │    │                │
│ Channel Mixer  │───▶│ Noise Reducer  │───▶│ VAD Processor  │───▶ Output
│                │    │                │    │                │
└────────────────┘    └────────────────┘    └────────────────┘
```

## Components

### 1. Processing Manager (`manager.go`)

Central coordinator that:
- Initializes and configures all audio processors
- Orchestrates the processing pipeline
- Maintains processing statistics
- Provides a clean interface for external integration

### 2. Voice Activity Detector (`vad.go`)

Detects presence or absence of speech:
- Uses energy threshold-based detection with noise floor adaptation
- Implements hold time to prevent cutting off quiet speech
- Provides silence suppression with comfort noise generation

### 3. Noise Reducer (`noise_reduction.go`)

Reduces background noise:
- Implements a simple spectral subtraction algorithm
- Adapts to the noise floor automatically
- Preserves speech while attenuating noise

### 4. Channel Mixer (`multi_channel.go`)

Handles multi-channel audio:
- Supports mixing multiple audio channels into a single stream
- Maintains channel separation when needed
- Provides utilities for channel splitting and merging

## Configuration

Audio processing can be configured using the `ProcessingConfig` struct with these key parameters:

```go
type ProcessingConfig struct {
    // Voice Activity Detection
    EnableVAD           bool    // Enable/disable VAD
    VADThreshold        float64 // Energy threshold (0.0-1.0)
    VADHoldTime         int     // Hold frames after silence detection
    
    // Noise Reduction
    EnableNoiseReduction bool    // Enable/disable noise reduction
    NoiseFloor           float64 // Initial noise floor estimate
    NoiseAttenuationDB   float64 // Noise attenuation in dB
    
    // Multi-channel
    ChannelCount         int     // Number of audio channels
    MixChannels          bool    // Whether to mix channels
    
    // General settings
    SampleRate           int     // Audio sample rate (typically 8000Hz)
    FrameSize            int     // Frame size in samples
    BufferSize           int     // Processing buffer size
}
```

## Integration

To integrate audio processing in your RTP handling:

1. Create a processing manager with your desired configuration
2. Pass audio data through the manager
3. Use the processed output

Example:

```go
// Create audio processing manager
config := audio.ProcessingConfig{
    EnableVAD:            true,
    VADThreshold:         0.02,
    EnableNoiseReduction: true,
    // ... other settings
}
processor := audio.NewProcessingManager(config, logger)

// Process audio data
processedData, err := processor.ProcessAudio(rawAudioData)
if err != nil {
    // Handle error
}

// Use processed data
// ...
```

## Metrics

The processing manager tracks and provides these metrics:

- Packets processed count
- Voice vs. silence ratio
- Current noise floor
- Processing rate
- Error counts

Access them with `processor.GetStats()`.

## Testing

The package includes test tools to verify functionality:

- `test_audio.go` - Tests direct RTP packet handling with various pattern types
- RTP pattern testing with silence, tones, and noise samples
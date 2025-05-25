# Audio Processing Implementation

This document outlines the audio processing features implemented for the SIPREC server.

## Features Implemented

1. **Voice Activity Detection (VAD)**
   - Energy-based algorithm to detect speech vs. silence
   - Dynamic noise floor adaptation for robust detection in varying conditions
   - Hold time to prevent clipping of speech during short pauses
   - Silence suppression with comfort noise generation

2. **Noise Reduction**
   - Algorithm to reduce background noise while preserving speech
   - Noise profile estimation and adaptation
   - Configurable noise attenuation
   - Maintains speech intelligibility

3. **Multi-channel Support**
   - Handling of multi-channel audio for conference recordings
   - Channel mixing capabilities
   - Support for separate or mixed channel processing

## Architecture

The audio processing system uses a modular pipeline architecture:

```
┌─────────────────┐      ┌─────────────┐     ┌──────────────┐
│ RTP Packet      │ ──▶  │ Processor 1 │ ──▶ │ Processor 2  │ ──▶ ... ─▶ Processed Output
└─────────────────┘      └─────────────┘     └──────────────┘
```

### Components

1. **Audio Processing Manager** (`manager.go`)
   - Central coordinator that manages all audio processors
   - Provides a unified interface for the RTP handler
   - Maintains processing statistics and metrics
   - Configuration and runtime control

2. **Audio Processor Interface** (`types.go`)
   - Common interface for all audio processors
   - Methods for processing, resetting, and resource management
   - Configuration structures

3. **Audio Pipeline** (`types.go`)
   - Chains multiple processors in sequence
   - Handles data flow between processors
   - Provides a reader interface for easy integration

4. **Individual Processors**
   - Voice Activity Detector (`vad.go`)
   - Noise Reducer (`noise_reduction.go`)
   - Channel Mixer (`multi_channel.go`)

## Integration

The audio processing is integrated with the SIPREC server's media handling pipeline:

1. The RTP handler (`pkg/media/rtp.go`) initializes the audio processing manager
2. For each RTP packet:
   - Decrypt if SRTP is enabled
   - Extract audio payload
   - Pass through audio processing pipeline
   - Forward processed audio for recording and transcription

## Configuration

Audio processing is configured through the following settings:

```go
type AudioProcessingConfig struct {
    // General settings
    Enabled bool

    // Voice Activity Detection
    EnableVAD bool
    VADThreshold float64
    VADHoldTimeMs int

    // Noise Reduction
    EnableNoiseReduction bool
    NoiseReductionLevel float64

    // Multi-channel
    ChannelCount int
    MixChannels bool
}
```

These settings can be adjusted in the application configuration or environment variables.

## Performance Metrics

The audio processing system tracks the following metrics:

- Packets processed count
- Voice vs. silence ratio
- Noise floor level
- Processing rate (packets/second)
- Error counts

These metrics can be accessed via the `GetStats()` method and are logged periodically.

## Testing

The implementation includes the following testing components:

1. **Direct RTP Testing** (`test_audio.go`)
   - Validates audio processing using direct UDP packets
   - Tests silence detection, voice detection, and noise reduction
   - Provides visual feedback on VAD performance

2. **Pattern-based Testing**
   - Uses synthetic audio patterns (silence, tones, noise)
   - Verifies processor behavior with different input types
   - Tests transitions between speech and silence

## Future Enhancements

Potential future enhancements include:

1. **Advanced VAD Algorithms**
   - Machine learning-based VAD
   - Spectral feature extraction

2. **Enhanced Noise Reduction**
   - Spectral subtraction with psychoacoustic masking
   - Multi-band processing

3. **Audio Quality Metrics**
   - PESQ/POLQA scoring
   - SNR measurement

4. **Acoustic Echo Cancellation**
   - For handling echoes in recorded calls
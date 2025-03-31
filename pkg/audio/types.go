package audio

import (
	"io"
	"sync"
)

// ProcessingConfig holds configuration for audio processing
type ProcessingConfig struct {
	// Voice Activity Detection
	EnableVAD           bool    // Whether to enable voice activity detection
	VADThreshold        float64 // Energy threshold for voice activity detection (0.0-1.0)
	VADHoldTime         int     // How long to hold voice detection in frames after dropping below threshold
	
	// Noise Reduction
	EnableNoiseReduction bool    // Whether to enable noise reduction
	NoiseFloor           float64 // Noise floor level (0.0-1.0)
	NoiseAttenuationDB   float64 // Noise attenuation in dB
	
	// Multi-channel Support
	ChannelCount         int     // Number of audio channels
	MixChannels          bool    // Whether to mix multiple channels into a single output
	
	// General Processing
	SampleRate           int     // Audio sample rate (8000, 16000, 44100, etc.)
	FrameSize            int     // Frame size in samples
	BufferSize           int     // Buffer size for processing
}

// DefaultProcessingConfig returns default audio processing configuration
func DefaultProcessingConfig() ProcessingConfig {
	return ProcessingConfig{
		// Voice Activity Detection
		EnableVAD:           true,
		VADThreshold:        0.02,  // 2% of max energy
		VADHoldTime:         20,    // Hold voice detection for 20 frames (400ms at 50fps)
		
		// Noise Reduction
		EnableNoiseReduction: true,
		NoiseFloor:           0.01,  // 1% of max signal
		NoiseAttenuationDB:   12.0,  // 12dB attenuation
		
		// Multi-channel Support
		ChannelCount:         1,     // Default to mono
		MixChannels:          true,  // Mix channels by default
		
		// General Processing
		SampleRate:           8000,  // 8kHz (typical for telephony)
		FrameSize:            160,   // 20ms at 8kHz
		BufferSize:           1024,  // Processing buffer size
	}
}

// AudioProcessor is the interface that all audio processors must implement
type AudioProcessor interface {
	// Process takes raw audio data and returns processed audio
	Process(rawData []byte) ([]byte, error)
	
	// Reset resets the processor state
	Reset()
	
	// Close releases resources
	Close() error
}

// AudioFrame represents a frame of audio data
type AudioFrame struct {
	Data       []byte // Raw audio data
	IsVoice    bool   // Whether frame contains voice activity
	Energy     float64 // Energy level of frame
	ChannelID  int    // Channel identifier for multi-channel
	Timestamp  int64  // Timestamp in milliseconds
}

// AudioBufferPool helps reduce GC pressure when processing audio
var AudioBufferPool = sync.Pool{
	New: func() interface{} {
		// Default buffer size for audio processing (larger than typical RTP packet)
		return make([]byte, 2048)
	},
}

// AudioPipeline chains multiple audio processors
type AudioPipeline struct {
	Config     ProcessingConfig
	Processors []AudioProcessor
	FrameCh    chan AudioFrame
	StopCh     chan struct{}
}

// NewAudioPipeline creates a new audio processing pipeline
func NewAudioPipeline(config ProcessingConfig) *AudioPipeline {
	return &AudioPipeline{
		Config:     config,
		Processors: make([]AudioProcessor, 0),
		FrameCh:    make(chan AudioFrame, 100), // Buffer up to 100 frames
		StopCh:     make(chan struct{}),
	}
}

// AddProcessor adds a processor to the pipeline
func (p *AudioPipeline) AddProcessor(processor AudioProcessor) {
	p.Processors = append(p.Processors, processor)
}

// Process runs audio through the processing pipeline
func (p *AudioPipeline) Process(data []byte) ([]byte, error) {
	processed := data
	var err error
	
	for _, processor := range p.Processors {
		processed, err = processor.Process(processed)
		if err != nil {
			return nil, err
		}
	}
	
	return processed, nil
}

// Start begins processing audio from a reader
func (p *AudioPipeline) Start(reader io.Reader) io.Reader {
	pipeReader, pipeWriter := io.Pipe()
	
	go func() {
		defer pipeWriter.Close()
		buffer := make([]byte, p.Config.BufferSize)
		
		for {
			select {
			case <-p.StopCh:
				return
			default:
				n, err := reader.Read(buffer)
				if err != nil {
					if err != io.EOF {
						// Log error here
					}
					return
				}
				
				if n > 0 {
					processed, err := p.Process(buffer[:n])
					if err != nil {
						// Log error here
						continue
					}
					
					if len(processed) > 0 {
						_, err = pipeWriter.Write(processed)
						if err != nil {
							// Log error here
							return
						}
					}
				}
			}
		}
	}()
	
	return pipeReader
}

// Stop stops the audio pipeline
func (p *AudioPipeline) Stop() {
	close(p.StopCh)
	
	// Close all processors
	for _, processor := range p.Processors {
		processor.Close()
	}
}
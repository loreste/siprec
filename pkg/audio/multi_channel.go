package audio

import (
	"fmt"
	"sync"
)

// ChannelMixer handles multi-channel audio processing
type ChannelMixer struct {
	// Configuration
	channelCount      int     // Number of channels
	mixChannels       bool    // Whether to mix channels or keep separate
	bytesPerSample    int     // Bytes per sample (typically 2 for PCM16)
	
	// State
	channels          [][]byte // Separate channel buffers
	mixBuffer         []byte   // Buffer for mixed output
	
	// Lock for thread safety
	mu                sync.Mutex
}

// NewChannelMixer creates a new multi-channel audio processor
func NewChannelMixer(config ProcessingConfig) *ChannelMixer {
	channelCount := config.ChannelCount
	if channelCount < 1 {
		channelCount = 1 // Ensure at least mono
	}
	
	channels := make([][]byte, channelCount)
	for i := 0; i < channelCount; i++ {
		channels[i] = make([]byte, config.BufferSize)
	}
	
	return &ChannelMixer{
		channelCount:    channelCount,
		mixChannels:     config.MixChannels,
		bytesPerSample:  2, // Assume 16-bit PCM
		
		channels:        channels,
		mixBuffer:       make([]byte, config.BufferSize),
	}
}

// Process implements AudioProcessor interface
func (cm *ChannelMixer) Process(data []byte) ([]byte, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	// For single channel audio, no processing needed
	if cm.channelCount == 1 {
		return data, nil
	}
	
	// Interpret the data based on the channel format
	// This implementation assumes interleaved multi-channel audio
	// e.g., for stereo: [left0, right0, left1, right1, ...]
	
	if cm.mixChannels {
		// Mix all channels to mono
		return cm.mixToMono(data)
	} else {
		// Forward interleaved multi-channel audio as is
		return data, nil
	}
}

// mixToMono mixes multi-channel interleaved audio to mono
func (cm *ChannelMixer) mixToMono(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	
	samplesPerChannel := len(data) / (cm.bytesPerSample * cm.channelCount)
	outputSize := samplesPerChannel * cm.bytesPerSample
	
	// Ensure output buffer is large enough
	if len(cm.mixBuffer) < outputSize {
		cm.mixBuffer = make([]byte, outputSize)
	}
	
	// Process each sample
	for i := 0; i < samplesPerChannel; i++ {
		mixedSample := 0
		
		// Sum samples from all channels
		for ch := 0; ch < cm.channelCount; ch++ {
			sampleIndex := (i*cm.channelCount + ch) * cm.bytesPerSample
			if sampleIndex+1 < len(data) {
				// Convert bytes to 16-bit sample (little endian)
				sample := int16(data[sampleIndex]) | (int16(data[sampleIndex+1]) << 8)
				mixedSample += int(sample)
			}
		}
		
		// Average the sum
		mixedSample = mixedSample / cm.channelCount
		
		// Prevent overflow
		if mixedSample > 32767 {
			mixedSample = 32767
		} else if mixedSample < -32768 {
			mixedSample = -32768
		}
		
		// Convert back to bytes (little endian)
		outputIndex := i * cm.bytesPerSample
		cm.mixBuffer[outputIndex] = byte(mixedSample & 0xFF)
		cm.mixBuffer[outputIndex+1] = byte(mixedSample >> 8)
	}
	
	return cm.mixBuffer[:outputSize], nil
}

// SplitChannels separates interleaved multi-channel audio into separate channel buffers
func (cm *ChannelMixer) SplitChannels(data []byte) ([][]byte, error) {
	if cm.channelCount <= 1 {
		return [][]byte{data}, nil
	}
	
	samplesPerChannel := len(data) / (cm.bytesPerSample * cm.channelCount)
	
	// Ensure channel buffers are large enough
	for ch := 0; ch < cm.channelCount; ch++ {
		if len(cm.channels[ch]) < samplesPerChannel*cm.bytesPerSample {
			cm.channels[ch] = make([]byte, samplesPerChannel*cm.bytesPerSample)
		}
	}
	
	// Extract each channel
	for i := 0; i < samplesPerChannel; i++ {
		for ch := 0; ch < cm.channelCount; ch++ {
			inputIndex := (i*cm.channelCount + ch) * cm.bytesPerSample
			outputIndex := i * cm.bytesPerSample
			
			if inputIndex+1 < len(data) && outputIndex+1 < len(cm.channels[ch]) {
				// Copy sample
				cm.channels[ch][outputIndex] = data[inputIndex]
				cm.channels[ch][outputIndex+1] = data[inputIndex+1]
			}
		}
	}
	
	// Create result slices with the correct length
	result := make([][]byte, cm.channelCount)
	for ch := 0; ch < cm.channelCount; ch++ {
		result[ch] = cm.channels[ch][:samplesPerChannel*cm.bytesPerSample]
	}
	
	return result, nil
}

// MergeChannels combines separate channel buffers into interleaved multi-channel audio
func (cm *ChannelMixer) MergeChannels(channels [][]byte) ([]byte, error) {
	if len(channels) != cm.channelCount {
		return nil, fmt.Errorf("expected %d channels, got %d", cm.channelCount, len(channels))
	}
	
	// Find the shortest channel length
	minSamples := -1
	for _, channel := range channels {
		samples := len(channel) / cm.bytesPerSample
		if minSamples == -1 || samples < minSamples {
			minSamples = samples
		}
	}
	
	if minSamples <= 0 {
		return []byte{}, nil
	}
	
	// Create output buffer
	outputSize := minSamples * cm.bytesPerSample * cm.channelCount
	output := make([]byte, outputSize)
	
	// Interleave channels
	for i := 0; i < minSamples; i++ {
		for ch := 0; ch < cm.channelCount; ch++ {
			inputIndex := i * cm.bytesPerSample
			outputIndex := (i*cm.channelCount + ch) * cm.bytesPerSample
			
			if inputIndex+1 < len(channels[ch]) && outputIndex+1 < len(output) {
				// Copy sample
				output[outputIndex] = channels[ch][inputIndex]
				output[outputIndex+1] = channels[ch][inputIndex+1]
			}
		}
	}
	
	return output, nil
}

// Reset implements AudioProcessor interface
func (cm *ChannelMixer) Reset() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	// Clear channel buffers
	for i := range cm.channels {
		for j := range cm.channels[i] {
			cm.channels[i][j] = 0
		}
	}
}

// Close implements AudioProcessor interface
func (cm *ChannelMixer) Close() error {
	return nil
}

// SetMixChannels enables or disables channel mixing
func (cm *ChannelMixer) SetMixChannels(mix bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.mixChannels = mix
}

// GetChannelCount returns the number of channels
func (cm *ChannelMixer) GetChannelCount() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.channelCount
}
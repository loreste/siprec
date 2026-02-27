package audio

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// ChannelMixer handles multi-channel audio processing
type ChannelMixer struct {
	// Configuration
	channelCount   int  // Number of channels
	mixChannels    bool // Whether to mix channels or keep separate
	bytesPerSample int  // Bytes per sample (typically 2 for PCM16)

	// State
	channels  [][]byte // Separate channel buffers
	mixBuffer []byte   // Buffer for mixed output

	// Lock for thread safety
	mu sync.Mutex
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
		channelCount:   channelCount,
		mixChannels:    config.MixChannels,
		bytesPerSample: 2, // Assume 16-bit PCM

		channels:  channels,
		mixBuffer: make([]byte, config.BufferSize),
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

// StereoProcessor handles stereo-specific audio processing
type StereoProcessor struct {
	leftChannel  []byte
	rightChannel []byte
	stereoBuffer []byte
	bufferSize   int
	sampleRate   int

	// Stereo enhancement
	enableWidening bool
	wideningAmount float64

	// Balance control
	leftGain  float64
	rightGain float64

	mu sync.Mutex
}

// NewStereoProcessor creates a new stereo audio processor
func NewStereoProcessor(config ProcessingConfig) *StereoProcessor {
	bufferSize := config.BufferSize
	if bufferSize == 0 {
		bufferSize = 2048
	}

	return &StereoProcessor{
		leftChannel:    make([]byte, bufferSize),
		rightChannel:   make([]byte, bufferSize),
		stereoBuffer:   make([]byte, bufferSize*2), // Interleaved stereo
		bufferSize:     bufferSize,
		sampleRate:     config.SampleRate,
		enableWidening: false,
		wideningAmount: 0.5,
		leftGain:       1.0,
		rightGain:      1.0,
	}
}

// Process implements AudioProcessor interface for stereo processing
func (sp *StereoProcessor) Process(data []byte) ([]byte, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if len(data) == 0 {
		return data, nil
	}

	// Assume input is interleaved stereo (L, R, L, R, ...)
	samples := len(data) / 4 // 2 channels * 2 bytes per sample

	// Ensure buffers are large enough
	if samples*2 > len(sp.leftChannel) {
		sp.leftChannel = make([]byte, samples*2)
		sp.rightChannel = make([]byte, samples*2)
	}
	if len(data) > len(sp.stereoBuffer) {
		sp.stereoBuffer = make([]byte, len(data))
	}

	// Separate stereo channels
	err := sp.separateChannels(data, samples)
	if err != nil {
		return nil, err
	}

	// Apply processing to each channel
	sp.processChannels(samples)

	// Recombine channels
	return sp.combineChannels(samples), nil
}

// separateChannels splits interleaved stereo into separate left/right channels
func (sp *StereoProcessor) separateChannels(data []byte, samples int) error {
	for i := 0; i < samples; i++ {
		// Left channel (sample i*4 and i*4+1)
		if i*4+1 < len(data) && i*2+1 < len(sp.leftChannel) {
			sp.leftChannel[i*2] = data[i*4]
			sp.leftChannel[i*2+1] = data[i*4+1]
		}

		// Right channel (sample i*4+2 and i*4+3)
		if i*4+3 < len(data) && i*2+1 < len(sp.rightChannel) {
			sp.rightChannel[i*2] = data[i*4+2]
			sp.rightChannel[i*2+1] = data[i*4+3]
		}
	}
	return nil
}

// processChannels applies processing to individual channels
func (sp *StereoProcessor) processChannels(samples int) {
	for i := 0; i < samples; i++ {
		sampleIndex := i * 2
		if sampleIndex+1 >= len(sp.leftChannel) || sampleIndex+1 >= len(sp.rightChannel) {
			break
		}

		// Convert to 16-bit samples
		leftSample := int16(sp.leftChannel[sampleIndex]) | (int16(sp.leftChannel[sampleIndex+1]) << 8)
		rightSample := int16(sp.rightChannel[sampleIndex]) | (int16(sp.rightChannel[sampleIndex+1]) << 8)

		// Apply gain
		leftSample = int16(float64(leftSample) * sp.leftGain)
		rightSample = int16(float64(rightSample) * sp.rightGain)

		// Apply stereo widening if enabled
		if sp.enableWidening {
			leftFloat := float64(leftSample)
			rightFloat := float64(rightSample)

			// Simple stereo widening: enhance differences, preserve mono component
			mid := (leftFloat + rightFloat) / 2.0
			side := (leftFloat - rightFloat) / 2.0

			// Enhance the side signal
			side *= (1.0 + sp.wideningAmount)

			// Reconstruct left/right
			leftFloat = mid + side
			rightFloat = mid - side

			// Clamp to 16-bit range
			if leftFloat > 32767 {
				leftFloat = 32767
			} else if leftFloat < -32768 {
				leftFloat = -32768
			}
			if rightFloat > 32767 {
				rightFloat = 32767
			} else if rightFloat < -32768 {
				rightFloat = -32768
			}

			leftSample = int16(leftFloat)
			rightSample = int16(rightFloat)
		}

		// Convert back to bytes
		sp.leftChannel[sampleIndex] = byte(leftSample & 0xFF)
		sp.leftChannel[sampleIndex+1] = byte(leftSample >> 8)
		sp.rightChannel[sampleIndex] = byte(rightSample & 0xFF)
		sp.rightChannel[sampleIndex+1] = byte(rightSample >> 8)
	}
}

// combineChannels recombines left/right channels into interleaved stereo
func (sp *StereoProcessor) combineChannels(samples int) []byte {
	outputSize := samples * 4 // 2 channels * 2 bytes per sample
	if outputSize > len(sp.stereoBuffer) {
		sp.stereoBuffer = make([]byte, outputSize)
	}

	for i := 0; i < samples; i++ {
		// Copy left channel
		if i*2+1 < len(sp.leftChannel) && i*4+1 < len(sp.stereoBuffer) {
			sp.stereoBuffer[i*4] = sp.leftChannel[i*2]
			sp.stereoBuffer[i*4+1] = sp.leftChannel[i*2+1]
		}

		// Copy right channel
		if i*2+1 < len(sp.rightChannel) && i*4+3 < len(sp.stereoBuffer) {
			sp.stereoBuffer[i*4+2] = sp.rightChannel[i*2]
			sp.stereoBuffer[i*4+3] = sp.rightChannel[i*2+1]
		}
	}

	return sp.stereoBuffer[:outputSize]
}

// SetStereoWidening enables/disables stereo widening with specified amount
func (sp *StereoProcessor) SetStereoWidening(enable bool, amount float64) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	sp.enableWidening = enable
	if amount >= 0 && amount <= 2.0 {
		sp.wideningAmount = amount
	}
}

// SetChannelBalance sets the gain for left and right channels
func (sp *StereoProcessor) SetChannelBalance(leftGain, rightGain float64) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Clamp gains to reasonable range
	if leftGain < 0 {
		leftGain = 0
	} else if leftGain > 2.0 {
		leftGain = 2.0
	}

	if rightGain < 0 {
		rightGain = 0
	} else if rightGain > 2.0 {
		rightGain = 2.0
	}

	sp.leftGain = leftGain
	sp.rightGain = rightGain
}

// GetChannelData returns the current left and right channel data
func (sp *StereoProcessor) GetChannelData() ([]byte, []byte) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Return copies to avoid race conditions
	leftCopy := make([]byte, len(sp.leftChannel))
	rightCopy := make([]byte, len(sp.rightChannel))
	copy(leftCopy, sp.leftChannel)
	copy(rightCopy, sp.rightChannel)

	return leftCopy, rightCopy
}

// Reset implements AudioProcessor interface
func (sp *StereoProcessor) Reset() {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Clear buffers
	for i := range sp.leftChannel {
		sp.leftChannel[i] = 0
	}
	for i := range sp.rightChannel {
		sp.rightChannel[i] = 0
	}
	for i := range sp.stereoBuffer {
		sp.stereoBuffer[i] = 0
	}
}

// Close implements AudioProcessor interface
func (sp *StereoProcessor) Close() error {
	return nil
}

// MultiChannelRecorder handles recording of multiple audio channels simultaneously
type MultiChannelRecorder struct {
	channels     map[int]*ChannelRecorder // Channel ID to recorder mapping
	outputFormat string                   // "separate", "mixed", "interleaved"
	basePath     string                   // Base path for output files
	maxChannels  int                      // Maximum number of channels
	sampleRate   int                      // Sample rate

	// Synchronization
	masterClock   time.Time     // Master clock for sync
	syncTolerance time.Duration // Sync tolerance

	mu sync.RWMutex
}

// ChannelRecorder handles recording for a single channel
type ChannelRecorder struct {
	channelID     int
	file          *os.File
	buffer        []byte
	bufferIndex   int
	bufferSize    int
	lastTimestamp time.Time

	// Quality tracking - interface to avoid circular imports
	qualityMetrics interface{}

	mu sync.Mutex
}

// NewMultiChannelRecorder creates a new multi-channel recorder
func NewMultiChannelRecorder(basePath string, maxChannels int, sampleRate int, outputFormat string) *MultiChannelRecorder {
	return &MultiChannelRecorder{
		channels:      make(map[int]*ChannelRecorder),
		outputFormat:  outputFormat,
		basePath:      basePath,
		maxChannels:   maxChannels,
		sampleRate:    sampleRate,
		syncTolerance: time.Millisecond * 10, // 10ms sync tolerance
		masterClock:   time.Now(),
	}
}

// AddChannel adds a new channel for recording
func (mcr *MultiChannelRecorder) AddChannel(channelID int) error {
	mcr.mu.Lock()
	defer mcr.mu.Unlock()

	if len(mcr.channels) >= mcr.maxChannels {
		return fmt.Errorf("maximum number of channels (%d) reached", mcr.maxChannels)
	}

	if _, exists := mcr.channels[channelID]; exists {
		return fmt.Errorf("channel %d already exists", channelID)
	}

	// Create channel recorder
	recorder, err := mcr.createChannelRecorder(channelID)
	if err != nil {
		return err
	}

	mcr.channels[channelID] = recorder
	return nil
}

// createChannelRecorder creates a new channel recorder
func (mcr *MultiChannelRecorder) createChannelRecorder(channelID int) (*ChannelRecorder, error) {
	filename := fmt.Sprintf("%s_channel_%d.wav", mcr.basePath, channelID)
	file, err := os.Create(filename)
	if err != nil {
		return nil, err
	}

	// Write WAV header (simplified - 16-bit PCM)
	err = mcr.writeWAVHeader(file, mcr.sampleRate, 1) // Mono for individual channel
	if err != nil {
		file.Close()
		return nil, err
	}

	return &ChannelRecorder{
		channelID:      channelID,
		file:           file,
		buffer:         make([]byte, 4096),
		bufferSize:     4096,
		lastTimestamp:  time.Now(),
		qualityMetrics: nil, // Will be initialized when needed
	}, nil
}

// writeWAVHeader writes a basic WAV file header
func (mcr *MultiChannelRecorder) writeWAVHeader(file *os.File, sampleRate, channels int) error {
	// WAV header structure (simplified)
	header := []byte{
		// RIFF header
		'R', 'I', 'F', 'F',
		0, 0, 0, 0, // File size (placeholder)
		'W', 'A', 'V', 'E',

		// fmt chunk
		'f', 'm', 't', ' ',
		16, 0, 0, 0, // Chunk size
		1, 0, // Audio format (PCM)
		byte(channels), 0, // Number of channels

		// Sample rate (little endian)
		byte(sampleRate), byte(sampleRate >> 8), byte(sampleRate >> 16), byte(sampleRate >> 24),

		// Byte rate (SampleRate * NumChannels * BitsPerSample/8)
		0, 0, 0, 0, // Placeholder

		// Block align
		byte(channels * 2), 0, // NumChannels * BitsPerSample/8

		// Bits per sample
		16, 0,

		// data chunk
		'd', 'a', 't', 'a',
		0, 0, 0, 0, // Data size (placeholder)
	}

	// Calculate byte rate
	byteRate := sampleRate * channels * 2 // 16-bit samples
	header[28] = byte(byteRate)
	header[29] = byte(byteRate >> 8)
	header[30] = byte(byteRate >> 16)
	header[31] = byte(byteRate >> 24)

	_, err := file.Write(header)
	return err
}

// RecordSample records a sample for a specific channel
func (mcr *MultiChannelRecorder) RecordSample(channelID int, data []byte, timestamp time.Time) error {
	mcr.mu.RLock()
	recorder, exists := mcr.channels[channelID]
	mcr.mu.RUnlock()

	if !exists {
		return fmt.Errorf("channel %d not found", channelID)
	}

	return recorder.WriteSample(data, timestamp)
}

// WriteSample writes a sample to the channel recorder
func (cr *ChannelRecorder) WriteSample(data []byte, timestamp time.Time) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	// Update quality metrics (if available and has ProcessRTPPacket method)
	if cr.qualityMetrics != nil {
		if processor, ok := cr.qualityMetrics.(interface{ ProcessRTPPacket([]byte, time.Time) error }); ok {
			_ = processor.ProcessRTPPacket(data, timestamp)
		}
	}

	// Buffer the data
	if cr.bufferIndex+len(data) > cr.bufferSize {
		// Flush buffer to file
		err := cr.flushBuffer()
		if err != nil {
			return err
		}
	}

	// Add data to buffer
	copy(cr.buffer[cr.bufferIndex:], data)
	cr.bufferIndex += len(data)
	cr.lastTimestamp = timestamp

	return nil
}

// flushBuffer flushes the buffer to file
func (cr *ChannelRecorder) flushBuffer() error {
	if cr.bufferIndex == 0 {
		return nil
	}

	_, err := cr.file.Write(cr.buffer[:cr.bufferIndex])
	cr.bufferIndex = 0
	return err
}

// Close closes the multi-channel recorder
func (mcr *MultiChannelRecorder) Close() error {
	mcr.mu.Lock()
	defer mcr.mu.Unlock()

	var errs []error
	for _, recorder := range mcr.channels {
		if err := recorder.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing channels: %v", errs)
	}

	return nil
}

// Close closes the channel recorder
func (cr *ChannelRecorder) Close() error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	// Flush any remaining data
	err := cr.flushBuffer()
	if err != nil {
		return err
	}

	// Close file
	return cr.file.Close()
}

// GetChannelQualityMetrics returns quality metrics for a specific channel
func (mcr *MultiChannelRecorder) GetChannelQualityMetrics(channelID int) (interface{}, error) {
	mcr.mu.RLock()
	defer mcr.mu.RUnlock()

	recorder, exists := mcr.channels[channelID]
	if !exists {
		return nil, fmt.Errorf("channel %d not found", channelID)
	}

	return recorder.qualityMetrics, nil
}

// GetActiveChannels returns a list of active channel IDs
func (mcr *MultiChannelRecorder) GetActiveChannels() []int {
	mcr.mu.RLock()
	defer mcr.mu.RUnlock()

	channels := make([]int, 0, len(mcr.channels))
	for channelID := range mcr.channels {
		channels = append(channels, channelID)
	}

	return channels
}

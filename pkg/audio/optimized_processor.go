package audio

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"siprec-server/pkg/util"
)

// OptimizedAudioProcessor provides high-performance audio processing for concurrent sessions
type OptimizedAudioProcessor struct {
	// Resource pools
	resourcePool *util.ResourcePool
	workerPool   *util.WorkerPoolManager

	// Processing configuration
	maxConcurrent int32
	bufferSize    int

	// Statistics
	activeProcessors int32
	totalProcessed   int64
	processingTime   int64 // nanoseconds

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// AudioProcessingTask represents an audio processing job
type AudioProcessingTask struct {
	SessionID  string
	AudioData  []byte
	SampleRate int
	Channels   int
	Timestamp  time.Time
	Callback   func(processed []byte, err error)
}

// ProcessorStats provides audio processor statistics
type ProcessorStats struct {
	ActiveProcessors int32
	TotalProcessed   int64
	AverageTime      time.Duration
	BufferPoolHits   int64
	MemoryUsage      uint64
}

// NewOptimizedAudioProcessor creates a new optimized audio processor
func NewOptimizedAudioProcessor(maxConcurrent int, bufferSize int) *OptimizedAudioProcessor {
	if maxConcurrent <= 0 {
		maxConcurrent = runtime.NumCPU() * 2
	}
	if bufferSize <= 0 {
		bufferSize = 4096
	}

	ctx, cancel := context.WithCancel(context.Background())

	processor := &OptimizedAudioProcessor{
		resourcePool:  util.GetGlobalResourcePool(),
		workerPool:    util.GetGlobalWorkerPoolManager(),
		maxConcurrent: int32(maxConcurrent),
		bufferSize:    bufferSize,
		ctx:           ctx,
		cancel:        cancel,
	}

	return processor
}

// ProcessAudio processes audio data asynchronously with resource optimization
func (oap *OptimizedAudioProcessor) ProcessAudio(task *AudioProcessingTask) bool {
	// Check if we can accept more work
	current := atomic.LoadInt32(&oap.activeProcessors)
	if current >= oap.maxConcurrent {
		return false
	}

	// Submit processing task
	processingTask := func() {
		atomic.AddInt32(&oap.activeProcessors, 1)
		defer atomic.AddInt32(&oap.activeProcessors, -1)

		start := time.Now()
		processed, err := oap.processAudioData(task)
		duration := time.Since(start)

		// Update statistics
		atomic.AddInt64(&oap.totalProcessed, 1)
		atomic.AddInt64(&oap.processingTime, duration.Nanoseconds())

		// Execute callback
		if task.Callback != nil {
			task.Callback(processed, err)
		}
	}

	// Use audio-specific worker pool
	return oap.workerPool.SubmitTask("audio_processing", processingTask)
}

// processAudioData performs the actual audio processing with memory optimization
func (oap *OptimizedAudioProcessor) processAudioData(task *AudioProcessingTask) ([]byte, error) {
	// Get optimized buffer from pool
	outputSize := len(task.AudioData) * 2 // Estimate output size
	outputBuffer := oap.resourcePool.GetBuffer(outputSize)
	defer oap.resourcePool.PutBuffer(outputBuffer)

	// Reset buffer to ensure clean state
	outputBuffer = outputBuffer[:0]

	// Process audio based on configuration
	switch task.Channels {
	case 1:
		return oap.processMonoAudio(task.AudioData, task.SampleRate, outputBuffer)
	case 2:
		return oap.processStereoAudio(task.AudioData, task.SampleRate, outputBuffer)
	default:
		return oap.processMultiChannelAudio(task.AudioData, task.SampleRate, task.Channels, outputBuffer)
	}
}

// processMonoAudio optimized mono audio processing
func (oap *OptimizedAudioProcessor) processMonoAudio(data []byte, sampleRate int, outputBuffer []byte) ([]byte, error) {
	// Optimized mono processing with minimal allocations
	samplesPerFrame := sampleRate / 50 // 20ms frames
	bytesPerSample := 2                // 16-bit samples
	frameSize := samplesPerFrame * bytesPerSample

	// Process in frames to reduce memory pressure
	for offset := 0; offset < len(data); offset += frameSize {
		end := offset + frameSize
		if end > len(data) {
			end = len(data)
		}

		frame := data[offset:end]

		// Apply noise reduction and echo cancellation
		processedFrame := oap.applyNoiseReduction(frame)
		processedFrame = oap.applyEchoCancellation(processedFrame)

		// Append to output buffer
		outputBuffer = append(outputBuffer, processedFrame...)
	}

	// Return copy to prevent buffer reuse issues
	result := make([]byte, len(outputBuffer))
	copy(result, outputBuffer)
	return result, nil
}

// processStereoAudio optimized stereo audio processing
func (oap *OptimizedAudioProcessor) processStereoAudio(data []byte, sampleRate int, outputBuffer []byte) ([]byte, error) {
	// Stereo processing with channel separation optimization
	channelData := oap.separateChannels(data)

	// Process each channel independently
	leftProcessed, err := oap.processMonoAudio(channelData[0], sampleRate, nil)
	if err != nil {
		return nil, err
	}

	rightProcessed, err := oap.processMonoAudio(channelData[1], sampleRate, nil)
	if err != nil {
		return nil, err
	}

	// Interleave channels back together
	return oap.interleaveChannels([][]byte{leftProcessed, rightProcessed}), nil
}

// processMultiChannelAudio optimized multi-channel audio processing
func (oap *OptimizedAudioProcessor) processMultiChannelAudio(data []byte, sampleRate, channels int, outputBuffer []byte) ([]byte, error) {
	// Separate channels
	channelData := oap.separateMultipleChannels(data, channels)

	// Process each channel
	processedChannels := make([][]byte, channels)
	for i, channel := range channelData {
		processed, err := oap.processMonoAudio(channel, sampleRate, nil)
		if err != nil {
			return nil, err
		}
		processedChannels[i] = processed
	}

	// Recombine channels
	return oap.interleaveChannels(processedChannels), nil
}

// applyNoiseReduction applies basic noise reduction
func (oap *OptimizedAudioProcessor) applyNoiseReduction(data []byte) []byte {
	// Simplified noise reduction - in production this would be more sophisticated
	result := make([]byte, len(data))

	// Apply simple high-pass filter
	for i := 2; i < len(data)-2; i += 2 {
		// Convert to 16-bit sample
		sample := int16(data[i]) | int16(data[i+1])<<8

		// Simple noise gate
		if abs(int32(sample)) < 100 {
			sample = 0
		}

		// Convert back to bytes
		result[i] = byte(sample & 0xFF)
		result[i+1] = byte((sample >> 8) & 0xFF)
	}

	return result
}

// applyEchoCancellation applies basic echo cancellation
func (oap *OptimizedAudioProcessor) applyEchoCancellation(data []byte) []byte {
	// Simplified echo cancellation - in production this would use sophisticated algorithms
	result := make([]byte, len(data))
	copy(result, data)

	// Apply simple echo reduction
	for i := 4; i < len(result)-4; i += 2 {
		current := int16(result[i]) | int16(result[i+1])<<8
		delayed := int16(result[i-4]) | int16(result[i-3])<<8

		// Subtract delayed signal (simplified echo cancellation)
		processed := current - (delayed / 4)

		result[i] = byte(processed & 0xFF)
		result[i+1] = byte((processed >> 8) & 0xFF)
	}

	return result
}

// separateChannels separates stereo audio into left and right channels
func (oap *OptimizedAudioProcessor) separateChannels(data []byte) [][]byte {
	channels := make([][]byte, 2)
	channelSize := len(data) / 2

	channels[0] = make([]byte, channelSize) // Left channel
	channels[1] = make([]byte, channelSize) // Right channel

	// Separate interleaved stereo data
	for i := 0; i < len(data); i += 4 {
		// Left channel (samples 0, 1)
		channels[0][i/2] = data[i]
		channels[0][i/2+1] = data[i+1]

		// Right channel (samples 2, 3)
		channels[1][i/2] = data[i+2]
		channels[1][i/2+1] = data[i+3]
	}

	return channels
}

// separateMultipleChannels separates multi-channel audio
func (oap *OptimizedAudioProcessor) separateMultipleChannels(data []byte, numChannels int) [][]byte {
	channels := make([][]byte, numChannels)
	channelSize := len(data) / numChannels

	for ch := 0; ch < numChannels; ch++ {
		channels[ch] = make([]byte, channelSize)
	}

	bytesPerSample := 2 // 16-bit samples
	frameSize := numChannels * bytesPerSample

	for frame := 0; frame < len(data)/frameSize; frame++ {
		frameOffset := frame * frameSize

		for ch := 0; ch < numChannels; ch++ {
			sampleOffset := frameOffset + ch*bytesPerSample
			channelOffset := frame * bytesPerSample

			channels[ch][channelOffset] = data[sampleOffset]
			channels[ch][channelOffset+1] = data[sampleOffset+1]
		}
	}

	return channels
}

// interleaveChannels combines separate channel data back into interleaved format
func (oap *OptimizedAudioProcessor) interleaveChannels(channels [][]byte) []byte {
	if len(channels) == 0 {
		return nil
	}

	numChannels := len(channels)
	channelSize := len(channels[0])
	totalSize := numChannels * channelSize

	result := make([]byte, totalSize)
	bytesPerSample := 2

	for frame := 0; frame < channelSize/bytesPerSample; frame++ {
		frameOffset := frame * numChannels * bytesPerSample

		for ch := 0; ch < numChannels; ch++ {
			sampleOffset := frameOffset + ch*bytesPerSample
			channelOffset := frame * bytesPerSample

			result[sampleOffset] = channels[ch][channelOffset]
			result[sampleOffset+1] = channels[ch][channelOffset+1]
		}
	}

	return result
}

// GetStats returns processor statistics
func (oap *OptimizedAudioProcessor) GetStats() ProcessorStats {
	totalProcessed := atomic.LoadInt64(&oap.totalProcessed)
	totalTime := atomic.LoadInt64(&oap.processingTime)

	var avgTime time.Duration
	if totalProcessed > 0 {
		avgTime = time.Duration(totalTime / totalProcessed)
	}

	memStats := util.GetMemoryStats()

	return ProcessorStats{
		ActiveProcessors: atomic.LoadInt32(&oap.activeProcessors),
		TotalProcessed:   totalProcessed,
		AverageTime:      avgTime,
		MemoryUsage:      memStats.AllocatedBytes,
	}
}

// Shutdown gracefully shuts down the processor
func (oap *OptimizedAudioProcessor) Shutdown(timeout time.Duration) {
	oap.cancel()

	// Wait for active processors to complete
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&oap.activeProcessors) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// abs returns absolute value of int32
func abs(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}

// Global optimized audio processor instance
var globalOptimizedProcessor *OptimizedAudioProcessor
var processorOnce sync.Once

// GetGlobalOptimizedProcessor returns the global optimized audio processor
func GetGlobalOptimizedProcessor() *OptimizedAudioProcessor {
	processorOnce.Do(func() {
		globalOptimizedProcessor = NewOptimizedAudioProcessor(0, 0) // Use defaults
	})
	return globalOptimizedProcessor
}

package realtime

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

// AudioBuffer provides a thread-safe, memory-efficient circular buffer for audio data
type AudioBuffer struct {
	// Buffer configuration
	sampleRate    int
	channels      int
	bufferSizeMS  int
	maxBufferSize int

	// Circular buffer implementation
	buffer   []byte
	writePos int
	readPos  int
	size     int
	capacity int

	// Synchronization
	mutex     sync.RWMutex
	readCond  *sync.Cond
	writeCond *sync.Cond
	closed    bool

	// Memory optimization
	lastGC      time.Time
	gcThreshold int

	// Statistics
	bytesWritten int64
	bytesRead    int64
	overruns     int64
	underruns    int64
}

// NewAudioBuffer creates a new audio buffer with specified parameters
func NewAudioBuffer(bufferSizeMS, sampleRate, channels int) *AudioBuffer {
	// Calculate buffer size in bytes
	// Assuming 16-bit (2 bytes) samples
	bufferSizeBytes := (bufferSizeMS * sampleRate * channels * 2) / 1000

	// Ensure buffer size is reasonable
	if bufferSizeBytes < 1024 {
		bufferSizeBytes = 1024
	}
	if bufferSizeBytes > 1024*1024 { // Max 1MB
		bufferSizeBytes = 1024 * 1024
	}

	ab := &AudioBuffer{
		sampleRate:    sampleRate,
		channels:      channels,
		bufferSizeMS:  bufferSizeMS,
		maxBufferSize: bufferSizeBytes * 4, // Allow 4x expansion for buffering
		buffer:        make([]byte, bufferSizeBytes),
		capacity:      bufferSizeBytes,
		lastGC:        time.Now(),
		gcThreshold:   bufferSizeBytes / 4, // GC when 25% of buffer size has been processed
	}

	ab.readCond = sync.NewCond(&ab.mutex)
	ab.writeCond = sync.NewCond(&ab.mutex)

	return ab
}

// Write adds audio data to the buffer
func (ab *AudioBuffer) Write(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	ab.mutex.Lock()
	defer ab.mutex.Unlock()

	// Check if we have enough space
	availableSpace := ab.capacity - ab.size
	if len(data) > availableSpace {
		// Handle buffer overflow - drop oldest data
		ab.handleOverflow(len(data) - availableSpace)
		ab.overruns++
	}

	// Write data to circular buffer
	for i, b := range data {
		if ab.size < ab.capacity {
			ab.buffer[ab.writePos] = b
			ab.writePos = (ab.writePos + 1) % ab.capacity
			ab.size++
		} else {
			// Buffer is full, overwrite oldest data
			ab.buffer[ab.writePos] = b
			ab.writePos = (ab.writePos + 1) % ab.capacity
			ab.readPos = (ab.readPos + 1) % ab.capacity
		}

		// Update statistics
		if i == 0 {
			ab.bytesWritten += int64(len(data))
		}
	}

	// Signal waiting readers
	ab.readCond.Broadcast()

	// Perform periodic cleanup
	ab.periodicCleanup()

	return nil
}

// Read reads available audio data from the buffer
func (ab *AudioBuffer) Read() ([]byte, error) {
	ab.mutex.Lock()
	defer ab.mutex.Unlock()

	// Wait for data if buffer is empty
	for ab.size == 0 && !ab.closed {
		ab.readCond.Wait()
	}

	if ab.closed && ab.size == 0 {
		return nil, fmt.Errorf("buffer closed")
	}

	// Calculate how much to read (read all available data)
	readSize := ab.size
	if readSize == 0 {
		ab.underruns++
		return nil, fmt.Errorf("buffer underrun")
	}

	// Read data from circular buffer
	data := make([]byte, readSize)
	for i := 0; i < readSize; i++ {
		data[i] = ab.buffer[ab.readPos]
		ab.readPos = (ab.readPos + 1) % ab.capacity
	}

	ab.size -= readSize
	ab.bytesRead += int64(readSize)

	// Signal waiting writers
	ab.writeCond.Broadcast()

	return data, nil
}

// CanRead returns true if there's data available to read
func (ab *AudioBuffer) CanRead() bool {
	ab.mutex.RLock()
	defer ab.mutex.RUnlock()

	// Consider buffer ready when we have at least 100ms of audio
	minSize := (ab.bufferSizeMS * ab.sampleRate * ab.channels * 2) / 1000 / 10 // 10ms minimum
	return ab.size >= minSize
}

// handleOverflow handles buffer overflow by dropping oldest data
func (ab *AudioBuffer) handleOverflow(overflowBytes int) {
	// Move read position forward to make space
	dropBytes := overflowBytes
	if dropBytes > ab.size {
		dropBytes = ab.size
	}

	ab.readPos = (ab.readPos + dropBytes) % ab.capacity
	ab.size -= dropBytes
}

// periodicCleanup performs periodic memory optimization
func (ab *AudioBuffer) periodicCleanup() {
	now := time.Now()
	if now.Sub(ab.lastGC) > 30*time.Second && ab.bytesWritten > int64(ab.gcThreshold) {
		// Reset byte counters and optionally trigger GC
		ab.bytesWritten = 0
		ab.bytesRead = 0
		ab.lastGC = now

		// Force GC if memory usage is high
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		if m.HeapInuse > 50*1024*1024 { // 50MB threshold
			go runtime.GC()
		}
	}
}

// Cleanup performs cleanup operations
func (ab *AudioBuffer) Cleanup() {
	ab.mutex.Lock()
	defer ab.mutex.Unlock()

	// Reset buffer state
	ab.writePos = 0
	ab.readPos = 0
	ab.size = 0

	// Clear statistics
	ab.bytesWritten = 0
	ab.bytesRead = 0
	ab.overruns = 0
	ab.underruns = 0

	// Optionally shrink buffer if it grew too large
	if len(ab.buffer) > ab.maxBufferSize {
		ab.buffer = make([]byte, ab.capacity)
	}
}

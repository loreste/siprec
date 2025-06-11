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
	buffer        []byte
	writePos      int
	readPos       int
	size          int
	capacity      int
	
	// Synchronization
	mutex         sync.RWMutex
	readCond      *sync.Cond
	writeCond     *sync.Cond
	
	// Memory optimization
	lastGC        time.Time
	gcThreshold   int
	
	// Statistics
	bytesWritten  int64
	bytesRead     int64
	overruns      int64
	underruns     int64
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
	for ab.size == 0 {
		ab.readCond.Wait()
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

// ReadChunk reads a specific chunk size from the buffer
func (ab *AudioBuffer) ReadChunk(chunkSize int) ([]byte, error) {
	ab.mutex.Lock()
	defer ab.mutex.Unlock()
	
	// Wait for enough data
	for ab.size < chunkSize {
		ab.readCond.Wait()
	}
	
	// Read chunk from circular buffer
	data := make([]byte, chunkSize)
	for i := 0; i < chunkSize; i++ {
		data[i] = ab.buffer[ab.readPos]
		ab.readPos = (ab.readPos + 1) % ab.capacity
	}
	
	ab.size -= chunkSize
	ab.bytesRead += int64(chunkSize)
	
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

// Available returns the number of bytes available for reading
func (ab *AudioBuffer) Available() int {
	ab.mutex.RLock()
	defer ab.mutex.RUnlock()
	return ab.size
}

// Capacity returns the total buffer capacity
func (ab *AudioBuffer) Capacity() int {
	ab.mutex.RLock()
	defer ab.mutex.RUnlock()
	return ab.capacity
}

// Usage returns the buffer usage as a percentage
func (ab *AudioBuffer) Usage() float64 {
	ab.mutex.RLock()
	defer ab.mutex.RUnlock()
	return float64(ab.size) / float64(ab.capacity) * 100
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

// Resize changes the buffer capacity (use with caution)
func (ab *AudioBuffer) Resize(newSizeMS int) error {
	ab.mutex.Lock()
	defer ab.mutex.Unlock()
	
	// Calculate new buffer size
	newBufferSize := (newSizeMS * ab.sampleRate * ab.channels * 2) / 1000
	if newBufferSize < 1024 || newBufferSize > ab.maxBufferSize {
		return fmt.Errorf("invalid buffer size: %d", newBufferSize)
	}
	
	// Create new buffer
	newBuffer := make([]byte, newBufferSize)
	
	// Copy existing data if any
	if ab.size > 0 {
		copySize := ab.size
		if copySize > newBufferSize {
			copySize = newBufferSize
		}
		
		for i := 0; i < copySize; i++ {
			newBuffer[i] = ab.buffer[ab.readPos]
			ab.readPos = (ab.readPos + 1) % ab.capacity
		}
		
		ab.readPos = 0
		ab.writePos = copySize
		ab.size = copySize
	}
	
	// Replace buffer
	ab.buffer = newBuffer
	ab.capacity = newBufferSize
	ab.bufferSizeMS = newSizeMS
	
	return nil
}

// GetStats returns buffer statistics
func (ab *AudioBuffer) GetStats() map[string]interface{} {
	ab.mutex.RLock()
	defer ab.mutex.RUnlock()
	
	return map[string]interface{}{
		"capacity":         ab.capacity,
		"size":             ab.size,
		"usage_percent":    ab.Usage(),
		"bytes_written":    ab.bytesWritten,
		"bytes_read":       ab.bytesRead,
		"overruns":         ab.overruns,
		"underruns":        ab.underruns,
		"sample_rate":      ab.sampleRate,
		"channels":         ab.channels,
		"buffer_size_ms":   ab.bufferSizeMS,
		"read_pos":         ab.readPos,
		"write_pos":        ab.writePos,
	}
}

// Reset clears the buffer and resets all positions
func (ab *AudioBuffer) Reset() {
	ab.mutex.Lock()
	defer ab.mutex.Unlock()
	
	ab.writePos = 0
	ab.readPos = 0
	ab.size = 0
	
	// Signal all waiting goroutines
	ab.readCond.Broadcast()
	ab.writeCond.Broadcast()
}

// Close closes the buffer and releases resources
func (ab *AudioBuffer) Close() {
	ab.mutex.Lock()
	defer ab.mutex.Unlock()
	
	// Signal all waiting goroutines to exit
	ab.readCond.Broadcast()
	ab.writeCond.Broadcast()
	
	// Clear buffer
	ab.buffer = nil
	ab.capacity = 0
	ab.size = 0
}
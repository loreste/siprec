package media

import (
	"errors"
	"io"
	"sync"
)

// BufferedPipe provides a buffered pipe that decouples writer from reader.
// Unlike io.Pipe which blocks the writer when the reader is slow, this
// implementation uses a ring buffer to absorb temporary backpressure.
type BufferedPipe struct {
	mu       sync.Mutex
	cond     *sync.Cond
	buffer   []byte
	size     int // total buffer size
	start    int // read position
	end      int // write position
	count    int // bytes in buffer
	closed   bool
	writeErr error
	readErr  error
}

// BufferedPipeReader is the read half of a BufferedPipe
type BufferedPipeReader struct {
	pipe *BufferedPipe
}

// BufferedPipeWriter is the write half of a BufferedPipe
type BufferedPipeWriter struct {
	pipe *BufferedPipe
}

// NewBufferedPipe creates a new buffered pipe with the specified buffer size.
// bufferSize should be large enough to absorb 3-4 packets of audio (~60-80ms).
// For 8kHz 16-bit mono, that's about 1280-1920 bytes.
func NewBufferedPipe(bufferSize int) (*BufferedPipeReader, *BufferedPipeWriter) {
	if bufferSize <= 0 {
		bufferSize = 4096 // Default: ~250ms at 8kHz mono
	}

	p := &BufferedPipe{
		buffer: make([]byte, bufferSize),
		size:   bufferSize,
	}
	p.cond = sync.NewCond(&p.mu)

	return &BufferedPipeReader{pipe: p}, &BufferedPipeWriter{pipe: p}
}

// Read reads data from the pipe, blocking if no data is available.
func (r *BufferedPipeReader) Read(p []byte) (n int, err error) {
	pipe := r.pipe
	pipe.mu.Lock()
	defer pipe.mu.Unlock()

	// Wait for data or close
	for pipe.count == 0 && !pipe.closed && pipe.writeErr == nil {
		pipe.cond.Wait()
	}

	// Check for errors/close
	if pipe.count == 0 {
		if pipe.writeErr != nil {
			return 0, pipe.writeErr
		}
		if pipe.closed {
			return 0, io.EOF
		}
	}

	// Read available data
	n = 0
	for n < len(p) && pipe.count > 0 {
		// Calculate how much we can read in one contiguous chunk
		available := pipe.size - pipe.start
		if available > pipe.count {
			available = pipe.count
		}
		if available > len(p)-n {
			available = len(p) - n
		}

		copy(p[n:n+available], pipe.buffer[pipe.start:pipe.start+available])
		pipe.start = (pipe.start + available) % pipe.size
		pipe.count -= available
		n += available
	}

	// Signal writers that space is available
	pipe.cond.Signal()
	return n, nil
}

// Close closes the reader
func (r *BufferedPipeReader) Close() error {
	return r.CloseWithError(nil)
}

// CloseWithError closes the reader with an error
func (r *BufferedPipeReader) CloseWithError(err error) error {
	pipe := r.pipe
	pipe.mu.Lock()
	defer pipe.mu.Unlock()

	if err == nil {
		err = io.ErrClosedPipe
	}
	pipe.readErr = err
	pipe.cond.Broadcast()
	return nil
}

// Write writes data to the pipe. If the buffer is full, it drops oldest data
// to make room (non-blocking behavior to prevent RTP handler stalls).
func (w *BufferedPipeWriter) Write(p []byte) (n int, err error) {
	pipe := w.pipe
	pipe.mu.Lock()
	defer pipe.mu.Unlock()

	if pipe.closed || pipe.readErr != nil {
		return 0, io.ErrClosedPipe
	}

	n = 0
	for n < len(p) {
		// Calculate available space
		space := pipe.size - pipe.count

		if space == 0 {
			// Buffer full - drop oldest data to make room
			// This prevents blocking the RTP handler
			dropSize := len(p) - n
			if dropSize > pipe.size/2 {
				dropSize = pipe.size / 2 // Don't drop more than half at once
			}
			pipe.start = (pipe.start + dropSize) % pipe.size
			pipe.count -= dropSize
			space = dropSize
		}

		// Calculate how much we can write in one contiguous chunk
		toWrite := len(p) - n
		if toWrite > space {
			toWrite = space
		}

		// Handle wraparound
		endSpace := pipe.size - pipe.end
		if toWrite > endSpace {
			// Write in two parts
			copy(pipe.buffer[pipe.end:], p[n:n+endSpace])
			copy(pipe.buffer[0:toWrite-endSpace], p[n+endSpace:n+toWrite])
		} else {
			copy(pipe.buffer[pipe.end:pipe.end+toWrite], p[n:n+toWrite])
		}

		pipe.end = (pipe.end + toWrite) % pipe.size
		pipe.count += toWrite
		n += toWrite
	}

	// Signal readers that data is available
	pipe.cond.Signal()
	return n, nil
}

// Close closes the writer
func (w *BufferedPipeWriter) Close() error {
	return w.CloseWithError(nil)
}

// CloseWithError closes the writer with an error
func (w *BufferedPipeWriter) CloseWithError(err error) error {
	pipe := w.pipe
	pipe.mu.Lock()
	defer pipe.mu.Unlock()

	pipe.closed = true
	if err != nil {
		pipe.writeErr = err
	}
	pipe.cond.Broadcast()
	return nil
}

// Buffered returns the number of bytes currently buffered
func (w *BufferedPipeWriter) Buffered() int {
	pipe := w.pipe
	pipe.mu.Lock()
	defer pipe.mu.Unlock()
	return pipe.count
}

// ErrBufferOverflow is returned when data is dropped due to buffer overflow
var ErrBufferOverflow = errors.New("buffer overflow: data dropped")

// bufferedPipeReaderAdapter adapts BufferedPipeReader to io.PipeReader interface
type bufferedPipeReaderAdapter struct {
	*BufferedPipeReader
}

// CloseWithError implements the io.PipeReader interface
func (a *bufferedPipeReaderAdapter) CloseWithError(err error) error {
	return a.BufferedPipeReader.CloseWithError(err)
}

// bufferedPipeWriterAdapter adapts BufferedPipeWriter to io.PipeWriter interface
type bufferedPipeWriterAdapter struct {
	*BufferedPipeWriter
}

// CloseWithError implements the io.PipeWriter interface
func (a *bufferedPipeWriterAdapter) CloseWithError(err error) error {
	return a.BufferedPipeWriter.CloseWithError(err)
}

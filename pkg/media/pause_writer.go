package media

import (
	"io"
	"sync"
)

// PausableWriter wraps an io.Writer and allows pausing/resuming writes
type PausableWriter struct {
	writer   io.Writer
	paused   bool
	pauseMu  sync.RWMutex
	onPause  func() // Optional callback when paused
	onResume func() // Optional callback when resumed
}

// NewPausableWriter creates a new pausable writer
func NewPausableWriter(w io.Writer) *PausableWriter {
	return &PausableWriter{
		writer: w,
		paused: false,
	}
}

// Write implements io.Writer, only writing when not paused
func (pw *PausableWriter) Write(p []byte) (n int, err error) {
	pw.pauseMu.RLock()
	defer pw.pauseMu.RUnlock()

	// If paused, pretend we wrote the data but don't actually write
	if pw.paused {
		return len(p), nil
	}

	// Write to the underlying writer while holding the read lock
	// This ensures pause state cannot change during the write
	return pw.writer.Write(p)
}

// Pause pauses writing
func (pw *PausableWriter) Pause() {
	pw.pauseMu.Lock()
	defer pw.pauseMu.Unlock()
	
	if !pw.paused {
		pw.paused = true
		if pw.onPause != nil {
			pw.onPause()
		}
	}
}

// Resume resumes writing
func (pw *PausableWriter) Resume() {
	pw.pauseMu.Lock()
	defer pw.pauseMu.Unlock()
	
	if pw.paused {
		pw.paused = false
		if pw.onResume != nil {
			pw.onResume()
		}
	}
}

// IsPaused returns whether writing is paused
func (pw *PausableWriter) IsPaused() bool {
	pw.pauseMu.RLock()
	defer pw.pauseMu.RUnlock()
	return pw.paused
}

// SetCallbacks sets optional callbacks for pause/resume events
func (pw *PausableWriter) SetCallbacks(onPause, onResume func()) {
	pw.pauseMu.Lock()
	defer pw.pauseMu.Unlock()
	pw.onPause = onPause
	pw.onResume = onResume
}

// PausableReader wraps an io.Reader and allows pausing/resuming reads
type PausableReader struct {
	reader   io.Reader
	paused   bool
	pauseMu  sync.RWMutex
	pauseCh  chan struct{}
	resumeCh chan struct{}
}

// NewPausableReader creates a new pausable reader
func NewPausableReader(r io.Reader) *PausableReader {
	return &PausableReader{
		reader:   r,
		paused:   false,
		pauseCh:  make(chan struct{}),
		resumeCh: make(chan struct{}),
	}
}

// Read implements io.Reader, blocking when paused
func (pr *PausableReader) Read(p []byte) (n int, err error) {
	for {
		pr.pauseMu.RLock()
		isPaused := pr.paused
		pr.pauseMu.RUnlock()

		if !isPaused {
			// Not paused, perform the read
			return pr.reader.Read(p)
		}

		// Wait until resumed
		select {
		case <-pr.resumeCh:
			// Resume signal received, continue to read
			continue
		case <-pr.pauseCh:
			// Redundant pause signal, ignore and continue waiting
			continue
		}
	}
}

// Pause pauses reading
func (pr *PausableReader) Pause() {
	pr.pauseMu.Lock()
	defer pr.pauseMu.Unlock()
	
	if !pr.paused {
		pr.paused = true
		// Signal pause (non-blocking)
		select {
		case pr.pauseCh <- struct{}{}:
		default:
		}
	}
}

// Resume resumes reading
func (pr *PausableReader) Resume() {
	pr.pauseMu.Lock()
	defer pr.pauseMu.Unlock()
	
	if pr.paused {
		pr.paused = false
		// Signal resume (non-blocking)
		select {
		case pr.resumeCh <- struct{}{}:
		default:
		}
	}
}

// IsPaused returns whether reading is paused
func (pr *PausableReader) IsPaused() bool {
	pr.pauseMu.RLock()
	defer pr.pauseMu.RUnlock()
	return pr.paused
}
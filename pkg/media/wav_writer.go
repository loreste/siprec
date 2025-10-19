package media

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"
)

// WAVWriter handles writing PCM samples into a WAV container.
type WAVWriter struct {
	file          *os.File
	sampleRate    int
	channels      int
	bytesWritten  uint32
	headerWritten bool
	finalized     bool
	mu            sync.Mutex
}

// NewWAVWriter creates a WAV writer and writes an initial header.
func NewWAVWriter(file *os.File, sampleRate, channels int) (*WAVWriter, error) {
	if file == nil {
		return nil, fmt.Errorf("nil file provided for WAV writer")
	}
	if sampleRate <= 0 {
		sampleRate = 8000
	}
	if channels <= 0 {
		channels = 1
	}

	writer := &WAVWriter{
		file:       file,
		sampleRate: sampleRate,
		channels:   channels,
	}

	if err := writer.writeHeader(); err != nil {
		return nil, err
	}
	return writer, nil
}

// SetFormat updates the sample rate or channel count used for the header.
// The new values are applied immediately.
func (w *WAVWriter) SetFormat(sampleRate, channels int) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.finalized {
		return fmt.Errorf("cannot change WAV format after finalization")
	}

	if sampleRate > 0 {
		w.sampleRate = sampleRate
	}
	if channels > 0 {
		w.channels = channels
	}
	return w.rewriteHeaderLocked()
}

// Write appends PCM samples to the WAV file.
func (w *WAVWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.headerWritten {
		if err := w.writeHeaderLocked(); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.bytesWritten += uint32(n)
	return n, err
}

// Finalize updates the WAV header with the final data sizes.
func (w *WAVWriter) Finalize() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.finalized {
		return nil
	}

	if !w.headerWritten {
		if err := w.writeHeaderLocked(); err != nil {
			return err
		}
	}

	if err := w.updateSizesLocked(); err != nil {
		return err
	}

	w.finalized = true
	return nil
}

func (w *WAVWriter) writeHeader() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writeHeaderLocked()
}

func (w *WAVWriter) writeHeaderLocked() error {
	header := make([]byte, 44)

	// ChunkID "RIFF"
	copy(header[0:], []byte("RIFF"))
	// ChunkSize (placeholder, will be updated in Finalize)
	binary.LittleEndian.PutUint32(header[4:], 36)
	// Format "WAVE"
	copy(header[8:], []byte("WAVE"))
	// Subchunk1ID "fmt "
	copy(header[12:], []byte("fmt "))
	// Subchunk1Size (16 for PCM)
	binary.LittleEndian.PutUint32(header[16:], 16)
	// AudioFormat (1 = PCM)
	binary.LittleEndian.PutUint16(header[20:], 1)
	// NumChannels
	binary.LittleEndian.PutUint16(header[22:], uint16(w.channels))
	// SampleRate
	binary.LittleEndian.PutUint32(header[24:], uint32(w.sampleRate))
	// ByteRate = SampleRate * NumChannels * BitsPerSample/8 (16-bit samples)
	byteRate := uint32(w.sampleRate * w.channels * 2)
	binary.LittleEndian.PutUint32(header[28:], byteRate)
	// BlockAlign = NumChannels * BitsPerSample/8
	blockAlign := uint16(w.channels * 2)
	binary.LittleEndian.PutUint16(header[32:], blockAlign)
	// BitsPerSample = 16
	binary.LittleEndian.PutUint16(header[34:], 16)
	// Subchunk2ID "data"
	copy(header[36:], []byte("data"))
	// Subchunk2Size placeholder
	binary.LittleEndian.PutUint32(header[40:], 0)

	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := w.file.Write(header); err != nil {
		return err
	}
	if _, err := w.file.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	w.headerWritten = true
	return nil
}

func (w *WAVWriter) rewriteHeaderLocked() error {
	if !w.headerWritten {
		return w.writeHeaderLocked()
	}
	// Re-write header with updated format info while keeping current sizes.
	currentBytes := w.bytesWritten
	if err := w.writeHeaderLocked(); err != nil {
		return err
	}
	w.bytesWritten = currentBytes
	return w.updateSizesLocked()
}

func (w *WAVWriter) updateSizesLocked() error {
	// Update ChunkSize and Subchunk2Size
	if _, err := w.file.Seek(4, io.SeekStart); err != nil {
		return err
	}
	fileSize := w.bytesWritten + 36
	if err := binary.Write(w.file, binary.LittleEndian, fileSize); err != nil {
		return err
	}
	if _, err := w.file.Seek(40, io.SeekStart); err != nil {
		return err
	}
	if err := binary.Write(w.file, binary.LittleEndian, w.bytesWritten); err != nil {
		return err
	}
	if _, err := w.file.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	return nil
}

package media

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// WAVReader provides minimal streaming reads for 16-bit PCM WAV files.
type WAVReader struct {
	file          *os.File
	SampleRate    int
	Channels      int
	BitsPerSample int

	dataOffset int64
	dataSize   int64
	bytesRead  int64
}

// NewWAVReader opens a WAV file for streaming reads.
func NewWAVReader(path string) (*WAVReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	reader := &WAVReader{
		file: f,
	}

	if err := reader.parseHeader(); err != nil {
		f.Close()
		return nil, fmt.Errorf("invalid WAV file %s: %w", path, err)
	}
	return reader, nil
}

func (wr *WAVReader) parseHeader() error {
	header := make([]byte, 12)
	if _, err := io.ReadFull(wr.file, header); err != nil {
		return err
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return fmt.Errorf("missing RIFF/WAVE header")
	}

	var fmtFound bool
	var dataFound bool

	for !fmtFound || !dataFound {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(wr.file, chunkHeader); err != nil {
			return err
		}
		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:8])

		switch chunkID {
		case "fmt ":
			fmtChunk := make([]byte, chunkSize)
			if _, err := io.ReadFull(wr.file, fmtChunk); err != nil {
				return err
			}
			audioFormat := binary.LittleEndian.Uint16(fmtChunk[0:2])
			if audioFormat != 1 {
				return fmt.Errorf("unsupported audio format: %d", audioFormat)
			}
			wr.Channels = int(binary.LittleEndian.Uint16(fmtChunk[2:4]))
			wr.SampleRate = int(binary.LittleEndian.Uint32(fmtChunk[4:8]))
			wr.BitsPerSample = int(binary.LittleEndian.Uint16(fmtChunk[14:16]))
			if wr.BitsPerSample != 16 {
				return fmt.Errorf("unsupported bits per sample: %d", wr.BitsPerSample)
			}
			fmtFound = true

			// Skip any remaining fmt bytes
			if extra := int64(chunkSize) - 16; extra > 0 {
				if _, err := wr.file.Seek(extra, io.SeekCurrent); err != nil {
					return err
				}
			}
		case "data":
			wr.dataOffset, _ = wr.file.Seek(0, io.SeekCurrent)
			wr.dataSize = int64(chunkSize)
			if _, err := wr.file.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				return err
			}
			dataFound = true
		default:
			if _, err := wr.file.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				return err
			}
		}
	}

	if _, err := wr.file.Seek(wr.dataOffset, io.SeekStart); err != nil {
		return err
	}

	return nil
}

// ReadSamples reads up to maxSamples (per channel) from the WAV file.
// Returns io.EOF when no samples remain.
func (wr *WAVReader) ReadSamples(maxSamples int) ([]int16, error) {
	if wr.bytesRead >= wr.dataSize {
		return nil, io.EOF
	}

	if maxSamples <= 0 {
		maxSamples = 1024
	}

	bytesPerFrame := wr.Channels * (wr.BitsPerSample / 8)
	remainingFrames := int((wr.dataSize - wr.bytesRead) / int64(bytesPerFrame))
	if remainingFrames <= 0 {
		return nil, io.EOF
	}

	if maxSamples > remainingFrames {
		maxSamples = remainingFrames
	}

	readBuffer := make([]byte, maxSamples*bytesPerFrame)
	n, err := io.ReadFull(wr.file, readBuffer)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	if n == 0 {
		return nil, io.EOF
	}
	wr.bytesRead += int64(n)

	frameCount := n / bytesPerFrame
	samples := make([]int16, frameCount*wr.Channels)
	for i := 0; i < frameCount*wr.Channels; i++ {
		byteIdx := i * 2
		samples[i] = int16(binary.LittleEndian.Uint16(readBuffer[byteIdx : byteIdx+2]))
	}

	return samples, nil
}

// Close closes the underlying file.
func (wr *WAVReader) Close() error {
	if wr.file == nil {
		return nil
	}
	err := wr.file.Close()
	wr.file = nil
	return err
}

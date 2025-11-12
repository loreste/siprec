package media

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CombineWAVRecordings merges multiple mono recordings into a single multi-channel WAV.
// The order of inputPaths determines the channel order in the output file.
func CombineWAVRecordings(outputPath string, inputPaths []string) error {
	if len(inputPaths) < 2 {
		return fmt.Errorf("need at least two recordings to combine")
	}

	readers := make([]*WAVReader, 0, len(inputPaths))
	defer func() {
		for _, r := range readers {
			if r != nil {
				r.Close()
			}
		}
	}()

	for _, path := range inputPaths {
		reader, err := NewWAVReader(path)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", path, err)
		}
		if reader.Channels != 1 {
			reader.Close()
			return fmt.Errorf("%s is not mono (channels=%d)", path, reader.Channels)
		}
		readers = append(readers, reader)
	}

	sampleRate := readers[0].SampleRate
	for _, r := range readers[1:] {
		if r.SampleRate != sampleRate {
			return fmt.Errorf("mismatched sample rates: %d vs %d", sampleRate, r.SampleRate)
		}
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	writer, err := NewWAVWriter(outFile, sampleRate, len(readers))
	if err != nil {
		return err
	}

	const chunkSize = 1024
	channelCount := len(readers)
	buffer := make([]byte, chunkSize*channelCount*2)

	for {
		maxSamples := 0
		channelSamples := make([][]int16, channelCount)

		for i, r := range readers {
			samples, err := r.ReadSamples(chunkSize)
			if err != nil && err != io.EOF {
				return fmt.Errorf("failed to read samples: %w", err)
			}
			channelSamples[i] = samples
			if len(samples) > maxSamples {
				maxSamples = len(samples)
			}
		}

		if maxSamples == 0 {
			break
		}

		required := maxSamples * channelCount * 2
		if len(buffer) < required {
			buffer = make([]byte, required)
		}

		for sampleIdx := 0; sampleIdx < maxSamples; sampleIdx++ {
			for ch := 0; ch < channelCount; ch++ {
				var sample int16
				if sampleIdx < len(channelSamples[ch]) {
					sample = channelSamples[ch][sampleIdx]
				}
				offset := (sampleIdx*channelCount + ch) * 2
				buffer[offset] = byte(sample)
				buffer[offset+1] = byte(sample >> 8)
			}
		}

		if _, err := writer.Write(buffer[:required]); err != nil {
			return fmt.Errorf("failed to write combined samples: %w", err)
		}
	}

	if err := writer.Finalize(); err != nil {
		return err
	}

	return nil
}

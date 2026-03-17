package media

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// LegTiming holds timing information for a recording leg
type LegTiming struct {
	Path          string    // Path to the WAV file
	FirstRTPTime  time.Time // Wall-clock time of first RTP packet
	SampleRate    int       // Sample rate (for calculating padding)
}

// CombineWAVRecordings merges multiple mono recordings into a single multi-channel WAV.
// The order of inputPaths determines the channel order in the output file.
func CombineWAVRecordings(outputPath string, inputPaths []string) error {
	// Convert to LegTiming without alignment info
	legs := make([]LegTiming, len(inputPaths))
	for i, path := range inputPaths {
		legs[i] = LegTiming{Path: path}
	}
	return CombineWAVRecordingsAligned(outputPath, legs)
}

// CombineWAVRecordingsAligned merges multiple mono recordings with start-time alignment.
// If timing information is provided, it will pad the earlier-starting leg(s) with silence
// so both channels are wall-clock aligned in the output.
func CombineWAVRecordingsAligned(outputPath string, legs []LegTiming) error {
	if len(legs) < 2 {
		return fmt.Errorf("need at least two recordings to combine")
	}

	readers := make([]*WAVReader, 0, len(legs))
	defer func() {
		for _, r := range readers {
			if r != nil {
				r.Close()
			}
		}
	}()

	for _, leg := range legs {
		reader, err := NewWAVReader(leg.Path)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", leg.Path, err)
		}
		if reader.Channels != 1 {
			reader.Close()
			return fmt.Errorf("%s is not mono (channels=%d)", leg.Path, reader.Channels)
		}
		readers = append(readers, reader)
	}

	sampleRate := readers[0].SampleRate
	for _, r := range readers[1:] {
		if r.SampleRate != sampleRate {
			return fmt.Errorf("mismatched sample rates: %d vs %d", sampleRate, r.SampleRate)
		}
	}

	// Calculate padding for each leg based on start times (Fix G)
	padSamples := calculatePadding(legs, sampleRate)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
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

	// Track how many padding samples remain for each channel
	remainingPad := make([]int, channelCount)
	copy(remainingPad, padSamples)

	for {
		maxSamples := 0
		channelSamples := make([][]int16, channelCount)

		for i, r := range readers {
			// If we still have padding to output, create silence samples
			if remainingPad[i] > 0 {
				padCount := chunkSize
				if padCount > remainingPad[i] {
					padCount = remainingPad[i]
				}
				channelSamples[i] = make([]int16, padCount)
				remainingPad[i] -= padCount
				if padCount > maxSamples {
					maxSamples = padCount
				}
				continue
			}

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

// calculatePadding determines how many silence samples to prepend to each leg
// so that all legs are aligned to the same wall-clock start time.
func calculatePadding(legs []LegTiming, sampleRate int) []int {
	padSamples := make([]int, len(legs))

	// Check if we have valid timing for all legs
	hasValidTiming := true
	for _, leg := range legs {
		if leg.FirstRTPTime.IsZero() {
			hasValidTiming = false
			break
		}
	}

	if !hasValidTiming || len(legs) < 2 {
		return padSamples // No padding needed
	}

	// Find the latest start time (this leg needs no padding)
	var latestStart time.Time
	for _, leg := range legs {
		if leg.FirstRTPTime.After(latestStart) {
			latestStart = leg.FirstRTPTime
		}
	}

	// Calculate padding for each leg
	for i, leg := range legs {
		delta := latestStart.Sub(leg.FirstRTPTime)
		if delta > 0 {
			// Calculate samples to pad: delta * sampleRate
			// delta is in nanoseconds, sampleRate is samples/second
			padSamples[i] = int(delta.Seconds() * float64(sampleRate))
		}
	}

	return padSamples
}

// GetFirstRTPTiming retrieves the first RTP timing from an RTPForwarder
func GetFirstRTPTiming(f *RTPForwarder) (time.Time, bool) {
	if f == nil {
		return time.Time{}, false
	}
	f.firstRTPMutex.Lock()
	defer f.firstRTPMutex.Unlock()
	return f.FirstRTPWallClock, f.HasFirstRTP
}

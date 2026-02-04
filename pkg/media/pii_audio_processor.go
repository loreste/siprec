package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

// PIIAudioProcessor handles audio redaction based on PII markers
type PIIAudioProcessor struct {
	logger         *logrus.Logger
	redactionType  RedactionType
	sampleRate     int
	bytesPerSample int
}

// RedactionType specifies how PII segments should be redacted in audio
type RedactionType string

const (
	// RedactionSilence replaces PII audio with silence
	RedactionSilence RedactionType = "silence"
	// RedactionTone replaces PII audio with a tone (1kHz beep)
	RedactionTone RedactionType = "tone"
	// RedactionNoise replaces PII audio with white noise
	RedactionNoise RedactionType = "noise"
)

// PIIAudioProcessorConfig holds configuration for the audio processor
type PIIAudioProcessorConfig struct {
	RedactionType  RedactionType
	SampleRate     int // Audio sample rate (default: 8000 Hz)
	BytesPerSample int // Bytes per audio sample (default: 2 for 16-bit PCM)
}

// NewPIIAudioProcessor creates a new PII audio processor
func NewPIIAudioProcessor(logger *logrus.Logger, config *PIIAudioProcessorConfig) *PIIAudioProcessor {
	if config == nil {
		config = &PIIAudioProcessorConfig{
			RedactionType:  RedactionSilence,
			SampleRate:     8000,
			BytesPerSample: 2,
		}
	}

	if config.SampleRate == 0 {
		config.SampleRate = 8000
	}
	if config.BytesPerSample == 0 {
		config.BytesPerSample = 2
	}
	if config.RedactionType == "" {
		config.RedactionType = RedactionSilence
	}

	return &PIIAudioProcessor{
		logger:         logger,
		redactionType:  config.RedactionType,
		sampleRate:     config.SampleRate,
		bytesPerSample: config.BytesPerSample,
	}
}

// ProcessRecording applies PII redaction to a raw audio file
// The input file should be raw PCM audio (no header)
func (p *PIIAudioProcessor) ProcessRecording(inputPath, outputPath string, intervals []RedactionInterval) error {
	if len(intervals) == 0 {
		p.logger.Debug("No redaction intervals, copying file unchanged")
		return copyFile(inputPath, outputPath)
	}

	// Merge overlapping intervals
	mergedIntervals := MergeOverlappingIntervals(intervals)

	p.logger.WithFields(logrus.Fields{
		"input_path":       inputPath,
		"output_path":      outputPath,
		"intervals":        len(mergedIntervals),
		"redaction_type":   p.redactionType,
		"sample_rate":      p.sampleRate,
		"bytes_per_sample": p.bytesPerSample,
	}).Info("Processing audio for PII redaction")

	// Open input file
	inputFile, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer inputFile.Close()

	// Get file size
	fileInfo, err := inputFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat input file: %w", err)
	}
	totalBytes := fileInfo.Size()
	totalDuration := p.bytesToDuration(totalBytes)

	// Create output file
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outputFile.Close()

	// Process audio in chunks
	const bufferSize = 8192
	buffer := make([]byte, bufferSize)
	var currentOffset int64

	for {
		n, err := inputFile.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		// Check if this chunk overlaps with any redaction interval
		chunkStart := p.bytesToDuration(currentOffset)
		chunkEnd := p.bytesToDuration(currentOffset + int64(n))

		outputChunk := buffer[:n]
		for _, interval := range mergedIntervals {
			if p.intervalsOverlap(chunkStart, chunkEnd, interval.StartOffset, interval.EndOffset) {
				// Calculate the overlap within this chunk
				redactStart := maxDuration(interval.StartOffset, chunkStart)
				redactEnd := minDuration(interval.EndOffset, chunkEnd)

				// Convert to byte offsets within the chunk
				startByte := int(p.durationToBytes(redactStart - chunkStart))
				endByte := int(p.durationToBytes(redactEnd - chunkStart))

				if startByte < 0 {
					startByte = 0
				}
				if endByte > n {
					endByte = n
				}

				// Apply redaction to the overlapping portion
				outputChunk = p.redactBytes(outputChunk, startByte, endByte)
			}
		}

		if _, err := outputFile.Write(outputChunk); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}

		currentOffset += int64(n)
	}

	p.logger.WithFields(logrus.Fields{
		"input_path":     inputPath,
		"output_path":    outputPath,
		"total_duration": totalDuration,
		"intervals":      len(mergedIntervals),
	}).Info("Audio PII redaction complete")

	return nil
}

// ProcessRecordingInPlace applies PII redaction to a file in place
func (p *PIIAudioProcessor) ProcessRecordingInPlace(path string, intervals []RedactionInterval) error {
	if len(intervals) == 0 {
		return nil
	}

	// Create temporary output file
	tempPath := path + ".pii_temp"
	if err := p.ProcessRecording(path, tempPath, intervals); err != nil {
		os.Remove(tempPath) // Clean up temp file on error
		return err
	}

	// Replace original file with redacted version
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath) // Clean up temp file on error
		return fmt.Errorf("failed to replace original file: %w", err)
	}

	return nil
}

// redactBytes applies redaction to a portion of the audio buffer
func (p *PIIAudioProcessor) redactBytes(data []byte, start, end int) []byte {
	// Ensure alignment to sample boundaries
	start = (start / p.bytesPerSample) * p.bytesPerSample
	end = ((end + p.bytesPerSample - 1) / p.bytesPerSample) * p.bytesPerSample

	if end > len(data) {
		end = len(data)
	}

	result := make([]byte, len(data))
	copy(result, data)

	switch p.redactionType {
	case RedactionSilence:
		// Replace with silence (zeros for PCM)
		for i := start; i < end; i++ {
			result[i] = 0
		}

	case RedactionTone:
		// Replace with 1kHz tone
		p.generateTone(result[start:end])

	case RedactionNoise:
		// Replace with low-level white noise
		p.generateNoise(result[start:end])

	default:
		// Default to silence
		for i := start; i < end; i++ {
			result[i] = 0
		}
	}

	return result
}

// generateTone generates a 1kHz sine wave tone (16-bit PCM)
func (p *PIIAudioProcessor) generateTone(buffer []byte) {
	if len(buffer) < 2 {
		return
	}

	// Generate 1kHz tone at 50% volume
	frequency := 1000.0
	amplitude := int16(16384) // 50% of max 16-bit amplitude
	samplesPerCycle := float64(p.sampleRate) / frequency

	numSamples := len(buffer) / p.bytesPerSample
	for i := 0; i < numSamples; i++ {
		// Simple approximation of sine wave using triangle wave
		phase := float64(i) / samplesPerCycle
		phase = phase - float64(int(phase)) // Keep in 0-1 range

		var value int16
		if phase < 0.25 {
			value = int16(float64(amplitude) * (phase * 4))
		} else if phase < 0.75 {
			value = int16(float64(amplitude) * (2 - phase*4))
		} else {
			value = int16(float64(amplitude) * (phase*4 - 4))
		}

		// Write 16-bit sample (little-endian)
		binary.LittleEndian.PutUint16(buffer[i*2:], uint16(value))
	}
}

// generateNoise generates low-level white noise
func (p *PIIAudioProcessor) generateNoise(buffer []byte) {
	// Simple LFSR-based noise generation
	lfsr := uint16(0xACE1) // Seed
	amplitude := int16(4096) // Low amplitude noise

	numSamples := len(buffer) / p.bytesPerSample
	for i := 0; i < numSamples; i++ {
		// LFSR step
		bit := ((lfsr >> 0) ^ (lfsr >> 2) ^ (lfsr >> 3) ^ (lfsr >> 5)) & 1
		lfsr = (lfsr >> 1) | (bit << 15)

		// Scale to amplitude
		value := int16(lfsr) % amplitude
		if lfsr&1 == 0 {
			value = -value
		}

		binary.LittleEndian.PutUint16(buffer[i*2:], uint16(value))
	}
}

// bytesToDuration converts byte offset to time duration
func (p *PIIAudioProcessor) bytesToDuration(bytes int64) time.Duration {
	samples := bytes / int64(p.bytesPerSample)
	return time.Duration(samples) * time.Second / time.Duration(p.sampleRate)
}

// durationToBytes converts time duration to byte offset
func (p *PIIAudioProcessor) durationToBytes(d time.Duration) int64 {
	samples := int64(d.Seconds() * float64(p.sampleRate))
	return samples * int64(p.bytesPerSample)
}

// intervalsOverlap checks if two time intervals overlap
func (p *PIIAudioProcessor) intervalsOverlap(start1, end1, start2, end2 time.Duration) bool {
	return start1 < end2 && end1 > start2
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// ProcessRecordingWithMetadata applies PII redaction using marker metadata
func (p *PIIAudioProcessor) ProcessRecordingWithMetadata(metadata *PIIAudioMarkerMetadata) error {
	if metadata == nil || len(metadata.Markers) == 0 {
		return nil
	}

	// Convert markers to redaction intervals
	intervals := make([]RedactionInterval, 0, len(metadata.Markers))
	var startTime time.Time
	for i, marker := range metadata.Markers {
		if i == 0 {
			startTime = marker.StartTime
		}
		intervals = append(intervals, RedactionInterval{
			StartOffset: marker.StartTime.Sub(startTime),
			EndOffset:   marker.EndTime.Sub(startTime),
			PIIType:     marker.PIIType,
			Reason:      "PII detected in transcription",
		})
	}

	return p.ProcessRecordingInPlace(metadata.RecordingPath, intervals)
}

// GenerateRedactionReport creates a summary report of redactions applied
func (p *PIIAudioProcessor) GenerateRedactionReport(intervals []RedactionInterval) *PIIRedactionReport {
	if len(intervals) == 0 {
		return &PIIRedactionReport{
			TotalIntervals:  0,
			TotalDuration:   0,
			TypeCounts:      make(map[string]int),
			ProcessedAt:     time.Now(),
		}
	}

	merged := MergeOverlappingIntervals(intervals)

	var totalDuration time.Duration
	typeCounts := make(map[string]int)

	for _, interval := range merged {
		totalDuration += interval.EndOffset - interval.StartOffset
		typeCounts[interval.PIIType]++
	}

	return &PIIRedactionReport{
		TotalIntervals:  len(merged),
		TotalDuration:   totalDuration,
		TypeCounts:      typeCounts,
		RedactionType:   string(p.redactionType),
		ProcessedAt:     time.Now(),
	}
}

// PIIRedactionReport contains statistics about audio redaction
type PIIRedactionReport struct {
	TotalIntervals int               `json:"total_intervals"`
	TotalDuration  time.Duration     `json:"total_duration"`
	TypeCounts     map[string]int    `json:"type_counts"`
	RedactionType  string            `json:"redaction_type"`
	ProcessedAt    time.Time         `json:"processed_at"`
}

// SaveRedactionMetadata saves redaction metadata to a JSON sidecar file
func (p *PIIAudioProcessor) SaveRedactionMetadata(recordingPath string, metadata *PIIAudioMarkerMetadata) error {
	if metadata == nil {
		return nil
	}

	metadataPath := recordingPath + ".pii.json"

	var buf bytes.Buffer
	buf.WriteString("{\n")
	buf.WriteString(fmt.Sprintf("  \"call_uuid\": \"%s\",\n", metadata.CallUUID))
	buf.WriteString(fmt.Sprintf("  \"recording_path\": \"%s\",\n", metadata.RecordingPath))
	buf.WriteString(fmt.Sprintf("  \"total_markers\": %d,\n", metadata.TotalMarkers))
	buf.WriteString(fmt.Sprintf("  \"created_at\": \"%s\",\n", metadata.CreatedAt.Format(time.RFC3339)))
	buf.WriteString("  \"markers\": [\n")

	for i, marker := range metadata.Markers {
		buf.WriteString("    {\n")
		buf.WriteString(fmt.Sprintf("      \"pii_type\": \"%s\",\n", marker.PIIType))
		buf.WriteString(fmt.Sprintf("      \"start_time\": \"%s\",\n", marker.StartTime.Format(time.RFC3339Nano)))
		buf.WriteString(fmt.Sprintf("      \"end_time\": \"%s\",\n", marker.EndTime.Format(time.RFC3339Nano)))
		buf.WriteString(fmt.Sprintf("      \"confidence\": %.2f,\n", marker.Confidence))
		buf.WriteString(fmt.Sprintf("      \"redacted_text\": \"%s\"\n", marker.RedactedText))
		if i < len(metadata.Markers)-1 {
			buf.WriteString("    },\n")
		} else {
			buf.WriteString("    }\n")
		}
	}

	buf.WriteString("  ]\n")
	buf.WriteString("}\n")

	return os.WriteFile(metadataPath, buf.Bytes(), 0644)
}

package media

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestWAV(t *testing.T, path string, samples []int16) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer f.Close()

	writer, err := NewWAVWriter(f, 8000, 1)
	if err != nil {
		t.Fatalf("wav writer: %v", err)
	}

	buf := make([]byte, len(samples)*2)
	for i, sample := range samples {
		buf[i*2] = byte(sample)
		buf[i*2+1] = byte(sample >> 8)
	}
	if _, err := writer.Write(buf); err != nil {
		t.Fatalf("write samples: %v", err)
	}
	if err := writer.Finalize(); err != nil {
		t.Fatalf("finalize: %v", err)
	}
}

func TestCombineWAVRecordings(t *testing.T) {
	dir := t.TempDir()
	leg0 := filepath.Join(dir, "leg0.wav")
	leg1 := filepath.Join(dir, "leg1.wav")

	writeTestWAV(t, leg0, []int16{100, 200, 300, 400})
	writeTestWAV(t, leg1, []int16{1000, 2000})

	output := filepath.Join(dir, "combined.wav")
	if err := CombineWAVRecordings(output, []string{leg0, leg1}); err != nil {
		t.Fatalf("combine: %v", err)
	}

	reader, err := NewWAVReader(output)
	if err != nil {
		t.Fatalf("open combined: %v", err)
	}
	defer reader.Close()

	if reader.Channels != 2 {
		t.Fatalf("expected 2 channels, got %d", reader.Channels)
	}
	if reader.SampleRate != 8000 {
		t.Fatalf("unexpected samplerate %d", reader.SampleRate)
	}

	samples, err := reader.ReadSamples(10)
	if err != nil {
		t.Fatalf("read samples: %v", err)
	}

	expected := []int16{
		100, 1000,
		200, 2000,
		300, 0,
		400, 0,
	}
	if len(samples) != len(expected) {
		t.Fatalf("expected %d samples, got %d", len(expected), len(samples))
	}
	for i := range expected {
		if samples[i] != expected[i] {
			t.Fatalf("sample %d mismatch: expected %d got %d", i, expected[i], samples[i])
		}
	}
}

func TestCombineWAVRecordingsAligned(t *testing.T) {
	dir := t.TempDir()
	leg0 := filepath.Join(dir, "leg0.wav")
	leg1 := filepath.Join(dir, "leg1.wav")

	// Leg0 has 4 samples: 100, 200, 300, 400
	// Leg1 has 2 samples: 1000, 2000
	writeTestWAV(t, leg0, []int16{100, 200, 300, 400})
	writeTestWAV(t, leg1, []int16{1000, 2000})

	// Leg1 started 2 samples (250 microseconds at 8kHz) after leg0
	// So leg0 should be padded with 2 silence samples at the beginning
	baseTime := time.Now()
	legs := []LegTiming{
		{Path: leg0, FirstRTPTime: baseTime, SampleRate: 8000},
		{Path: leg1, FirstRTPTime: baseTime.Add(250 * time.Microsecond), SampleRate: 8000}, // 2 samples later
	}

	output := filepath.Join(dir, "combined_aligned.wav")
	if err := CombineWAVRecordingsAligned(output, legs); err != nil {
		t.Fatalf("combine aligned: %v", err)
	}

	reader, err := NewWAVReader(output)
	if err != nil {
		t.Fatalf("open combined: %v", err)
	}
	defer reader.Close()

	if reader.Channels != 2 {
		t.Fatalf("expected 2 channels, got %d", reader.Channels)
	}

	samples, err := reader.ReadSamples(20)
	if err != nil {
		t.Fatalf("read samples: %v", err)
	}

	// Expected output:
	// - Leg0 starts 2 samples earlier, so it gets 2 silence samples padding
	// - Leg1 starts at sample 0
	// Format: [leg0, leg1, leg0, leg1, ...]
	// Sample 0: leg0=0 (padding), leg1=1000
	// Sample 1: leg0=0 (padding), leg1=2000
	// Sample 2: leg0=100, leg1=0
	// Sample 3: leg0=200, leg1=0
	// etc.
	expected := []int16{
		0, 1000,      // leg0 padded, leg1 starts
		0, 2000,      // leg0 padded, leg1 sample 2
		100, 0,       // leg0 starts, leg1 ends
		200, 0,       // leg0 sample 2
		300, 0,       // leg0 sample 3
		400, 0,       // leg0 sample 4
	}

	if len(samples) != len(expected) {
		t.Fatalf("expected %d samples, got %d", len(expected), len(samples))
	}
	for i := range expected {
		if samples[i] != expected[i] {
			t.Fatalf("sample %d mismatch: expected %d got %d", i, expected[i], samples[i])
		}
	}
}

func TestCombineWAVRecordingsAligned_NoTiming(t *testing.T) {
	// Test that when timing is missing, no padding is applied (backward compatible)
	dir := t.TempDir()
	leg0 := filepath.Join(dir, "leg0.wav")
	leg1 := filepath.Join(dir, "leg1.wav")

	writeTestWAV(t, leg0, []int16{100, 200})
	writeTestWAV(t, leg1, []int16{1000, 2000})

	// No timing info (zero time values)
	legs := []LegTiming{
		{Path: leg0, SampleRate: 8000},
		{Path: leg1, SampleRate: 8000},
	}

	output := filepath.Join(dir, "combined_no_timing.wav")
	if err := CombineWAVRecordingsAligned(output, legs); err != nil {
		t.Fatalf("combine: %v", err)
	}

	reader, err := NewWAVReader(output)
	if err != nil {
		t.Fatalf("open combined: %v", err)
	}
	defer reader.Close()

	samples, err := reader.ReadSamples(10)
	if err != nil {
		t.Fatalf("read samples: %v", err)
	}

	// Without timing, samples are interleaved directly (no padding)
	expected := []int16{
		100, 1000,
		200, 2000,
	}
	if len(samples) != len(expected) {
		t.Fatalf("expected %d samples, got %d", len(expected), len(samples))
	}
	for i := range expected {
		if samples[i] != expected[i] {
			t.Fatalf("sample %d mismatch: expected %d got %d", i, expected[i], samples[i])
		}
	}
}

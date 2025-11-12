package media

import (
	"os"
	"path/filepath"
	"testing"
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

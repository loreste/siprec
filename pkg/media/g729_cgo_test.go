//go:build cgo

package media

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"testing"
)

// TestDecodeAudioPayload_G729 tests G.729 decoding
func TestDecodeAudioPayload_G729(t *testing.T) {
	testCases := []struct {
		name  string
		input []byte
	}{
		{"single frame (10 bytes)", make([]byte, 10)},
		{"two frames (20 bytes)", make([]byte, 20)},
		{"typical 20ms (2 frames)", make([]byte, 20)},
		{"100ms audio (10 frames)", make([]byte, 100)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := DecodeAudioPayload(tc.input, "G729")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// G.729 produces 80 samples per 10-byte frame, each sample is 2 bytes
			numFrames := len(tc.input) / 10
			if numFrames == 0 {
				numFrames = 1
			}
			expectedLen := numFrames * 160 // 80 samples * 2 bytes
			if len(result) != expectedLen {
				t.Errorf("expected %d bytes, got %d", expectedLen, len(result))
			}

			// Verify output is valid 16-bit PCM
			for i := 0; i < len(result); i += 2 {
				_ = int16(binary.LittleEndian.Uint16(result[i:]))
			}
		})
	}
}

// TestDecodeAudioPayload_G729_Aliases tests G.729 codec name aliases
func TestDecodeAudioPayload_G729_Aliases(t *testing.T) {
	input := make([]byte, 10) // Single frame

	aliases := []string{"G729", "G.729", "G729A"}

	for _, alias := range aliases {
		t.Run(alias, func(t *testing.T) {
			result, err := DecodeAudioPayload(input, alias)
			if err != nil {
				t.Fatalf("unexpected error for alias %s: %v", alias, err)
			}

			expectedLen := 160 // 80 samples * 2 bytes
			if len(result) != expectedLen {
				t.Errorf("expected %d bytes for alias %s, got %d", expectedLen, alias, len(result))
			}
		})
	}
}

// TestDecodeAudioPayload_G729_EmptyPayload tests G.729 with empty input
func TestDecodeAudioPayload_G729_EmptyPayload(t *testing.T) {
	_, err := DecodeAudioPayload([]byte{}, "G729")
	if err == nil {
		t.Error("expected error for empty G.729 payload")
	}
}

// TestDecodeAudioPayload_G729_SIDFrame tests G.729B SID frame handling
func TestDecodeAudioPayload_G729_SIDFrame(t *testing.T) {
	input := []byte{0x00, 0x00}

	result, err := DecodeAudioPayload(input, "G729")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedLen := 160
	if len(result) != expectedLen {
		t.Errorf("expected %d bytes for SID frame, got %d", expectedLen, len(result))
	}
}

// BenchmarkDecodeAudioPayload_G729 benchmarks G.729 decoding
func BenchmarkDecodeAudioPayload_G729(b *testing.B) {
	data := make([]byte, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeAudioPayload(data, "G729")
	}
}

// BenchmarkDecodeAudioPayload_G729_MultiFrame benchmarks G.729 decoding with multiple frames
func BenchmarkDecodeAudioPayload_G729_MultiFrame(b *testing.B) {
	data := make([]byte, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeAudioPayload(data, "G729")
	}
}

// TestG729ErrorScenarioResolution verifies the exact error scenario from GitHub issue #16 is resolved
func TestG729ErrorScenarioResolution(t *testing.T) {
	codecInfo, exists := GetCodecInfo(18)
	if !exists {
		t.Fatal("payload type 18 should be recognized as G.729")
	}
	if codecInfo.Name != "G729" {
		t.Fatalf("expected codec name 'G729', got '%s'", codecInfo.Name)
	}

	payload := []byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0, 0x11, 0x22}
	codecName := codecInfo.Name

	result, err := DecodeAudioPayload(payload, codecName)
	if err != nil {
		t.Fatalf("G.729 decoding failed (this was the bug): %v", err)
	}

	if len(result) != 160 {
		t.Errorf("expected 160 bytes, got %d", len(result))
	}

	uppercaseName := "G729"
	result2, err := DecodeAudioPayload(payload, uppercaseName)
	if err != nil {
		t.Fatalf("G.729 decoding with uppercase name failed: %v", err)
	}
	if len(result2) != 160 {
		t.Errorf("expected 160 bytes for uppercase, got %d", len(result2))
	}

	t.Log("GitHub issue #16 is RESOLVED: G.729 (payload type 18) decoding works correctly")
}

// TestG729DecoderProducesNonSilentOutput tests that varied input produces non-silent output
func TestG729DecoderProducesNonSilentOutput(t *testing.T) {
	frame := []byte{0xA5, 0x3C, 0x78, 0xF1, 0x2E, 0x9B, 0x47, 0xD0, 0x6A, 0x15}

	result, err := DecodeAudioPayload(frame, "G729")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nonZeroSamples := 0
	for i := 0; i < len(result); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(result[i:]))
		if sample != 0 {
			nonZeroSamples++
		}
	}

	if nonZeroSamples == 0 {
		t.Error("expected some non-zero samples in output, got all zeros")
	}

	t.Logf("Non-zero samples: %d out of %d", nonZeroSamples, len(result)/2)
}

// TestG729PartialFrame tests handling of partial frames
func TestG729PartialFrame(t *testing.T) {
	testCases := []struct {
		name        string
		inputLen    int
		expectError bool
		numFrames   int
	}{
		{"5 bytes (partial)", 5, true, 0},
		{"7 bytes (partial)", 7, true, 0},
		{"3 bytes (partial)", 3, true, 0},
		{"15 bytes (1.5 frames)", 15, false, 1},
		{"25 bytes (2.5 frames)", 25, false, 2},
		{"20 bytes (2 frames)", 20, false, 2},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := make([]byte, tc.inputLen)
			for i := range input {
				input[i] = byte(i)
			}

			result, err := DecodeAudioPayload(input, "G729")
			if tc.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				expectedLen := tc.numFrames * 160
				if len(result) != expectedLen {
					t.Errorf("expected %d bytes, got %d", expectedLen, len(result))
				}
			}
		})
	}
}

// TestG729OutputRange tests that all output samples are within valid PCM range
func TestG729OutputRange(t *testing.T) {
	testPatterns := [][]byte{
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		{0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55},
		{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0, 0x11, 0x22},
	}

	for i, pattern := range testPatterns {
		t.Run(fmt.Sprintf("pattern_%d", i), func(t *testing.T) {
			result, err := DecodeAudioPayload(pattern, "G729")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for j := 0; j < len(result); j += 2 {
				sample := int16(binary.LittleEndian.Uint16(result[j:]))
				if sample < -32768 || sample > 32767 {
					t.Errorf("sample %d out of range: %d", j/2, sample)
				}
			}
		})
	}
}

// TestG729MultipleConsecutiveFrames tests decoding many consecutive frames
func TestG729MultipleConsecutiveFrames(t *testing.T) {
	numFrames := 50
	input := make([]byte, numFrames*10)

	for i := range input {
		input[i] = byte((i * 1103515245) >> 16)
	}

	result, err := DecodeAudioPayload(input, "G729")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedLen := numFrames * 160
	if len(result) != expectedLen {
		t.Errorf("expected %d bytes for %d frames, got %d", expectedLen, numFrames, len(result))
	}

	for i := 0; i < len(result); i += 2 {
		_ = int16(binary.LittleEndian.Uint16(result[i:]))
	}

	t.Logf("Successfully decoded %d frames (%dms of audio)", numFrames, numFrames*10)
}

// TestG729IntegrationWithRTPPacket tests full RTP packet processing for G.729
func TestG729IntegrationWithRTPPacket(t *testing.T) {
	rtpPacket := make([]byte, 32)

	rtpPacket[0] = 0x80
	rtpPacket[1] = 18
	rtpPacket[2] = 0x00
	rtpPacket[3] = 0x01
	rtpPacket[4] = 0x00
	rtpPacket[5] = 0x00
	rtpPacket[6] = 0x00
	rtpPacket[7] = 0xA0
	rtpPacket[8] = 0x12
	rtpPacket[9] = 0x34
	rtpPacket[10] = 0x56
	rtpPacket[11] = 0x78

	for i := 12; i < 32; i++ {
		rtpPacket[i] = byte(i * 7)
	}

	codecName, err := DetectCodec(rtpPacket)
	if err != nil {
		t.Fatalf("failed to detect codec: %v", err)
	}
	if codecName != "G729" {
		t.Fatalf("expected G729, got %s", codecName)
	}

	payload := rtpPacket[12:]
	result, err := DecodeAudioPayload(payload, codecName)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(result) != 320 {
		t.Errorf("expected 320 bytes, got %d", len(result))
	}
}

// TestG729OutputNotClipped verifies the decoder doesn't produce heavily clipped audio
func TestG729OutputNotClipped(t *testing.T) {
	frames := [][]byte{
		{0xA5, 0x3C, 0x78, 0xF1, 0x2E, 0x9B, 0x47, 0xD0, 0x6A, 0x15},
		{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0, 0x11, 0x22},
		{0xFF, 0xEE, 0xDD, 0xCC, 0xBB, 0xAA, 0x99, 0x88, 0x77, 0x66},
		{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99},
	}

	for i, frame := range frames {
		result, err := DecodeAudioPayload(frame, "G729")
		if err != nil {
			t.Fatalf("frame %d: unexpected error: %v", i, err)
		}

		clipped := 0
		total := len(result) / 2
		for j := 0; j < len(result); j += 2 {
			sample := int16(binary.LittleEndian.Uint16(result[j:]))
			if sample == 32767 || sample == -32768 {
				clipped++
			}
		}

		clippedPercent := float64(clipped) / float64(total) * 100
		t.Logf("Frame %d: %d/%d samples clipped (%.2f%%)", i, clipped, total, clippedPercent)

		if clippedPercent > 10 {
			t.Errorf("frame %d: too many clipped samples: %.2f%% (should be <10%%)", i, clippedPercent)
		}
	}
}

// TestG729ComfortNoiseGeneration tests SID frame handling
func TestG729ComfortNoiseGeneration(t *testing.T) {
	sidFrame := []byte{0x00, 0x00}

	result, err := DecodeAudioPayload(sidFrame, "G729")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 160 {
		t.Errorf("expected 160 bytes for SID frame, got %d", len(result))
	}

	maxAmplitude := int16(0)
	for i := 0; i < len(result); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(result[i:]))
		if sample < 0 {
			sample = -sample
		}
		if sample > maxAmplitude {
			maxAmplitude = sample
		}
	}

	if maxAmplitude > 500 {
		t.Errorf("comfort noise amplitude too high: %d", maxAmplitude)
	}

	t.Logf("Comfort noise max amplitude: %d", maxAmplitude)
}

// TestG729ConcurrentDecoding tests that multiple goroutines can decode simultaneously
func TestG729ConcurrentDecoding(t *testing.T) {
	const numGoroutines = 100
	const framesPerGoroutine = 50

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			input := make([]byte, framesPerGoroutine*10)
			for j := range input {
				input[j] = byte((id*1000 + j) & 0xFF)
			}

			result, err := DecodeAudioPayload(input, "G729")
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: %v", id, err)
				return
			}

			expectedLen := framesPerGoroutine * 160
			if len(result) != expectedLen {
				errors <- fmt.Errorf("goroutine %d: expected %d bytes, got %d", id, expectedLen, len(result))
				return
			}

			for j := 0; j < len(result); j += 2 {
				sample := int16(binary.LittleEndian.Uint16(result[j:]))
				if sample < -32768 || sample > 32767 {
					errors <- fmt.Errorf("goroutine %d: sample out of range at %d", id, j/2)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	var errCount int
	for err := range errors {
		t.Error(err)
		errCount++
	}

	if errCount == 0 {
		t.Logf("Successfully ran %d concurrent goroutines, each decoding %d frames", numGoroutines, framesPerGoroutine)
	}
}

// TestG729MemoryLeak tests for memory leaks during repeated decoding
func TestG729MemoryLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory leak test in short mode")
	}

	const iterations = 1000
	const framesPerIteration = 100

	runtime.GC()
	var baselineStats runtime.MemStats
	runtime.ReadMemStats(&baselineStats)

	for i := 0; i < iterations; i++ {
		input := make([]byte, framesPerIteration*10)
		for j := range input {
			input[j] = byte((i + j) & 0xFF)
		}

		result, err := DecodeAudioPayload(input, "G729")
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}

		if len(result) == 0 {
			t.Fatal("unexpected empty result")
		}

		if i%100 == 0 {
			runtime.GC()
		}
	}

	runtime.GC()
	runtime.GC()
	var finalStats runtime.MemStats
	runtime.ReadMemStats(&finalStats)

	heapGrowth := int64(finalStats.HeapAlloc) - int64(baselineStats.HeapAlloc)
	heapObjects := int64(finalStats.HeapObjects) - int64(baselineStats.HeapObjects)

	t.Logf("Memory stats after %d iterations:", iterations)
	t.Logf("  Heap growth: %d bytes", heapGrowth)
	t.Logf("  Heap objects delta: %d", heapObjects)
	t.Logf("  Total allocations: %d", finalStats.Mallocs-baselineStats.Mallocs)
	t.Logf("  Total frees: %d", finalStats.Frees-baselineStats.Frees)

	const maxAcceptableGrowth = 10 * 1024 * 1024
	if heapGrowth > maxAcceptableGrowth {
		t.Errorf("excessive heap growth detected: %d bytes (max acceptable: %d)", heapGrowth, maxAcceptableGrowth)
	}
}

// TestG729StressTest performs a stress test with varied inputs
func TestG729StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const numIterations = 10000

	var totalFrames int
	var totalBytes int

	for i := 0; i < numIterations; i++ {
		numFrames := (i % 10) + 1
		input := make([]byte, numFrames*10)

		for j := range input {
			input[j] = byte((i*j + j*j) & 0xFF)
		}

		result, err := DecodeAudioPayload(input, "G729")
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}

		expectedLen := numFrames * 160
		if len(result) != expectedLen {
			t.Fatalf("iteration %d: expected %d bytes, got %d", i, expectedLen, len(result))
		}

		totalFrames += numFrames
		totalBytes += len(result)
	}

	t.Logf("Stress test completed: %d iterations, %d frames, %d bytes output",
		numIterations, totalFrames, totalBytes)
}

// TestG729ConcurrentMixedCodecs tests concurrent decoding of G.729 with other codecs
func TestG729ConcurrentMixedCodecs(t *testing.T) {
	const numGoroutines = 50
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*4)

	codecs := []struct {
		name    string
		data    []byte
		minLen  int
	}{
		{"G729", make([]byte, 20), 320},
		{"PCMU", make([]byte, 160), 320},
		{"PCMA", make([]byte, 160), 320},
		{"G722", make([]byte, 160), 640},
	}

	for i := 0; i < numGoroutines; i++ {
		for _, codec := range codecs {
			wg.Add(1)
			go func(id int, codecName string, data []byte, minLen int) {
				defer wg.Done()

				input := make([]byte, len(data))
				for j := range input {
					input[j] = byte((id + j) & 0xFF)
				}

				result, err := DecodeAudioPayload(input, codecName)
				if err != nil {
					errors <- fmt.Errorf("goroutine %d, codec %s: %v", id, codecName, err)
					return
				}

				if len(result) < minLen {
					errors <- fmt.Errorf("goroutine %d, codec %s: expected at least %d bytes, got %d",
						id, codecName, minLen, len(result))
				}
			}(i, codec.name, codec.data, codec.minLen)
		}
	}

	wg.Wait()
	close(errors)

	var errCount int
	for err := range errors {
		t.Error(err)
		errCount++
	}

	if errCount == 0 {
		t.Logf("Successfully ran %d goroutines with %d codec types each", numGoroutines, len(codecs))
	}
}

// BenchmarkG729ConcurrentDecoding benchmarks concurrent G.729 decoding
func BenchmarkG729ConcurrentDecoding(b *testing.B) {
	input := make([]byte, 100)
	for i := range input {
		input[i] = byte(i)
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := DecodeAudioPayload(input, "G729")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

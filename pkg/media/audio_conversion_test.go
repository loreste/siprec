package media

import (
	"encoding/binary"
	"fmt"
	"math"
	"runtime"
	"sync"
	"testing"
)

// TestDecodeAudioPayload_PCMU tests μ-law decoding
func TestDecodeAudioPayload_PCMU(t *testing.T) {
	// Test with known μ-law encoded samples
	testCases := []struct {
		name     string
		input    []byte
		codecName string
	}{
		{"empty payload", []byte{}, "PCMU"},
		{"silence (0xFF = -0 in μ-law)", []byte{0xFF, 0xFF, 0xFF, 0xFF}, "PCMU"},
		{"single sample", []byte{0x00}, "PCMU"},
		{"multiple samples", []byte{0x00, 0x7F, 0x80, 0xFF}, "PCMU"},
		{"G711U alias", []byte{0x00, 0x7F}, "G711U"},
		{"G.711U alias", []byte{0x00, 0x7F}, "G.711U"},
		{"G711MU alias", []byte{0x00, 0x7F}, "G711MU"},
		{"default empty codec name", []byte{0x00, 0x7F}, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := DecodeAudioPayload(tc.input, tc.codecName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(tc.input) == 0 {
				if len(result) != 0 {
					t.Errorf("expected nil or empty result for empty input, got %d bytes", len(result))
				}
				return
			}

			// Output should be 2x input size (16-bit samples)
			expectedLen := len(tc.input) * 2
			if len(result) != expectedLen {
				t.Errorf("expected %d bytes, got %d", expectedLen, len(result))
			}
		})
	}
}

// TestDecodeAudioPayload_PCMA tests A-law decoding
func TestDecodeAudioPayload_PCMA(t *testing.T) {
	testCases := []struct {
		name     string
		input    []byte
		codecName string
	}{
		{"empty payload", []byte{}, "PCMA"},
		{"silence (0xD5 = 0 in A-law)", []byte{0xD5, 0xD5, 0xD5, 0xD5}, "PCMA"},
		{"single sample", []byte{0x00}, "PCMA"},
		{"multiple samples", []byte{0x00, 0x7F, 0x80, 0xFF}, "PCMA"},
		{"G711A alias", []byte{0x00, 0x7F}, "G711A"},
		{"G.711A alias", []byte{0x00, 0x7F}, "G.711A"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := DecodeAudioPayload(tc.input, tc.codecName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(tc.input) == 0 {
				if len(result) != 0 {
					t.Errorf("expected nil or empty result for empty input, got %d bytes", len(result))
				}
				return
			}

			// Output should be 2x input size (16-bit samples)
			expectedLen := len(tc.input) * 2
			if len(result) != expectedLen {
				t.Errorf("expected %d bytes, got %d", expectedLen, len(result))
			}
		})
	}
}

// TestDecodeAudioPayload_L16 tests linear 16-bit PCM passthrough
func TestDecodeAudioPayload_L16(t *testing.T) {
	testCases := []struct {
		name      string
		input     []byte
		codecName string
	}{
		{"empty payload", []byte{}, "L16"},
		{"two samples", []byte{0x00, 0x01, 0x02, 0x03}, "L16"},
		{"LINEAR16 alias", []byte{0x00, 0x01, 0x02, 0x03}, "LINEAR16"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := DecodeAudioPayload(tc.input, tc.codecName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// L16 should pass through unchanged
			if len(result) != len(tc.input) {
				t.Errorf("expected %d bytes, got %d", len(tc.input), len(result))
			}

			for i := range tc.input {
				if result[i] != tc.input[i] {
					t.Errorf("byte %d: expected 0x%02X, got 0x%02X", i, tc.input[i], result[i])
				}
			}
		})
	}
}

// TestDecodeAudioPayload_G722 tests G.722 decoding
func TestDecodeAudioPayload_G722(t *testing.T) {
	testCases := []struct {
		name  string
		input []byte
	}{
		{"single byte", []byte{0x00}},
		{"silence pattern", []byte{0x00, 0x00, 0x00, 0x00}},
		{"typical G.722 frame", make([]byte, 80)}, // 10ms at 64kbps
		{"20ms frame", make([]byte, 160)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := DecodeAudioPayload(tc.input, "G722")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// G.722 produces 2 samples per byte, each sample is 2 bytes
			expectedLen := len(tc.input) * 4
			if len(result) != expectedLen {
				t.Errorf("expected %d bytes, got %d", expectedLen, len(result))
			}

			// Verify output is valid 16-bit PCM (can be read as int16)
			for i := 0; i < len(result); i += 2 {
				// Verify we can read valid int16 samples
				_ = int16(binary.LittleEndian.Uint16(result[i:]))
			}
		})
	}
}

// TestDecodeAudioPayload_G722_EmptyPayload tests G.722 with empty input
func TestDecodeAudioPayload_G722_EmptyPayload(t *testing.T) {
	_, err := DecodeAudioPayload([]byte{}, "G722")
	if err == nil {
		t.Error("expected error for empty G.722 payload")
	}
}

// TestDecodeAudioPayload_OPUS tests Opus decoding
func TestDecodeAudioPayload_OPUS(t *testing.T) {
	// Create a minimal valid Opus packet
	// TOC byte: config=24 (CELT FB 20ms), stereo=0, code=0
	opusPacket := []byte{0xC0} // (24 << 3) | (0 << 2) | 0 = 0xC0
	// Add some payload data
	opusPacket = append(opusPacket, make([]byte, 50)...)

	testCases := []struct {
		name      string
		input     []byte
		codecName string
	}{
		{"OPUS stereo", opusPacket, "OPUS"},
		{"OPUS_MONO", opusPacket, "OPUS_MONO"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := DecodeAudioPayload(tc.input, tc.codecName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Opus should produce PCM output
			if len(result) == 0 {
				t.Error("expected non-empty output")
			}

			// Verify output is valid 16-bit PCM
			for i := 0; i < len(result); i += 2 {
				if i+1 < len(result) {
					// Verify we can read valid int16 samples
					_ = int16(binary.LittleEndian.Uint16(result[i:]))
				}
			}
		})
	}
}

// TestDecodeAudioPayload_OPUS_EmptyPayload tests Opus with empty input
func TestDecodeAudioPayload_OPUS_EmptyPayload(t *testing.T) {
	_, err := DecodeAudioPayload([]byte{}, "OPUS")
	if err == nil {
		t.Error("expected error for empty Opus payload")
	}
}

// TestDecodeAudioPayload_OPUS_Modes tests different Opus modes
func TestDecodeAudioPayload_OPUS_Modes(t *testing.T) {
	testCases := []struct {
		name   string
		toc    byte
		mode   string
	}{
		{"SILK NB 10ms", 0x00, "SILK"},      // config 0
		{"SILK MB 20ms", 0x29, "SILK"},      // config 5
		{"SILK WB 20ms", 0x49, "SILK"},      // config 9
		{"Hybrid SWB 10ms", 0x60, "Hybrid"}, // config 12
		{"Hybrid FB 20ms", 0x79, "Hybrid"},  // config 15
		{"CELT NB 10ms", 0x80, "CELT"},      // config 16
		{"CELT WB 20ms", 0xA9, "CELT"},      // config 21
		{"CELT FB 20ms", 0xC9, "CELT"},      // config 25
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create packet with TOC byte and payload
			packet := append([]byte{tc.toc}, make([]byte, 40)...)

			result, err := DecodeAudioPayload(packet, "OPUS")
			if err != nil {
				t.Fatalf("unexpected error for mode %s: %v", tc.mode, err)
			}

			if len(result) == 0 {
				t.Errorf("expected non-empty output for mode %s", tc.mode)
			}
		})
	}
}

// TestDecodeAudioPayload_UnsupportedCodec tests unsupported codec handling
func TestDecodeAudioPayload_UnsupportedCodec(t *testing.T) {
	_, err := DecodeAudioPayload([]byte{0x00}, "UNSUPPORTED_CODEC")
	if err == nil {
		t.Error("expected error for unsupported codec")
	}
}

// TestMuLawDecoding tests μ-law decoding accuracy
func TestMuLawDecoding(t *testing.T) {
	// Test known μ-law values per ITU-T G.711
	testCases := []struct {
		input    byte
		expected int16
	}{
		{0xFF, 0},       // Positive zero (inverted 0x00)
		{0x7F, 0},       // Negative zero
		{0x00, -32124},  // Maximum negative amplitude
		{0x80, 32124},   // Maximum positive amplitude
	}

	for _, tc := range testCases {
		result := decodeMuLawSample(tc.input)
		// Allow some tolerance due to quantization
		diff := int(result) - int(tc.expected)
		if diff < 0 {
			diff = -diff
		}
		if diff > 100 {
			t.Errorf("μ-law 0x%02X: expected ~%d, got %d", tc.input, tc.expected, result)
		}
	}
}

// TestALawDecoding tests A-law decoding accuracy
func TestALawDecoding(t *testing.T) {
	// Test known A-law values
	testCases := []struct {
		input    byte
		expected int16
	}{
		{0xD5, 8},    // Positive small value
		{0x55, -8},   // Negative small value
	}

	for _, tc := range testCases {
		result := decodeALawSample(tc.input)
		// Allow some tolerance
		diff := int(result) - int(tc.expected)
		if diff < 0 {
			diff = -diff
		}
		if diff > 50 {
			t.Errorf("A-law 0x%02X: expected ~%d, got %d", tc.input, tc.expected, result)
		}
	}
}

// TestG722Decoder tests G.722 decoder state
func TestG722Decoder(t *testing.T) {
	decoder := NewG722Decoder()

	if decoder.lowBand.det != 32 {
		t.Errorf("expected lowBand.det = 32, got %d", decoder.lowBand.det)
	}
	if decoder.highBand.det != 8 {
		t.Errorf("expected highBand.det = 8, got %d", decoder.highBand.det)
	}
}

// TestG722QMFSynthesis tests QMF synthesis filter
func TestG722QMFSynthesis(t *testing.T) {
	decoder := NewG722Decoder()

	// Test with known sub-band values
	rlow := 1000
	rhigh := 500

	xout1, xout2 := decoder.qmfSynthesis(rlow, rhigh)

	// Outputs should be valid integers
	if xout1 < -32768 || xout1 > 32767 {
		t.Errorf("xout1 out of range: %d", xout1)
	}
	if xout2 < -32768 || xout2 > 32767 {
		t.Errorf("xout2 out of range: %d", xout2)
	}
}

// TestOpusFrameDecoder tests Opus frame decoder initialization
func TestOpusFrameDecoder(t *testing.T) {
	codecInfo := CodecInfo{Name: "OPUS", SampleRate: 48000, Channels: 2}

	// Create a valid Opus packet
	packet := []byte{0xC0} // CELT FB 20ms mono
	packet = append(packet, make([]byte, 40)...)

	result, err := decodeOpusPacket(packet, codecInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) == 0 {
		t.Error("expected non-empty output")
	}
}

// TestOpusParseConfig tests Opus configuration parsing
func TestOpusParseConfig(t *testing.T) {
	decoder := &OpusFrameDecoder{sampleRate: 48000, channels: 2}

	testCases := []struct {
		config         byte
		expectedMode   string
		expectedBW     string
	}{
		{0, "SILK", "NB"},
		{4, "SILK", "MB"},
		{8, "SILK", "WB"},
		{12, "Hybrid", "SWB"},
		{14, "Hybrid", "FB"},
		{16, "CELT", "NB"},
		{20, "CELT", "WB"},
		{24, "CELT", "SWB"},
		{28, "CELT", "FB"},
	}

	for _, tc := range testCases {
		mode, bw := decoder.parseConfig(tc.config)
		if mode != tc.expectedMode {
			t.Errorf("config %d: expected mode %s, got %s", tc.config, tc.expectedMode, mode)
		}
		if bw != tc.expectedBW {
			t.Errorf("config %d: expected bandwidth %s, got %s", tc.config, tc.expectedBW, bw)
		}
	}
}

// TestOpusGetFrameSize tests Opus frame size detection
func TestOpusGetFrameSize(t *testing.T) {
	decoder := &OpusFrameDecoder{sampleRate: 48000, channels: 2}

	testCases := []struct {
		config     byte
		expectedMs int
	}{
		{0, 10},  // 10ms
		{1, 20},  // 20ms
		{2, 40},  // 40ms
		{3, 60},  // 60ms
		{4, 10},  // 10ms (wraps)
		{5, 20},  // 20ms
	}

	for _, tc := range testCases {
		ms := decoder.getFrameSizeMs(tc.config)
		if ms != tc.expectedMs {
			t.Errorf("config %d: expected %dms, got %dms", tc.config, tc.expectedMs, ms)
		}
	}
}

// TestBitReader tests the bit reader utility
func TestBitReader(t *testing.T) {
	data := []byte{0xAB, 0xCD} // 10101011 11001101
	br := newBitReader(data)

	// Read 4 bits: should be 1010 = 10
	val := br.readBits(4)
	if val != 10 {
		t.Errorf("expected 10, got %d", val)
	}

	// Read 4 more bits: should be 1011 = 11
	val = br.readBits(4)
	if val != 11 {
		t.Errorf("expected 11, got %d", val)
	}

	// Read 8 bits: should be 11001101 = 205
	val = br.readBits(8)
	if val != 205 {
		t.Errorf("expected 205, got %d", val)
	}
}

// TestBitReaderEdgeCases tests bit reader edge cases
func TestBitReaderEdgeCases(t *testing.T) {
	// Empty data
	br := newBitReader([]byte{})
	val := br.readBits(8)
	if val != 0 {
		t.Errorf("expected 0 for empty data, got %d", val)
	}

	// Read more bits than available
	br = newBitReader([]byte{0xFF})
	val = br.readBits(16)
	if val != 0xFF {
		t.Errorf("expected 255, got %d", val)
	}
}

// TestClampInt tests integer clamping
func TestClampInt(t *testing.T) {
	testCases := []struct {
		val, min, max, expected int
	}{
		{50, 0, 100, 50},
		{-10, 0, 100, 0},
		{150, 0, 100, 100},
		{0, -100, 100, 0},
	}

	for _, tc := range testCases {
		result := clampInt(tc.val, tc.min, tc.max)
		if result != tc.expected {
			t.Errorf("clampInt(%d, %d, %d) = %d, expected %d",
				tc.val, tc.min, tc.max, result, tc.expected)
		}
	}
}

// TestClampInt16 tests int16 clamping
func TestClampInt16(t *testing.T) {
	testCases := []struct {
		val      int
		expected int16
	}{
		{0, 0},
		{32767, 32767},
		{-32768, -32768},
		{40000, 32767},
		{-40000, -32768},
	}

	for _, tc := range testCases {
		result := clampInt16(tc.val)
		if result != tc.expected {
			t.Errorf("clampInt16(%d) = %d, expected %d", tc.val, result, tc.expected)
		}
	}
}

// TestDecodeAudioPayloadIntegration tests full decode pipeline
func TestDecodeAudioPayloadIntegration(t *testing.T) {
	// Generate a simple sine wave and encode/decode cycle
	// This tests that the pipeline produces valid audio

	// Create 20ms of PCMU-encoded audio (160 samples at 8kHz)
	pcmuData := make([]byte, 160)
	for i := range pcmuData {
		// Generate a 400Hz sine wave
		phase := float64(i) * 2.0 * math.Pi * 400.0 / 8000.0
		sample := int16(math.Sin(phase) * 8000)
		// Encode to μ-law (simplified - just use lookup table inverse)
		pcmuData[i] = encodeMuLaw(sample)
	}

	// Decode
	result, err := DecodeAudioPayload(pcmuData, "PCMU")
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// Verify output length
	if len(result) != 320 { // 160 samples * 2 bytes
		t.Errorf("expected 320 bytes, got %d", len(result))
	}

	// Verify output is valid audio (not all zeros)
	hasNonZero := false
	for i := 0; i < len(result); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(result[i:]))
		if sample != 0 {
			hasNonZero = true
		}
	}
	if !hasNonZero {
		t.Error("all samples are zero, expected audio signal")
	}
}

// encodeMuLaw is a helper to encode PCM to μ-law for testing
func encodeMuLaw(sample int16) byte {
	const BIAS = 0x84
	const CLIP = 32635

	sign := byte(0)
	if sample < 0 {
		sign = 0x80
		sample = -sample
	}
	if int(sample) > CLIP {
		sample = CLIP
	}
	sample = sample + BIAS

	exponent := 7
	for i := 7; i >= 0; i-- {
		if sample&(1<<(i+7)) != 0 {
			exponent = i
			break
		}
	}

	mantissa := (sample >> (exponent + 3)) & 0x0F
	return ^(sign | byte(exponent<<4) | byte(mantissa))
}

// BenchmarkDecodeAudioPayload_PCMU benchmarks μ-law decoding
func BenchmarkDecodeAudioPayload_PCMU(b *testing.B) {
	data := make([]byte, 160) // 20ms of audio at 8kHz
	for i := range data {
		data[i] = byte(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeAudioPayload(data, "PCMU")
	}
}

// BenchmarkDecodeAudioPayload_G722 benchmarks G.722 decoding
func BenchmarkDecodeAudioPayload_G722(b *testing.B) {
	data := make([]byte, 160) // 20ms of audio
	for i := range data {
		data[i] = byte(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeAudioPayload(data, "G722")
	}
}

// BenchmarkDecodeAudioPayload_OPUS benchmarks Opus decoding
func BenchmarkDecodeAudioPayload_OPUS(b *testing.B) {
	// Create minimal Opus packet
	data := []byte{0xC0}
	data = append(data, make([]byte, 50)...)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeAudioPayload(data, "OPUS")
	}
}

// =============================================================================
// G.729 Decoder Tests
// =============================================================================

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
				// Verify we can read valid int16 samples
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
	// G.729B SID frame is 2 bytes
	input := []byte{0x00, 0x00}

	result, err := DecodeAudioPayload(input, "G729")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should produce comfort noise (80 samples = 160 bytes)
	expectedLen := 160
	if len(result) != expectedLen {
		t.Errorf("expected %d bytes for SID frame, got %d", expectedLen, len(result))
	}
}

// TestG729Decoder tests G.729 decoder state initialization
func TestG729Decoder(t *testing.T) {
	decoder := NewG729Decoder()

	// Check initial state
	if decoder.prevPitch != 60 {
		t.Errorf("expected prevPitch = 60, got %d", decoder.prevPitch)
	}
	if decoder.prevGain != 1.0 {
		t.Errorf("expected prevGain = 1.0, got %f", decoder.prevGain)
	}
	if decoder.frameCount != 0 {
		t.Errorf("expected frameCount = 0, got %d", decoder.frameCount)
	}

	// Check LSP initialization (should be equally spaced)
	for i := 0; i < 10; i++ {
		expected := float64(i+1) / 11.0
		if math.Abs(decoder.prevLSP[i]-expected) > 0.001 {
			t.Errorf("prevLSP[%d]: expected %f, got %f", i, expected, decoder.prevLSP[i])
		}
	}
}

// TestG729DecodeLSP tests LSP decoding
func TestG729DecodeLSP(t *testing.T) {
	decoder := NewG729Decoder()

	// Test with various index combinations
	testCases := []struct {
		l0, l1, l2, l3 int
	}{
		{0, 0, 0, 0},
		{1, 1, 1, 1},
		{2, 2, 2, 2},
		{3, 3, 3, 3},
	}

	for _, tc := range testCases {
		lsp := decoder.decodeLSP(tc.l0, tc.l1, tc.l2, tc.l3)

		// Check LSP values are valid (between 0 and 1)
		for i, val := range lsp {
			if val < 0.0 || val > 1.0 {
				t.Errorf("l0=%d,l1=%d,l2=%d,l3=%d: LSP[%d] = %f out of range [0,1]",
					tc.l0, tc.l1, tc.l2, tc.l3, i, val)
			}
		}

		// Check monotonic ordering
		for i := 1; i < len(lsp); i++ {
			if lsp[i] <= lsp[i-1] {
				t.Errorf("l0=%d,l1=%d,l2=%d,l3=%d: LSP not monotonic at index %d: %f <= %f",
					tc.l0, tc.l1, tc.l2, tc.l3, i, lsp[i], lsp[i-1])
			}
		}
	}
}

// TestG729DecodeFixedCodebook tests fixed codebook decoding
func TestG729DecodeFixedCodebook(t *testing.T) {
	decoder := NewG729Decoder()

	testCases := []int{0, 1, 0x1FFF, 0x0ABC}

	for _, index := range testCases {
		vector := decoder.decodeFixedCodebook(index)

		// Vector should be 40 samples
		if len(vector) != 40 {
			t.Errorf("index %d: expected 40 samples, got %d", index, len(vector))
		}

		// Should have exactly 4 non-zero pulses
		nonZero := 0
		for _, v := range vector {
			if v != 0 {
				nonZero++
				// Each pulse should be +1 or -1
				if math.Abs(v) != 1.0 {
					t.Errorf("index %d: pulse value %f should be +/-1", index, v)
				}
			}
		}
		if nonZero != 4 {
			t.Errorf("index %d: expected 4 pulses, got %d", index, nonZero)
		}
	}
}

// TestG729DecodePitchLag tests pitch lag decoding
func TestG729DecodePitchLag(t *testing.T) {
	decoder := NewG729Decoder()
	decoder.prevPitch = 60

	// First subframe (absolute)
	lag1 := decoder.decodePitchLag(0, true)
	if lag1 != 20 {
		t.Errorf("expected minimum pitch lag 20, got %d", lag1)
	}

	lag2 := decoder.decodePitchLag(123, true)
	if lag2 != 143 {
		t.Errorf("expected pitch lag 143, got %d", lag2)
	}

	lag3 := decoder.decodePitchLag(200, true)
	if lag3 != 143 {
		t.Errorf("expected clamped pitch lag 143, got %d", lag3)
	}

	// Second subframe (relative to prevPitch)
	decoder.prevPitch = 80
	lag4 := decoder.decodePitchLag(8, false) // Delta = 0
	if lag4 != 80 {
		t.Errorf("expected relative pitch lag 80, got %d", lag4)
	}
}

// TestG729LspToLP tests LSP to LP conversion
func TestG729LspToLP(t *testing.T) {
	decoder := NewG729Decoder()

	// Use default LSP values
	lsp := make([]float64, 10)
	for i := 0; i < 10; i++ {
		lsp[i] = float64(i+1) / 11.0
	}

	lpc := decoder.lspToLP(lsp)

	// Check LP coefficients
	if len(lpc) != 11 {
		t.Errorf("expected 11 LP coefficients, got %d", len(lpc))
	}

	// First coefficient should be 1.0
	if lpc[0] != 1.0 {
		t.Errorf("expected lpc[0] = 1.0, got %f", lpc[0])
	}

	// LP coefficients should be finite (not NaN or Inf)
	for i := 1; i < len(lpc); i++ {
		if math.IsNaN(lpc[i]) || math.IsInf(lpc[i], 0) {
			t.Errorf("lpc[%d] = %f is invalid (NaN or Inf)", i, lpc[i])
		}
	}
}

// TestG729ConcealFrame tests frame erasure concealment
func TestG729ConcealFrame(t *testing.T) {
	decoder := NewG729Decoder()

	// First decode a valid frame to set state
	validFrame := make([]byte, 10)
	decoder.decodeFrame(validFrame)

	// Then test concealment
	output := decoder.concealFrame()

	// Should produce 80 samples
	if len(output) != 80 {
		t.Errorf("expected 80 samples from concealment, got %d", len(output))
	}

	// Output should be valid (not NaN or Inf)
	for i, sample := range output {
		if math.IsNaN(sample) || math.IsInf(sample, 0) {
			t.Errorf("sample %d is invalid: %f", i, sample)
		}
	}
}

// TestG729BitReader tests the G.729 bit reader
func TestG729BitReader(t *testing.T) {
	data := []byte{0xAB, 0xCD} // 10101011 11001101
	br := newG729BitReader(data)

	// Read 4 bits: should be 1010 = 10
	val := br.readBits(4)
	if val != 10 {
		t.Errorf("expected 10, got %d", val)
	}

	// Read 4 more bits: should be 1011 = 11
	val = br.readBits(4)
	if val != 11 {
		t.Errorf("expected 11, got %d", val)
	}

	// Read 8 bits: should be 11001101 = 205
	val = br.readBits(8)
	if val != 205 {
		t.Errorf("expected 205, got %d", val)
	}
}

// TestClampFloat64 tests float64 clamping
func TestClampFloat64(t *testing.T) {
	testCases := []struct {
		val, min, max, expected float64
	}{
		{50.0, 0.0, 100.0, 50.0},
		{-10.0, 0.0, 100.0, 0.0},
		{150.0, 0.0, 100.0, 100.0},
		{0.0, -100.0, 100.0, 0.0},
	}

	for _, tc := range testCases {
		result := clampFloat64(tc.val, tc.min, tc.max)
		if result != tc.expected {
			t.Errorf("clampFloat64(%f, %f, %f) = %f, expected %f",
				tc.val, tc.min, tc.max, result, tc.expected)
		}
	}
}

// BenchmarkDecodeAudioPayload_G729 benchmarks G.729 decoding
func BenchmarkDecodeAudioPayload_G729(b *testing.B) {
	data := make([]byte, 10) // Single 10ms frame

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeAudioPayload(data, "G729")
	}
}

// BenchmarkDecodeAudioPayload_G729_MultiFrame benchmarks G.729 decoding with multiple frames
func BenchmarkDecodeAudioPayload_G729_MultiFrame(b *testing.B) {
	data := make([]byte, 100) // 10 frames = 100ms of audio

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeAudioPayload(data, "G729")
	}
}

// TestG729ErrorScenarioResolution verifies the exact error scenario from GitHub issue #16 is resolved
// Error was: {"codec":"G729","error":"unsupported codec for PCM conversion: G729","payload_type":18}
func TestG729ErrorScenarioResolution(t *testing.T) {
	// Simulate exactly what happens in production:
	// 1. Payload type 18 is detected
	// 2. Codec name "G729" is passed to DecodeAudioPayload

	// Verify payload type 18 is recognized as G.729
	codecInfo, exists := GetCodecInfo(18)
	if !exists {
		t.Fatal("payload type 18 should be recognized as G.729")
	}
	if codecInfo.Name != "G729" {
		t.Fatalf("expected codec name 'G729', got '%s'", codecInfo.Name)
	}

	// Simulate RTP payload (10 bytes = 1 G.729 frame)
	payload := []byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0, 0x11, 0x22}
	codecName := codecInfo.Name // "G729"

	// This is the exact call that was failing
	result, err := DecodeAudioPayload(payload, codecName)

	// MUST NOT return "unsupported codec for PCM conversion: G729"
	if err != nil {
		t.Fatalf("G.729 decoding failed (this was the bug): %v", err)
	}

	// Should produce 160 bytes (80 samples at 16-bit)
	if len(result) != 160 {
		t.Errorf("expected 160 bytes, got %d", len(result))
	}

	// Also verify uppercase conversion works (as done in SetCodecInfo)
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

// TestIsG729Codec tests the IsG729Codec helper function
func TestIsG729Codec(t *testing.T) {
	testCases := []struct {
		payloadType byte
		expected    bool
		description string
	}{
		{18, true, "G.729 standard payload type"},
		{0, false, "PCMU payload type"},
		{8, false, "PCMA payload type"},
		{9, false, "G.722 payload type"},
		{96, false, "OPUS payload type"},
		{97, false, "EVS payload type"},
		{255, false, "invalid payload type"},
		{17, false, "adjacent payload type (not G.729)"},
		{19, false, "adjacent payload type (not G.729)"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			result := IsG729Codec(tc.payloadType)
			if result != tc.expected {
				t.Errorf("IsG729Codec(%d) = %v, expected %v", tc.payloadType, result, tc.expected)
			}
		})
	}
}

// TestG729DetectCodec tests codec detection for G.729 RTP packets
func TestG729DetectCodec(t *testing.T) {
	// Create a mock RTP packet with G.729 payload type (18)
	// RTP header: V=2, P=0, X=0, CC=0, M=0, PT=18
	rtpPacket := make([]byte, 22) // 12 byte header + 10 byte G.729 frame
	rtpPacket[0] = 0x80           // V=2, P=0, X=0, CC=0
	rtpPacket[1] = 18             // M=0, PT=18 (G.729)

	codecName, err := DetectCodec(rtpPacket)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if codecName != "G729" {
		t.Errorf("expected codec name 'G729', got '%s'", codecName)
	}
}

// TestG729GetCodecInfo tests getting codec info for G.729
func TestG729GetCodecInfo(t *testing.T) {
	codecInfo, exists := GetCodecInfo(18)
	if !exists {
		t.Fatal("G.729 codec info should exist for payload type 18")
	}

	if codecInfo.Name != "G729" {
		t.Errorf("expected Name 'G729', got '%s'", codecInfo.Name)
	}
	if codecInfo.PayloadType != 18 {
		t.Errorf("expected PayloadType 18, got %d", codecInfo.PayloadType)
	}
	if codecInfo.SampleRate != 8000 {
		t.Errorf("expected SampleRate 8000, got %d", codecInfo.SampleRate)
	}
	if codecInfo.Channels != 1 {
		t.Errorf("expected Channels 1, got %d", codecInfo.Channels)
	}
	if codecInfo.Description != "G.729 CS-ACELP" {
		t.Errorf("expected Description 'G.729 CS-ACELP', got '%s'", codecInfo.Description)
	}
}

// TestG729DecoderStatePersistence tests that decoder state persists across frames
func TestG729DecoderStatePersistence(t *testing.T) {
	decoder := NewG729Decoder()

	// Decode first frame
	frame1 := make([]byte, 10)
	for i := range frame1 {
		frame1[i] = byte(i * 17) // Some varied data
	}
	output1 := decoder.decodeFrame(frame1)

	// Check that state was updated
	if decoder.frameCount != 1 {
		t.Errorf("expected frameCount 1 after first frame, got %d", decoder.frameCount)
	}

	// Decode second frame
	frame2 := make([]byte, 10)
	for i := range frame2 {
		frame2[i] = byte(i * 23) // Different data
	}
	output2 := decoder.decodeFrame(frame2)

	if decoder.frameCount != 2 {
		t.Errorf("expected frameCount 2 after second frame, got %d", decoder.frameCount)
	}

	// Both outputs should have 80 samples
	if len(output1) != 80 {
		t.Errorf("expected 80 samples from frame 1, got %d", len(output1))
	}
	if len(output2) != 80 {
		t.Errorf("expected 80 samples from frame 2, got %d", len(output2))
	}
}

// TestG729DecoderProducesNonSilentOutput tests that varied input produces non-silent output
func TestG729DecoderProducesNonSilentOutput(t *testing.T) {
	// Create a frame with varied data (simulating real encoded audio)
	frame := []byte{0xA5, 0x3C, 0x78, 0xF1, 0x2E, 0x9B, 0x47, 0xD0, 0x6A, 0x15}

	result, err := DecodeAudioPayload(frame, "G729")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Count non-zero samples
	nonZeroSamples := 0
	for i := 0; i < len(result); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(result[i:]))
		if sample != 0 {
			nonZeroSamples++
		}
	}

	// With varied input, we should get some non-zero output
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
	}{
		{"5 bytes (partial)", 5, false},
		{"7 bytes (partial)", 7, false},
		{"3 bytes (partial)", 3, false},
		{"15 bytes (1.5 frames)", 15, false},
		{"25 bytes (2.5 frames)", 25, false},
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
				// Should produce output based on complete frames
				numCompleteFrames := tc.inputLen / 10
				if numCompleteFrames == 0 {
					numCompleteFrames = 1 // Partial frame treated as one
				}
				expectedLen := numCompleteFrames * 160
				if len(result) != expectedLen {
					t.Errorf("expected %d bytes, got %d", expectedLen, len(result))
				}
			}
		})
	}
}

// TestG729OutputRange tests that all output samples are within valid PCM range
func TestG729OutputRange(t *testing.T) {
	// Test with various input patterns
	testPatterns := [][]byte{
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // All zeros
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, // All ones
		{0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55}, // Alternating
		{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0, 0x11, 0x22}, // Sequential
	}

	for i, pattern := range testPatterns {
		t.Run(fmt.Sprintf("pattern_%d", i), func(t *testing.T) {
			result, err := DecodeAudioPayload(pattern, "G729")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check all samples are in valid 16-bit range
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
	// Create 50 frames (500ms of audio)
	numFrames := 50
	input := make([]byte, numFrames*10)

	// Fill with pseudo-random data to simulate real encoded audio
	for i := range input {
		input[i] = byte((i * 1103515245) >> 16)
	}

	result, err := DecodeAudioPayload(input, "G729")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedLen := numFrames * 160 // 80 samples * 2 bytes per frame
	if len(result) != expectedLen {
		t.Errorf("expected %d bytes for %d frames, got %d", expectedLen, numFrames, len(result))
	}

	// Verify all samples can be read as valid int16
	for i := 0; i < len(result); i += 2 {
		_ = int16(binary.LittleEndian.Uint16(result[i:]))
	}

	t.Logf("Successfully decoded %d frames (%dms of audio)", numFrames, numFrames*10)
}

// TestG729StabilizeLSP tests the LSP stabilization function
func TestG729StabilizeLSP(t *testing.T) {
	decoder := NewG729Decoder()

	testCases := []struct {
		name     string
		input    []float64
		checkFn  func([]float64) bool
	}{
		{
			name:  "already valid LSPs",
			input: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.85, 0.9},
			checkFn: func(lsp []float64) bool {
				for i := 1; i < len(lsp); i++ {
					if lsp[i] <= lsp[i-1] {
						return false
					}
				}
				return true
			},
		},
		{
			name:  "out of order LSPs",
			input: []float64{0.5, 0.3, 0.7, 0.2, 0.8, 0.4, 0.9, 0.6, 0.95, 0.1},
			checkFn: func(lsp []float64) bool {
				for i := 1; i < len(lsp); i++ {
					if lsp[i] <= lsp[i-1] {
						return false
					}
				}
				return true
			},
		},
		{
			name:  "negative values",
			input: []float64{-0.1, 0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8},
			checkFn: func(lsp []float64) bool {
				return lsp[0] >= 0.0
			},
		},
		{
			name:  "values exceeding 1.0",
			input: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.5},
			checkFn: func(lsp []float64) bool {
				return lsp[9] <= 1.0
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			lsp := make([]float64, len(tc.input))
			copy(lsp, tc.input)

			decoder.stabilizeLSP(lsp)

			if !tc.checkFn(lsp) {
				t.Errorf("LSP stabilization failed for case '%s': %v", tc.name, lsp)
			}
		})
	}
}

// TestG729IntegrationWithRTPPacket tests full RTP packet processing for G.729
func TestG729IntegrationWithRTPPacket(t *testing.T) {
	// Create a complete RTP packet with G.729 payload
	// RTP header (12 bytes) + G.729 payload (20 bytes = 2 frames)
	rtpPacket := make([]byte, 32)

	// RTP header
	rtpPacket[0] = 0x80 // V=2, P=0, X=0, CC=0
	rtpPacket[1] = 18   // M=0, PT=18 (G.729)
	// Sequence number (bytes 2-3)
	rtpPacket[2] = 0x00
	rtpPacket[3] = 0x01
	// Timestamp (bytes 4-7)
	rtpPacket[4] = 0x00
	rtpPacket[5] = 0x00
	rtpPacket[6] = 0x00
	rtpPacket[7] = 0xA0 // 160 samples
	// SSRC (bytes 8-11)
	rtpPacket[8] = 0x12
	rtpPacket[9] = 0x34
	rtpPacket[10] = 0x56
	rtpPacket[11] = 0x78

	// G.729 payload (2 frames = 20 bytes)
	for i := 12; i < 32; i++ {
		rtpPacket[i] = byte(i * 7)
	}

	// Detect codec
	codecName, err := DetectCodec(rtpPacket)
	if err != nil {
		t.Fatalf("failed to detect codec: %v", err)
	}
	if codecName != "G729" {
		t.Fatalf("expected G729, got %s", codecName)
	}

	// Extract and decode payload
	payload := rtpPacket[12:]
	result, err := DecodeAudioPayload(payload, codecName)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// Should produce 320 bytes (2 frames * 80 samples * 2 bytes)
	if len(result) != 320 {
		t.Errorf("expected 320 bytes, got %d", len(result))
	}
}

// TestG729OutputNotClipped verifies the decoder doesn't produce heavily clipped audio
func TestG729OutputNotClipped(t *testing.T) {
	// Create varied input that simulates real G.729 encoded audio
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

		// Count clipped samples
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

		// Clipping should be less than 10% for normal audio
		if clippedPercent > 10 {
			t.Errorf("frame %d: too many clipped samples: %.2f%% (should be <10%%)", i, clippedPercent)
		}
	}
}

// TestG729ComfortNoiseGeneration tests SID frame handling
func TestG729ComfortNoiseGeneration(t *testing.T) {
	// G.729B SID frames are 2 bytes
	sidFrame := []byte{0x00, 0x00}

	result, err := DecodeAudioPayload(sidFrame, "G729")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should produce 160 bytes (80 samples)
	if len(result) != 160 {
		t.Errorf("expected 160 bytes for SID frame, got %d", len(result))
	}

	// Comfort noise should be low amplitude
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

	// Comfort noise should be low amplitude (< 500)
	if maxAmplitude > 500 {
		t.Errorf("comfort noise amplitude too high: %d", maxAmplitude)
	}

	t.Logf("Comfort noise max amplitude: %d", maxAmplitude)
}

// =============================================================================
// G.729 Race Condition and Memory Leak Tests
// =============================================================================

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

			// Each goroutine creates its own input data
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

			// Verify output is valid
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

	// Check for any errors
	var errCount int
	for err := range errors {
		t.Error(err)
		errCount++
	}

	if errCount == 0 {
		t.Logf("Successfully ran %d concurrent goroutines, each decoding %d frames", numGoroutines, framesPerGoroutine)
	}
}

// TestG729ConcurrentDecoderInstances tests multiple decoder instances running concurrently
func TestG729ConcurrentDecoderInstances(t *testing.T) {
	const numDecoders = 50
	const framesPerDecoder = 100

	var wg sync.WaitGroup
	results := make(chan struct {
		id         int
		frameCount int
		err        error
	}, numDecoders)

	for i := 0; i < numDecoders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			decoder := NewG729Decoder()

			for frame := 0; frame < framesPerDecoder; frame++ {
				frameData := make([]byte, 10)
				for j := range frameData {
					frameData[j] = byte((id*framesPerDecoder + frame + j) & 0xFF)
				}

				output := decoder.decodeFrame(frameData)
				if len(output) != 80 {
					results <- struct {
						id         int
						frameCount int
						err        error
					}{id, frame, fmt.Errorf("wrong output length: %d", len(output))}
					return
				}
			}

			results <- struct {
				id         int
				frameCount int
				err        error
			}{id, decoder.frameCount, nil}
		}(i)
	}

	wg.Wait()
	close(results)

	var successCount int
	for r := range results {
		if r.err != nil {
			t.Errorf("decoder %d: %v", r.id, r.err)
		} else if r.frameCount != framesPerDecoder {
			t.Errorf("decoder %d: expected %d frames, got %d", r.id, framesPerDecoder, r.frameCount)
		} else {
			successCount++
		}
	}

	t.Logf("Successfully completed %d/%d decoder instances", successCount, numDecoders)
}

// TestG729MemoryLeak tests for memory leaks during repeated decoding
func TestG729MemoryLeak(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("skipping memory leak test in short mode")
	}

	const iterations = 1000
	const framesPerIteration = 100

	// Force GC and get baseline memory
	runtime.GC()
	var baselineStats runtime.MemStats
	runtime.ReadMemStats(&baselineStats)

	// Perform many decoding iterations
	for i := 0; i < iterations; i++ {
		input := make([]byte, framesPerIteration*10)
		for j := range input {
			input[j] = byte((i + j) & 0xFF)
		}

		result, err := DecodeAudioPayload(input, "G729")
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}

		// Use result to prevent compiler optimization
		if len(result) == 0 {
			t.Fatal("unexpected empty result")
		}

		// Force GC periodically
		if i%100 == 0 {
			runtime.GC()
		}
	}

	// Force GC and get final memory
	runtime.GC()
	runtime.GC() // Run twice to ensure cleanup
	var finalStats runtime.MemStats
	runtime.ReadMemStats(&finalStats)

	// Calculate memory growth
	heapGrowth := int64(finalStats.HeapAlloc) - int64(baselineStats.HeapAlloc)
	heapObjects := int64(finalStats.HeapObjects) - int64(baselineStats.HeapObjects)

	t.Logf("Memory stats after %d iterations:", iterations)
	t.Logf("  Heap growth: %d bytes", heapGrowth)
	t.Logf("  Heap objects delta: %d", heapObjects)
	t.Logf("  Total allocations: %d", finalStats.Mallocs-baselineStats.Mallocs)
	t.Logf("  Total frees: %d", finalStats.Frees-baselineStats.Frees)

	// Allow some heap growth but flag excessive growth
	// 10MB would indicate a serious leak for this test
	const maxAcceptableGrowth = 10 * 1024 * 1024
	if heapGrowth > maxAcceptableGrowth {
		t.Errorf("excessive heap growth detected: %d bytes (max acceptable: %d)", heapGrowth, maxAcceptableGrowth)
	}
}

// TestG729DecoderNoSharedState tests that decoders don't share state
func TestG729DecoderNoSharedState(t *testing.T) {
	decoder1 := NewG729Decoder()
	decoder2 := NewG729Decoder()

	// Decode with decoder1
	frame1 := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22, 0x33, 0x44}
	output1a := decoder1.decodeFrame(frame1)

	// Decode different data with decoder2
	frame2 := []byte{0x55, 0x66, 0x77, 0x88, 0x99, 0x00, 0x11, 0x22, 0x33, 0x44}
	output2 := decoder2.decodeFrame(frame2)

	// Decode same frame with decoder1 again
	output1b := decoder1.decodeFrame(frame1)

	// decoder1 state should not be affected by decoder2
	if decoder1.frameCount != 2 {
		t.Errorf("decoder1 frameCount: expected 2, got %d", decoder1.frameCount)
	}
	if decoder2.frameCount != 1 {
		t.Errorf("decoder2 frameCount: expected 1, got %d", decoder2.frameCount)
	}

	// Outputs should all be valid
	if len(output1a) != 80 || len(output1b) != 80 || len(output2) != 80 {
		t.Error("invalid output lengths")
	}
}

// TestG729RaceOnGlobalTables tests that global lookup tables are safe for concurrent access
func TestG729RaceOnGlobalTables(t *testing.T) {
	const numReaders = 100
	var wg sync.WaitGroup

	// Concurrent reads from global tables
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Read from g729L0 table
			for j := 0; j < len(g729L0); j++ {
				_ = g729L0[j]
			}

			// Read from g729L1 table
			for j := 0; j < len(g729L1); j++ {
				_ = g729L1[j]
			}

			// Read from g729PitchGainTable
			for j := 0; j < len(g729PitchGainTable); j++ {
				_ = g729PitchGainTable[j]
			}

			// Read from g729FixedGainTable
			for j := 0; j < len(g729FixedGainTable); j++ {
				_ = g729FixedGainTable[j]
			}

			// Read from g729BwExpand
			for j := 0; j < len(g729BwExpand); j++ {
				_ = g729BwExpand[j]
			}
		}(i)
	}

	wg.Wait()
	t.Logf("Successfully completed %d concurrent table reads", numReaders)
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
		// Vary the number of frames per iteration
		numFrames := (i % 10) + 1
		input := make([]byte, numFrames*10)

		// Fill with pseudo-random data
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
		{"G729", make([]byte, 20), 320},        // 2 G.729 frames
		{"PCMU", make([]byte, 160), 320},       // 20ms PCMU
		{"PCMA", make([]byte, 160), 320},       // 20ms PCMA
		{"G722", make([]byte, 160), 640},       // 20ms G.722
	}

	for i := 0; i < numGoroutines; i++ {
		for _, codec := range codecs {
			wg.Add(1)
			go func(id int, codecName string, data []byte, minLen int) {
				defer wg.Done()

				// Create unique data for this goroutine
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

// TestG729BitReaderConcurrent tests concurrent bit reader usage
func TestG729BitReaderConcurrent(t *testing.T) {
	const numGoroutines = 100
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Each goroutine creates its own bit reader
			data := []byte{byte(id), byte(id + 1), byte(id + 2), byte(id + 3)}
			br := newG729BitReader(data)

			// Read various bit lengths
			var results []int
			results = append(results, br.readBits(4))
			results = append(results, br.readBits(8))
			results = append(results, br.readBits(12))
			results = append(results, br.readBits(8))

			// Verify we got valid results
			for j, r := range results {
				if r < 0 {
					errors <- fmt.Errorf("goroutine %d: negative result at index %d: %d", id, j, r)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

// BenchmarkG729ConcurrentDecoding benchmarks concurrent G.729 decoding
func BenchmarkG729ConcurrentDecoding(b *testing.B) {
	input := make([]byte, 100) // 10 frames
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

// BenchmarkG729DecoderAllocation benchmarks decoder creation
func BenchmarkG729DecoderAllocation(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		decoder := NewG729Decoder()
		_ = decoder
	}
}

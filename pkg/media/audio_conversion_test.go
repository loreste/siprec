package media

import (
	"encoding/binary"
	"math"
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
				if result != nil && len(result) != 0 {
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
				if result != nil && len(result) != 0 {
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
				sample := int16(binary.LittleEndian.Uint16(result[i:]))
				if sample < -32768 || sample > 32767 {
					t.Errorf("sample %d out of range: %d", i/2, sample)
				}
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
					sample := int16(binary.LittleEndian.Uint16(result[i:]))
					if sample < -32768 || sample > 32767 {
						t.Errorf("sample %d out of range: %d", i/2, sample)
					}
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

	// Verify output is valid audio (not all zeros, within range)
	hasNonZero := false
	for i := 0; i < len(result); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(result[i:]))
		if sample != 0 {
			hasNonZero = true
		}
		if sample < -32768 || sample > 32767 {
			t.Errorf("sample out of range: %d", sample)
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

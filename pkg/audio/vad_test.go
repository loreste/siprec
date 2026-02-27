package audio

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVADEnergyCalculation(t *testing.T) {
	config := ProcessingConfig{
		SampleRate:   8000,
		FrameSize:    160,
		VADThreshold: 0.01,
		BufferSize:   2048,
	}
	vad := NewVoiceActivityDetector(config)

	// Create a silent frame (all zeros)
	silence := make([]byte, 320) // 160 samples * 2 bytes
	energy := vad.calculateEnergy(silence)
	assert.Equal(t, 0.0, energy, "Energy of silence should be 0")

	// Create a "loud" frame (max amplitude)
	loud := make([]byte, 320)
	for i := 0; i < 320; i += 2 {
		loud[i] = 0xFF   // Little endian low byte
		loud[i+1] = 0x7F // Little endian high byte (~32767)
	}
	energy = vad.calculateEnergy(loud)
	assert.Greater(t, energy, 0.9, "Energy of max amplitude should be close to 1.0")
}

func TestVADThresholdAndHold(t *testing.T) {
	config := ProcessingConfig{
		SampleRate:   8000,
		FrameSize:    160,
		VADThreshold: 0.1, // Set a distinct threshold
		VADHoldTime:  2,   // Hold for 2 frames
		BufferSize:   2048,
	}
	vad := NewVoiceActivityDetector(config)

	// 1. Initial State: Silence
	silence := make([]byte, 320)
	_, _ = vad.Process(silence)
	assert.False(t, vad.IsVoiceActive(), "Should not detect voice in initial silence")

	// 2. Active Speech: High energy
	loud := make([]byte, 320)
	for i := 0; i < 320; i += 2 {
		loud[i] = 0x00
		loud[i+1] = 0x40 // ~16384 (Half valid range) => ~0.25 energy
	}
	_, _ = vad.Process(loud)
	assert.True(t, vad.IsVoiceActive(), "Should detect voice for loud frame")
	assert.Equal(t, 2, vad.holdCounter, "Hold counter should be set to holdTime")

	// 3. Silence (Hold Period Frame 1)
	_, _ = vad.Process(silence)
	assert.True(t, vad.IsVoiceActive(), "Should still be active in hold period (1/2)")
	assert.Equal(t, 1, vad.holdCounter, "Hold counter should decrement")

	// 4. Silence (Hold Period Frame 2)
	_, _ = vad.Process(silence)
	assert.True(t, vad.IsVoiceActive(), "Should still be active in hold period (2/2)")
	assert.Equal(t, 0, vad.holdCounter, "Hold counter should reach 0")

	// 5. Silence (After Hold)
	_, _ = vad.Process(silence)
	assert.False(t, vad.IsVoiceActive(), "Should be inactive after hold period expires")
}

func TestComfortNoiseGeneration(t *testing.T) {
	config := ProcessingConfig{
		SampleRate:   8000,
		FrameSize:    160,
		VADThreshold: 0.5,
		BufferSize:   2048,
	}
	vad := NewVoiceActivityDetector(config)
	vad.SetSilenceSuppression(true)
	vad.noiseFloor = 0.1 // Set a known noise floor

	// Process silence
	silence := make([]byte, 320)
	output, err := vad.Process(silence)
	assert.NoError(t, err)

	// Should return comfort noise
	assert.NotEqual(t, silence, output, "Output should not be pure silence")
	assert.Equal(t, 16*2, len(output), "Comfort noise length should be 16 samples (32 bytes)")

	// Check that comfort noise is not empty/zero
	isZero := true
	for _, b := range output {
		if b != 0 {
			isZero = false
			break
		}
	}
	assert.False(t, isZero, "Comfort noise should have non-zero content")
}

func TestVADReset(t *testing.T) {
	config := ProcessingConfig{
		SampleRate:   8000,
		FrameSize:    160,
		VADThreshold: 0.1,
	}
	vad := NewVoiceActivityDetector(config)

	// Simulate activity
	loud := make([]byte, 320)
	for i := 0; i < 320; i += 2 {
		loud[i+1] = 0x40
	}
	_, _ = vad.Process(loud)
	assert.True(t, vad.IsVoiceActive())

	// Reset
	vad.Reset()

	// Verify state is reset
	assert.False(t, vad.IsVoiceActive(), "Voice active state should be reset")
	assert.Equal(t, 0.0, vad.avgEnergy, "Average energy should be reset")
	assert.Equal(t, 0, vad.holdCounter, "Hold counter should be reset")
}

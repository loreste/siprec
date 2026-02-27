package audio

import (
	"context"
	"math"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestAudioEnhancer_NewAudioEnhancer(t *testing.T) {
	logger := logrus.New()
	config := DefaultAudioEnhancementConfig()
	
	enhancer := NewAudioEnhancer(logger, config)
	assert.NotNil(t, enhancer)
	assert.Equal(t, config, enhancer.config)
	assert.NotNil(t, enhancer.agc)
	assert.NotNil(t, enhancer.echo)
	assert.NotNil(t, enhancer.compressor)
}

func TestAudioEnhancer_ProcessAudio(t *testing.T) {
	logger := logrus.New()
	config := &AudioEnhancementConfig{
		AGC: AGCConfig{
			Enabled:     true,
			TargetLevel: -20,
		},
		Compression: CompressionConfig{
			Enabled:   true,
			Threshold: -20,
		},
	}
	
	enhancer := NewAudioEnhancer(logger, config)
	
	// Create test audio with varying amplitude
	samples := make([]float64, 1024)
	for i := range samples {
		// Create signal with varying amplitude
		amplitude := 0.1 + 0.8*float64(i)/float64(len(samples))
		samples[i] = amplitude * math.Sin(2*math.Pi*440*float64(i)/8000)
	}
	
	ctx := context.Background()
	processed, err := enhancer.ProcessAudio(ctx, samples)
	assert.NoError(t, err)
	assert.NotNil(t, processed)
	assert.Len(t, processed, len(samples))
	
	// Check that processing was applied
	different := false
	for i := range samples {
		if samples[i] != processed[i] {
			different = true
			break
		}
	}
	assert.True(t, different, "Processed audio should differ from input")
}

func TestAudioEnhancer_ProcessBasic(t *testing.T) {
	logger := logrus.New()
	config := DefaultAudioEnhancementConfig()
	config.AGC.Enabled = true
	config.EchoCancellation.Enabled = false // Disable echo cancellation for basic test
	config.Compression.Enabled = true
	
	enhancer := NewAudioEnhancer(logger, config)
	
	// Create test signal
	samples := make([]float64, 1024)
	for i := range samples {
		samples[i] = 0.5 * math.Sin(2*math.Pi*440*float64(i)/8000)
	}
	
	ctx := context.Background()
	processed, err := enhancer.ProcessAudio(ctx, samples)
	assert.NoError(t, err)
	assert.NotNil(t, processed)
	assert.Len(t, processed, len(samples))
}

func TestAudioEnhancer_AllFeaturesEnabled(t *testing.T) {
	logger := logrus.New()
	config := &AudioEnhancementConfig{
		AGC: AGCConfig{
			Enabled:     true,
			TargetLevel: -20,
		},
		EchoCancellation: EchoCancellationConfig{
			Enabled:      true,
			FilterLength: 256,
		},
		Compression: CompressionConfig{
			Enabled:   true,
			Threshold: -20,
		},
	}
	
	enhancer := NewAudioEnhancer(logger, config)
	
	// Complex test signal
	samples := make([]float64, 1024)
	for i := range samples {
		// Mix of frequencies with varying amplitude
		amplitude := 0.2 + 0.6*float64(i%200)/200
		low := amplitude * 0.3 * math.Sin(2*math.Pi*200*float64(i)/8000)
		mid := amplitude * 0.4 * math.Sin(2*math.Pi*1000*float64(i)/8000)
		high := amplitude * 0.3 * math.Sin(2*math.Pi*3000*float64(i)/8000)
		samples[i] = low + mid + high
	}
	
	ctx := context.Background()
	processed, err := enhancer.ProcessAudio(ctx, samples)
	assert.NoError(t, err)
	assert.NotNil(t, processed)
	assert.Len(t, processed, len(samples))
	
	// All enhancements should be applied
	assert.NotEqual(t, samples, processed)
}

func BenchmarkAudioEnhancer_ProcessAudio(b *testing.B) {
	logger := logrus.New()
	config := &AudioEnhancementConfig{
		AGC: AGCConfig{Enabled: true},
		EchoCancellation: EchoCancellationConfig{Enabled: true},
		Compression: CompressionConfig{Enabled: true},
	}
	
	enhancer := NewAudioEnhancer(logger, config)
	samples := make([]float64, 1024)
	for i := range samples {
		samples[i] = math.Sin(2*math.Pi*1000*float64(i)/8000)
	}
	
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = enhancer.ProcessAudio(ctx, samples)
	}
}
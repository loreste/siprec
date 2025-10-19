package audio

import (
	"context"
	"math"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestNoiseSuppressor_NewNoiseSuppressor(t *testing.T) {
	logger := logrus.New()
	config := DefaultNoiseSuppressionConfig()
	
	ns := NewNoiseSuppressor(logger, config)
	assert.NotNil(t, ns)
	assert.NotNil(t, ns.config)
	assert.NotNil(t, ns.logger)
}

func TestNoiseSuppressor_ProcessFrame(t *testing.T) {
	logger := logrus.New()
	config := DefaultNoiseSuppressionConfig()
	config.Enabled = true
	
	ns := NewNoiseSuppressor(logger, config)

	t.Run("process silence", func(t *testing.T) {
		// Process silence (should update noise profile)
		silence := make([]float64, 512)
		ctx := context.Background()
		processed, err := ns.ProcessFrame(ctx, silence)
		assert.NoError(t, err)
		assert.NotNil(t, processed)
		assert.Len(t, processed, 512)
		
		// All samples should be near zero
		for _, sample := range processed {
			assert.InDelta(t, 0.0, sample, 0.01)
		}
	})

	t.Run("process tone with noise", func(t *testing.T) {
		// Create a test signal with a tone and noise
		samples := make([]float64, 512)
		for i := range samples {
			// 1kHz tone at 8kHz sample rate
			tone := 0.5 * math.Sin(2*math.Pi*1000*float64(i)/8000)
			noise := 0.1 * (math.Sin(float64(i)*0.1) + math.Cos(float64(i)*0.3))
			samples[i] = tone + noise
		}

		ctx := context.Background()
		processed, err := ns.ProcessFrame(ctx, samples)
		assert.NoError(t, err)
		assert.NotNil(t, processed)
		assert.Len(t, processed, 512)

		// Verify some noise reduction occurred
		var originalEnergy, processedEnergy float64
		for i := range samples {
			originalEnergy += samples[i] * samples[i]
			processedEnergy += processed[i] * processed[i]
		}
		
		// Processed should have less energy due to noise reduction
		assert.Less(t, processedEnergy, originalEnergy)
	})
}


func TestNoiseSuppressor_ConcurrentProcessing(t *testing.T) {
	logger := logrus.New()
	config := DefaultNoiseSuppressionConfig()
	ns := NewNoiseSuppressor(logger, config)
	
	// Process multiple audio streams concurrently
	done := make(chan bool)
	ctx := context.Background()
	
	for i := 0; i < 10; i++ {
		go func(id int) {
			samples := make([]float64, 512)
			for j := range samples {
				samples[j] = 0.1 * math.Sin(float64(j+id))
			}
			
			processed, err := ns.ProcessFrame(ctx, samples)
			assert.NoError(t, err)
			assert.NotNil(t, processed)
			assert.Len(t, processed, 512)
			
			done <- true
		}(i)
	}
	
	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func BenchmarkNoiseSuppressor_ProcessFrame(b *testing.B) {
	logger := logrus.New()
	config := DefaultNoiseSuppressionConfig()
	ns := NewNoiseSuppressor(logger, config)
	samples := make([]float64, 512)
	for i := range samples {
		samples[i] = 0.5 * math.Sin(2*math.Pi*1000*float64(i)/8000)
	}
	
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ns.ProcessFrame(ctx, samples)
	}
}
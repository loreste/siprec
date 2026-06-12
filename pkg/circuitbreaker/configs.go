package circuitbreaker

import "time"

// Predefined configurations for different service types

// STTConfig returns circuit breaker config optimized for STT services
func STTConfig() *Config {
	return &Config{
		FailureThreshold:     3, // STT services can be flaky, lower threshold
		SuccessThreshold:     2,
		Timeout:              30 * time.Second, // Shorter timeout for faster recovery
		MaxTimeout:           180 * time.Second,
		RequestTimeout:       45 * time.Second, // STT requests can be slow
		ExponentialBackoff:   true,
		FailureRateThreshold: 0.6, // Allow higher failure rate
		MinRequestThreshold:  5,   // Lower min requests for faster detection
		TimeWindow:           45 * time.Second,
	}
}

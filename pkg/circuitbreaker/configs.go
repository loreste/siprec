package circuitbreaker

import "time"

// Predefined configurations for different service types

// STTConfig returns circuit breaker config optimized for STT services
func STTConfig() *Config {
	return &Config{
		FailureThreshold:     3,                // STT services can be flaky, lower threshold
		SuccessThreshold:     2,
		Timeout:              30 * time.Second, // Shorter timeout for faster recovery
		MaxTimeout:           180 * time.Second,
		RequestTimeout:       45 * time.Second, // STT requests can be slow
		ExponentialBackoff:   true,
		FailureRateThreshold: 0.6,              // Allow higher failure rate
		MinRequestThreshold:  5,                // Lower min requests for faster detection
		TimeWindow:           45 * time.Second,
	}
}

// AMQPConfig returns circuit breaker config optimized for AMQP services
func AMQPConfig() *Config {
	return &Config{
		FailureThreshold:     5,
		SuccessThreshold:     2,
		Timeout:              60 * time.Second,
		MaxTimeout:           300 * time.Second,
		RequestTimeout:       10 * time.Second, // AMQP should be fast
		ExponentialBackoff:   true,
		FailureRateThreshold: 0.5,
		MinRequestThreshold:  10,
		TimeWindow:           60 * time.Second,
	}
}

// RedisConfig returns circuit breaker config optimized for Redis
func RedisConfig() *Config {
	return &Config{
		FailureThreshold:     8,                // Redis is usually reliable
		SuccessThreshold:     3,
		Timeout:              20 * time.Second, // Quick recovery for cache
		MaxTimeout:           120 * time.Second,
		RequestTimeout:       5 * time.Second,  // Redis should be very fast
		ExponentialBackoff:   true,
		FailureRateThreshold: 0.4,              // Lower tolerance for cache failures
		MinRequestThreshold:  15,
		TimeWindow:           30 * time.Second,
	}
}

// DatabaseConfig returns circuit breaker config optimized for databases
func DatabaseConfig() *Config {
	return &Config{
		FailureThreshold:     10,
		SuccessThreshold:     3,
		Timeout:              90 * time.Second,
		MaxTimeout:           600 * time.Second,
		RequestTimeout:       30 * time.Second,
		ExponentialBackoff:   true,
		FailureRateThreshold: 0.3,              // Lower tolerance for DB failures
		MinRequestThreshold:  20,
		TimeWindow:           120 * time.Second,
	}
}

// HTTPClientConfig returns circuit breaker config optimized for HTTP clients
func HTTPClientConfig() *Config {
	return &Config{
		FailureThreshold:     5,
		SuccessThreshold:     2,
		Timeout:              45 * time.Second,
		MaxTimeout:           240 * time.Second,
		RequestTimeout:       30 * time.Second,
		ExponentialBackoff:   true,
		FailureRateThreshold: 0.5,
		MinRequestThreshold:  8,
		TimeWindow:           60 * time.Second,
	}
}

// FileSystemConfig returns circuit breaker config optimized for file operations
func FileSystemConfig() *Config {
	return &Config{
		FailureThreshold:     15,               // File operations should be reliable
		SuccessThreshold:     3,
		Timeout:              10 * time.Second, // Quick recovery for file ops
		MaxTimeout:           60 * time.Second,
		RequestTimeout:       15 * time.Second,
		ExponentialBackoff:   false,            // No exponential backoff for file ops
		FailureRateThreshold: 0.2,              // Very low tolerance
		MinRequestThreshold:  25,
		TimeWindow:           30 * time.Second,
	}
}

// RealtimeConfig returns circuit breaker config optimized for real-time services
func RealtimeConfig() *Config {
	return &Config{
		FailureThreshold:     4,                // Quick detection for real-time
		SuccessThreshold:     2,
		Timeout:              15 * time.Second, // Fast recovery for real-time
		MaxTimeout:           90 * time.Second,
		RequestTimeout:       8 * time.Second,  // Real-time should be fast
		ExponentialBackoff:   true,
		FailureRateThreshold: 0.7,              // Allow higher failure rate for real-time
		MinRequestThreshold:  6,
		TimeWindow:           30 * time.Second,
	}
}
# Provider Health Monitoring & Resilience

## Overview

The SIPREC server implements comprehensive health monitoring and resilience features for STT providers, ensuring high availability and optimal performance through intelligent failover, circuit breakers, and score-based provider selection.

## Key Features

### 1. Automatic Health Checks

Periodic health checks monitor the status of all registered STT providers.

**Features:**
- Configurable check intervals
- Parallel health checking
- Response time tracking
- Success/failure rate monitoring

**Configuration:**
```env
HEALTH_CHECK_ENABLED=true
HEALTH_CHECK_INTERVAL=30s        # Health check interval
HEALTH_CHECK_TIMEOUT=10s         # Health check timeout
UNHEALTHY_THRESHOLD=3            # Failures before marking unhealthy
RECOVERY_THRESHOLD=2             # Successes before marking healthy
```

### 2. Circuit Breaker Pattern

Prevents cascading failures by temporarily disabling failed providers.

**States:**
- **Closed**: Normal operation, requests flow through
- **Open**: Provider disabled after threshold failures
- **Half-Open**: Testing recovery with limited requests

**Configuration:**
```env
CIRCUIT_BREAKER_ENABLED=true
CIRCUIT_BREAKER_THRESHOLD=5      # Failures before opening
CIRCUIT_BREAKER_TIMEOUT=60s      # Time before attempting recovery
CIRCUIT_BREAKER_HALF_OPEN_MAX=3  # Max requests in half-open state
```

### 3. Intelligent Provider Selection

Score-based selection ensures the best available provider is used.

**Scoring Factors:**
- Current health status
- Recent error rate
- Average response time
- P95/P99 latency
- Time since last success
- Circuit breaker state

**Score Calculation:**
```
Base Score: 100
- Error Rate Penalty: up to -50
- High Latency Penalty: up to -20
- Recent Failure Penalty: up to -15
- Circuit Breaker State:
  - Open: Score = 0
  - Half-Open: -30
+ Long Uptime Bonus: +10
```

### 4. Automatic Failover

Seamless failover to backup providers when primary fails.

**Failover Strategy:**
1. Attempt primary provider
2. On failure, select best backup based on scores
3. Retry with exponential backoff
4. Alert on repeated failures

**Configuration:**
```env
STT_DEFAULT_VENDOR=google
STT_FALLBACK_ORDER=deepgram,openai,speechmatics,elevenlabs
FAILOVER_MAX_ATTEMPTS=3
FAILOVER_RETRY_DELAY=1s
```

## Provider Validation

### Registration Validation

All providers undergo validation during registration:

**Validation Checks:**
- Provider name validity
- Initialization success
- Capability detection
- Performance testing
- Configuration validation

**Example Validation Result:**
```json
{
  "valid": true,
  "provider": "openai",
  "capabilities": {
    "streaming": true,
    "health_check": true,
    "enhanced_streaming": true
  },
  "performance": {
    "init_latency": "150ms",
    "test_latency": "2.5s",
    "supports_streaming": true
  },
  "warnings": [],
  "errors": []
}
```

### Configuration Validation

Provider configurations are validated at startup:

```env
# OpenAI Configuration
OPENAI_STT_ENABLED=true
OPENAI_API_KEY=sk-xxxxx          # Validated format
OPENAI_MODEL=whisper-1           # Validated against supported models

# ElevenLabs Configuration  
ELEVENLABS_STT_ENABLED=true
ELEVENLABS_API_KEY=xi-xxxxx      # Validated format
ELEVENLABS_WEBSOCKET_URL=wss://... # Validated URL

# Speechmatics Configuration
SPEECHMATICS_STT_ENABLED=true
SPEECHMATICS_API_KEY=xxxxx
SPEECHMATICS_LANGUAGE=en-US      # Validated language code
```

## Metrics & Monitoring

### Prometheus Metrics

**Provider Health Metrics:**
- `siprec_provider_health_status` - Current health status (0/1)
- `siprec_provider_health_check_seconds` - Health check duration
- `siprec_provider_selection_score` - Current selection score
- `siprec_provider_circuit_breaker_status` - Circuit breaker state

**Request Metrics:**
- `siprec_stt_requests_total` - Total requests by provider
- `siprec_stt_latency_seconds` - Request latency histogram
- `siprec_stt_errors_total` - Error count by type

### Health Endpoints

```bash
# Get provider health status
curl http://localhost:8080/api/providers/health

# Response:
{
  "providers": {
    "openai": {
      "healthy": true,
      "last_check": "2024-01-20T10:30:00Z",
      "last_success": "2024-01-20T10:30:00Z",
      "error_rate": 0.02,
      "avg_latency": "250ms",
      "score": 85,
      "circuit_breaker": "closed"
    },
    "deepgram": {
      "healthy": true,
      "error_rate": 0.01,
      "avg_latency": "180ms",
      "score": 92,
      "circuit_breaker": "closed"
    }
  }
}

# Get specific provider health
curl http://localhost:8080/api/providers/openai/health
```

## Warning System

### Warning Levels

The system generates warnings with different severity levels:

- **INFO**: Informational messages
- **LOW**: Minor issues, no immediate action
- **MEDIUM**: Issues requiring attention
- **HIGH**: Significant problems affecting service
- **CRITICAL**: Immediate action required

### Warning Categories

**Provider Warnings:**
```json
{
  "category": "stt_provider",
  "severity": "HIGH",
  "message": "Provider 'openai' has high error rate",
  "details": {
    "error_rate": 0.35,
    "threshold": 0.2,
    "recent_errors": 15
  },
  "actions": [
    "Check STT provider configuration",
    "Verify API credentials",
    "Consider enabling fallback providers"
  ]
}
```

**Performance Warnings:**
```json
{
  "category": "performance",
  "severity": "MEDIUM",
  "message": "High STT latency detected",
  "details": {
    "avg_latency": "5.2s",
    "p99_latency": "12s",
    "provider": "speechmatics"
  },
  "actions": [
    "Review performance metrics",
    "Consider performance tuning"
  ]
}
```

### Warning Management

```bash
# Get active warnings
curl http://localhost:8080/api/warnings

# Get warnings by severity
curl http://localhost:8080/api/warnings?severity=HIGH

# Suppress a warning
curl -X POST http://localhost:8080/api/warnings/warn_123/suppress \
  -d '{"duration": "1h"}'

# Resolve a warning
curl -X POST http://localhost:8080/api/warnings/warn_123/resolve
```

## Configuration Validation

### Startup Validation

Comprehensive validation runs at startup to catch configuration issues early:

**Validated Components:**
- Server configuration (ports, addresses)
- SIP settings (transport, TLS)
- Media configuration (RTP ports, codecs)
- STT provider settings
- Storage paths and permissions
- Security configuration
- Network settings

**Validation Output:**
```
INFO: Configuration validation passed
WARN: STT provider 'azure' configured but not available
WARN: Small RTP port range may limit concurrent calls
ERROR: TLS certificate file not found
```

### Runtime Validation

Continuous validation during operation:

- Provider availability checks
- Resource limit monitoring
- Configuration change detection
- Certificate expiry warnings

## Best Practices

### High Availability Setup

**1. Configure Multiple Providers:**
```env
STT_PROVIDERS=openai,deepgram,speechmatics,elevenlabs
STT_DEFAULT_VENDOR=openai
STT_FALLBACK_ORDER=deepgram,speechmatics,elevenlabs
```

**2. Enable Health Monitoring:**
```env
HEALTH_CHECK_ENABLED=true
HEALTH_CHECK_INTERVAL=30s
CIRCUIT_BREAKER_ENABLED=true
```

**3. Configure Appropriate Thresholds:**
```env
UNHEALTHY_THRESHOLD=3       # Not too sensitive
RECOVERY_THRESHOLD=2         # Quick recovery
CIRCUIT_BREAKER_THRESHOLD=5  # Prevent flapping
```

### Performance Optimization

**1. Score-Based Selection:**
```env
PROVIDER_SELECTION_MODE=score    # Use intelligent selection
MIN_PROVIDER_SCORE=30            # Minimum acceptable score
```

**2. Latency Optimization:**
```env
PREFER_LOW_LATENCY=true          # Prioritize fast providers
MAX_ACCEPTABLE_LATENCY=5s        # Reject slow providers
```

**3. Load Distribution:**
```env
LOAD_BALANCE_PROVIDERS=true      # Distribute load
LOAD_BALANCE_STRATEGY=weighted   # Based on scores
```

## Troubleshooting

### Common Issues

**Issue: Provider Frequently Marked Unhealthy**
```bash
# Check health check logs
grep "health check" /var/log/siprec/siprec.log

# Solutions:
- Increase HEALTH_CHECK_TIMEOUT
- Adjust UNHEALTHY_THRESHOLD
- Check network connectivity
- Verify API credentials
```

**Issue: Circuit Breaker Stuck Open**
```bash
# Check circuit breaker state
curl http://localhost:8080/api/providers/health | jq '.providers.openai.circuit_breaker'

# Solutions:
- Manually reset circuit breaker
- Increase CIRCUIT_BREAKER_TIMEOUT
- Check provider service status
```

**Issue: Poor Provider Selection**
```bash
# Check provider scores
curl http://localhost:8080/api/providers/scores

# Solutions:
- Review scoring weights
- Check individual metrics
- Adjust threshold values
```

### Debug Commands

```bash
# Enable debug logging for health checks
export HEALTH_CHECK_DEBUG=true

# Test specific provider health
curl -X POST http://localhost:8080/api/providers/openai/test

# Force provider failover
curl -X POST http://localhost:8080/api/providers/openai/disable

# Reset all health states
curl -X POST http://localhost:8080/api/providers/reset-health
```

## Integration Examples

### Custom Health Check Implementation

```go
type CustomHealthCheck struct{}

func (c *CustomHealthCheck) HealthCheck(ctx context.Context) error {
    // Custom health check logic
    resp, err := http.Get("https://api.provider.com/health")
    if err != nil {
        return err
    }
    if resp.StatusCode != 200 {
        return fmt.Errorf("unhealthy: status %d", resp.StatusCode)
    }
    return nil
}
```

### Warning Handler Implementation

```go
type CustomWarningHandler struct{}

func (h *CustomWarningHandler) HandleWarning(warning *Warning) {
    // Send to monitoring system
    if warning.Severity >= SeverityHigh {
        alerting.SendAlert(warning)
    }
    
    // Log to centralized logging
    logger.WithFields(warning.ToFields()).Warn(warning.Message)
    
    // Update metrics
    metrics.RecordWarning(warning.Category, warning.Severity)
}
```

## Monitoring Dashboard

Key metrics to display:

1. **Provider Health Overview**
   - Health status indicators
   - Circuit breaker states
   - Current scores

2. **Performance Metrics**
   - Request latency trends
   - Error rates by provider
   - Success/failure ratios

3. **Warning Summary**
   - Active warnings by severity
   - Recent resolved warnings
   - Suppressed warnings

4. **Failover Events**
   - Recent failover occurrences
   - Failover success rate
   - Provider switch frequency
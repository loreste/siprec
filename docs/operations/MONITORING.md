# Monitoring & Alerting

Comprehensive monitoring setup for SIPREC Server operations.

## Metrics Overview

The SIPREC Server exposes Prometheus metrics at `/metrics` endpoint.

### Core Metrics

#### SIP Metrics
- `sip_sessions_active` - Currently active SIP sessions
- `sip_sessions_total` - Total SIP sessions created
- `sip_requests_total` - Total SIP requests by method
- `sip_responses_total` - Total SIP responses by code
- `sip_session_duration_seconds` - Session duration histogram

#### RTP Metrics
- `rtp_packets_received_total` - RTP packets received
- `rtp_packets_sent_total` - RTP packets sent
- `rtp_bytes_received_total` - RTP bytes received
- `rtp_bytes_sent_total` - RTP bytes sent
- `rtp_ports_allocated` - Currently allocated RTP ports

#### STT Metrics
- `stt_transcription_duration_seconds` - STT processing time
- `stt_transcription_errors_total` - STT errors by provider
- `stt_transcription_cost_total` - Estimated STT costs
- `stt_audio_processed_bytes_total` - Audio bytes processed

#### System Metrics
- `http_requests_total` - HTTP requests by endpoint
- `http_request_duration_seconds` - HTTP request duration
- `goroutines_total` - Active goroutines
- `memory_usage_bytes` - Memory usage by type

## Prometheus Configuration

### Scrape Configuration

```yaml
# prometheus.yml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'siprec'
    static_configs:
      - targets: ['localhost:8080']
    scrape_interval: 10s
    metrics_path: /metrics
```

### Service Discovery

```yaml
# For Kubernetes
- job_name: 'siprec-k8s'
  kubernetes_sd_configs:
    - role: pod
  relabel_configs:
    - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
      action: keep
      regex: true
    - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
      action: replace
      target_label: __metrics_path__
      regex: (.+)
```

## Grafana Dashboards

### Main Dashboard

Key panels:
- Active Sessions (time series)
- Request Rate (rate)
- Error Rate (percentage)
- Response Time (histogram)
- Resource Usage (gauge)

### SIP Dashboard

```json
{
  "dashboard": {
    "title": "SIPREC SIP Metrics",
    "panels": [
      {
        "title": "Active Sessions",
        "type": "stat",
        "targets": [
          {
            "expr": "sip_sessions_active"
          }
        ]
      },
      {
        "title": "Session Rate",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(sip_sessions_total[5m])"
          }
        ]
      }
    ]
  }
}
```

## Alerting Rules

### Prometheus Alerts

```yaml
# alerts.yml
groups:
  - name: siprec
    rules:
      - alert: SIPRECDown
        expr: up{job="siprec"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "SIPREC Server is down"
          
      - alert: HighErrorRate
        expr: rate(sip_responses_total{code=~"4..|5.."}[5m]) > 0.1
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "High SIP error rate: {{ $value }}"
          
      - alert: HighMemoryUsage
        expr: memory_usage_bytes / 1024 / 1024 / 1024 > 4
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High memory usage: {{ $value }}GB"
          
      - alert: RTPPortExhaustion
        expr: rtp_ports_allocated / 10000 > 0.9
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "RTP port pool nearly exhausted"
```

### AlertManager Configuration

```yaml
# alertmanager.yml
global:
  smtp_smarthost: 'localhost:587'
  smtp_from: 'alerts@example.com'

route:
  group_by: ['alertname']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'web.hook'

receivers:
  - name: 'web.hook'
    email_configs:
      - to: 'ops@example.com'
        subject: 'SIPREC Alert: {{ .GroupLabels.alertname }}'
        body: |
          {{ range .Alerts }}
          Alert: {{ .Annotations.summary }}
          {{ end }}
```

## Application Logs

### Log Levels

Configure appropriate log levels:

```bash
# Production
LOG_LEVEL=info

# Debugging
LOG_LEVEL=debug

# Error tracking only
LOG_LEVEL=error
```

### Log Aggregation

#### ELK Stack

```yaml
# filebeat.yml
filebeat.inputs:
  - type: log
    paths:
      - /var/log/siprec/*.log
    fields:
      service: siprec
    multiline.pattern: '^\d{4}-\d{2}-\d{2}'
    multiline.negate: true
    multiline.match: after

output.elasticsearch:
  hosts: ["elasticsearch:9200"]
  index: "siprec-%{+yyyy.MM.dd}"
```

#### Fluentd

```ruby
# fluent.conf
<source>
  @type tail
  path /var/log/siprec/*.log
  pos_file /var/log/fluentd/siprec.log.pos
  tag siprec.*
  format json
</source>

<match siprec.**>
  @type elasticsearch
  host elasticsearch
  port 9200
  index_name siprec
  type_name _doc
</match>
```

## Health Checks

### Health Endpoints

```bash
# Basic health
curl http://localhost:8080/health

# Detailed health
curl http://localhost:8080/health?detailed=true

# Readiness probe
curl http://localhost:8080/ready

# Liveness probe
curl http://localhost:8080/live
```

### Custom Health Checks

```yaml
# kubernetes pod spec
livenessProbe:
  httpGet:
    path: /live
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /ready
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
```

## Performance Monitoring

### Key Performance Indicators

1. **Throughput**
   - Sessions per second
   - Concurrent sessions
   - RTP packet rate

2. **Latency**
   - SIP response time
   - STT processing time
   - End-to-end latency

3. **Reliability**
   - Error rates
   - Uptime percentage
   - Failed sessions

### APM Integration

#### Jaeger Tracing

```go
// Enable tracing
TRACING_ENABLED=true
JAEGER_ENDPOINT=http://jaeger:14268/api/traces
```

#### New Relic

```bash
NEW_RELIC_LICENSE_KEY=your-key
NEW_RELIC_APP_NAME=siprec-server
```

## Monitoring Best Practices

1. **Metrics Collection**
   - Use appropriate scrape intervals
   - Implement service discovery
   - Monitor metric cardinality

2. **Alerting**
   - Set meaningful thresholds
   - Avoid alert fatigue
   - Include runbooks

3. **Dashboards**
   - Focus on key metrics
   - Use appropriate visualizations
   - Include business metrics

4. **Log Management**
   - Structured logging
   - Appropriate retention
   - Efficient search/filtering

## Troubleshooting Monitoring

### Common Issues

1. **Missing Metrics**
```bash
# Check metrics endpoint
curl http://localhost:8080/metrics | grep sip_sessions

# Verify Prometheus scraping
curl http://prometheus:9090/api/v1/targets
```

2. **High Cardinality**
```bash
# Check metric cardinality
curl -s http://localhost:8080/metrics | grep -v "^#" | wc -l
```

3. **Alert Flapping**
```yaml
# Add hysteresis
expr: rate(errors_total[5m]) > 0.1
for: 2m  # Wait before firing
```
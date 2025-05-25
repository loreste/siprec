# Configuration Examples

Example configurations for different deployment scenarios.

## Development Configuration

Basic configuration for local development:

```bash
# .env.development
SIP_HOST=0.0.0.0
SIP_PORT=5060
HTTP_HOST=0.0.0.0
HTTP_PORT=8080

# Use OpenAI for STT
STT_PROVIDER=openai
OPENAI_API_KEY=sk-your-api-key

# Enable debug logging
LOG_LEVEL=debug
LOG_FORMAT=text

# Disable production features
AMQP_ENABLED=false
ENCRYPTION_ENABLED=false
RATE_LIMIT_ENABLED=false
```

## Production Configuration

Full production configuration with all features:

```bash
# .env.production
# Core Configuration
SIP_HOST=0.0.0.0
SIP_PORT=5060
SIP_TLS_ENABLED=true
SIP_TLS_CERT=/etc/siprec/tls/cert.pem
SIP_TLS_KEY=/etc/siprec/tls/key.pem
SIP_MAX_SESSIONS=5000

HTTP_HOST=0.0.0.0
HTTP_PORT=8080
HTTP_ENABLE_METRICS=true

# RTP Configuration
RTP_START_PORT=30000
RTP_END_PORT=40000
RTP_PUBLIC_IP=203.0.113.1

# STT Provider - Google Cloud
STT_PROVIDER=google
GOOGLE_CREDENTIALS_PATH=/etc/siprec/google-creds.json
GOOGLE_LANGUAGE_CODE=en-US
GOOGLE_MODEL=latest_long

# Message Queue
AMQP_ENABLED=true
AMQP_URL=amqp://siprec:password@rabbitmq.internal:5672/
AMQP_EXCHANGE=siprec
AMQP_QUEUE_DURABLE=true

# Audio Processing
VAD_ENABLED=true
VAD_THRESHOLD=0.6
NOISE_REDUCTION_ENABLED=true
NOISE_REDUCTION_LEVEL=0.7

# Logging
LOG_LEVEL=info
LOG_FORMAT=json
LOG_FILE=/var/log/siprec/server.log

# Performance
MAX_GOROUTINES=20000
WORKER_POOL_SIZE=200
SESSION_CACHE_SIZE=20000

# Security
ENCRYPTION_ENABLED=true
ENCRYPTION_KEY_PATH=/etc/siprec/keys
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS=1000
RATE_LIMIT_WINDOW=1m
```

## Docker Compose Configuration

Configuration for Docker deployment:

```yaml
# docker-compose.yml
version: '3.8'

services:
  siprec:
    image: siprec:latest
    environment:
      - SIP_HOST=0.0.0.0
      - SIP_PORT=5060
      - HTTP_HOST=0.0.0.0
      - HTTP_PORT=8080
      - STT_PROVIDER=aws
      - AWS_REGION=us-east-1
      - AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}
      - AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}
      - AMQP_ENABLED=true
      - AMQP_URL=amqp://guest:guest@rabbitmq:5672/
      - LOG_LEVEL=info
    ports:
      - "5060:5060/udp"
      - "5060:5060/tcp"
      - "8080:8080"
      - "30000-31000:30000-31000/udp"
    depends_on:
      - rabbitmq

  rabbitmq:
    image: rabbitmq:3-management
    environment:
      - RABBITMQ_DEFAULT_USER=guest
      - RABBITMQ_DEFAULT_PASS=guest
    ports:
      - "5672:5672"
      - "15672:15672"
```

## Kubernetes Configuration

Configuration using ConfigMap and Secrets:

```yaml
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: siprec-config
data:
  SIP_HOST: "0.0.0.0"
  SIP_PORT: "5060"
  HTTP_HOST: "0.0.0.0"
  HTTP_PORT: "8080"
  STT_PROVIDER: "azure"
  AZURE_SPEECH_REGION: "eastus"
  AMQP_ENABLED: "true"
  AMQP_URL: "amqp://siprec:password@rabbitmq-service:5672/"
  LOG_LEVEL: "info"
  LOG_FORMAT: "json"
---
# secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: siprec-secrets
type: Opaque
stringData:
  AZURE_SPEECH_KEY: "your-azure-key"
```

## High Availability Configuration

Configuration for HA deployment:

```bash
# Instance 1
SIP_HOST=0.0.0.0
SIP_PORT=5060
HTTP_HOST=0.0.0.0
HTTP_PORT=8080
RTP_PUBLIC_IP=203.0.113.1

# Shared Redis for session state
REDIS_ENABLED=true
REDIS_URL=redis://redis-cluster:6379/0
REDIS_KEY_PREFIX=siprec:instance1:

# Shared message queue
AMQP_ENABLED=true
AMQP_URL=amqp://siprec:password@rabbitmq-cluster:5672/
AMQP_EXCHANGE=siprec
AMQP_QUEUE_NAME=siprec-instance1

# Health checks for load balancer
HEALTH_CHECK_ENABLED=true
HEALTH_CHECK_INTERVAL=10s
```

## Minimal Configuration

Absolute minimum configuration:

```bash
# .env.minimal
SIP_PORT=5060
HTTP_PORT=8080
STT_PROVIDER=openai
OPENAI_API_KEY=sk-your-api-key
```

## Testing Configuration

Configuration for running tests:

```bash
# .env.test
SIP_HOST=127.0.0.1
SIP_PORT=5061
HTTP_HOST=127.0.0.1
HTTP_PORT=8081

# Use mock STT provider
STT_PROVIDER=mock

# Disable external dependencies
AMQP_ENABLED=false
ENCRYPTION_ENABLED=false

# Verbose logging for tests
LOG_LEVEL=debug
LOG_FORMAT=text

# Fast timeouts for tests
SIP_TRANSACTION_TIMEOUT=5s
RTP_TIMEOUT=5s
```
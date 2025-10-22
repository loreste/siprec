# Environment Variables Reference

Complete reference for all environment variables supported by the SIPREC Server.

## Core Configuration

### SIP Configuration
- `SIP_HOST` (default: "0.0.0.0") - SIP server bind address. Set to "0.0.0.0" to listen on all interfaces, or specify a specific IP address (e.g., "127.0.0.1") to bind to a particular interface. This address will also be used in Via and Contact headers of SIP responses.
- `SIP_PORT` (default: 5060) - SIP server port
- `SIP_TLS_ENABLED` (default: false) - Enable SIP over TLS
- `SIP_TLS_CERT` - Path to TLS certificate (required if TLS enabled)
- `SIP_TLS_KEY` - Path to TLS private key (required if TLS enabled)
- `SIP_MAX_SESSIONS` (default: 1000) - Maximum concurrent sessions

### HTTP Configuration
- `HTTP_HOST` (default: "0.0.0.0") - HTTP server bind address
- `HTTP_PORT` (default: 8080) - HTTP server port
- `HTTP_READ_TIMEOUT` (default: "15s") - HTTP read timeout
- `HTTP_WRITE_TIMEOUT` (default: "15s") - HTTP write timeout
- `HTTP_ENABLE_METRICS` (default: true) - Enable Prometheus metrics

### RTP & NAT Configuration
- `RTP_PORT_MIN` (default: 10000) - Even-numbered start of the RTP/RTCP port range
- `RTP_PORT_MAX` (default: 20000) - End of the RTP/RTCP port range
- `ENABLE_SRTP` (default: false) - Enable SRTP key negotiation and encrypted media
- `BEHIND_NAT` (default: false) - Enable NAT-aware SIP/SDP rewriting and binding
- `INTERNAL_IP` - IP address used for local UDP binding when behind NAT
- `EXTERNAL_IP` - Public IP advertised in SDP (auto-detected when unset)
- `STUN_SERVER` (default: Google STUN pool) - Comma-separated list of STUN servers used for auto-discovery

## STT Provider Configuration

### Provider Selection & Fallback
- `SUPPORTED_VENDORS` (default: `google,openai`) - Comma-separated list defining the STT fallback order
- `STT_DEFAULT_VENDOR` (default: `google`) - Preferred provider used when the requested vendor is unavailable
- `STT_VENDORS` - Legacy alias for `SUPPORTED_VENDORS` (still honoured for backwards compatibility)

During runtime the provider manager walks the `SUPPORTED_VENDORS` list in order until a provider succeeds. Keep the list aligned with the providers you register in configuration.

### OpenAI
- `OPENAI_STT_ENABLED` (default: false) - Enable OpenAI Whisper
- `OPENAI_API_KEY` - API key used for authentication
- `OPENAI_STT_MODEL` (default: `whisper-1`) - Model identifier
- `OPENAI_STT_LANGUAGE` - Optional language hint (auto-detected when omitted)
- `OPENAI_STT_PROMPT` - Prompt text providing context for better accuracy
- `OPENAI_STT_RESPONSE_FORMAT` (default: `verbose_json`) - API response format
- `OPENAI_STT_TEMPERATURE` (default: `0.0`) - Sampling temperature
- `OPENAI_BASE_URL` (default: `https://api.openai.com/v1`) - Override for private deployments

### Google Cloud
- `GOOGLE_STT_ENABLED` (default: true) - Enable Google Cloud STT
- `GOOGLE_APPLICATION_CREDENTIALS` - Path to service-account JSON file
- `GOOGLE_PROJECT_ID` - Google Cloud project ID
- `GOOGLE_STT_API_KEY` - Optional API key alternative to service account
- `GOOGLE_STT_LANGUAGE` (default: `en-US`) - Language code
- `GOOGLE_STT_SAMPLE_RATE` (default: `16000`) - Sample rate in Hz
- `GOOGLE_STT_MODEL` (default: `latest_long`) - Recognition model
- `GOOGLE_STT_ENHANCED_MODELS` (default: `false`) - Enable premium models
- `GOOGLE_STT_AUTO_PUNCTUATION` (default: `true`) - Automatic punctuation
- `GOOGLE_STT_WORD_TIME_OFFSETS` (default: `true`) - Include word-level timestamps

### AWS Transcribe
- `AWS_REGION` - AWS region
- `AWS_ACCESS_KEY_ID` - AWS access key
- `AWS_SECRET_ACCESS_KEY` - AWS secret key
- `AWS_PROFILE` - Optional profile name (when using shared credentials)
- `TRANSCRIBE_LANGUAGE_CODE` (default: `en-US`) - Target language

### Azure Speech
- `AZURE_SPEECH_KEY` - Azure Speech API key
- `AZURE_SPEECH_REGION` - Azure region
- `AZURE_LANGUAGE` (default: `en-US`) - Language code

### Deepgram
- `DEEPGRAM_STT_ENABLED` (default: false) - Enable Deepgram STT
- `DEEPGRAM_API_KEY` - API key
- `DEEPGRAM_MODEL` (default: `nova-2`) - Model identifier
- `DEEPGRAM_LANGUAGE` (default: `en`) - Language code
- `DEEPGRAM_TIER` (default: `nova`) - Tier (nova, enhanced, base)
- `DEEPGRAM_VERSION` (default: `latest`) - API version
- `DEEPGRAM_PUNCTUATE` (default: `true`) - Automatic punctuation

## Recording Storage

Enable multi-cloud storage for captured audio after the local file is finalized.

- `RECORDING_STORAGE_ENABLED` (default: false) - Upload recordings to external storage backends
- `RECORDING_STORAGE_KEEP_LOCAL` (default: true) - Retain the local `.wav` file after upload

### AWS S3
- `RECORDING_STORAGE_S3_ENABLED` (default: false) - Enable S3 uploads
- `RECORDING_STORAGE_S3_BUCKET` - Target bucket name
- `RECORDING_STORAGE_S3_REGION` - AWS region
- `RECORDING_STORAGE_S3_ACCESS_KEY` / `RECORDING_STORAGE_S3_SECRET_KEY` - Access credentials
- `RECORDING_STORAGE_S3_PREFIX` - Optional path/prefix inside the bucket

### Google Cloud Storage
- `RECORDING_STORAGE_GCS_ENABLED` (default: false) - Enable GCS uploads
- `RECORDING_STORAGE_GCS_BUCKET` - Target bucket
- `RECORDING_STORAGE_GCS_SERVICE_ACCOUNT` - Base64 encoded service account JSON or path
- `RECORDING_STORAGE_GCS_PREFIX` - Optional object prefix

### Azure Blob Storage
- `RECORDING_STORAGE_AZURE_ENABLED` (default: false) - Enable Azure uploads
- `RECORDING_STORAGE_AZURE_ACCOUNT` - Storage account name
- `RECORDING_STORAGE_AZURE_CONTAINER` - Container name
- `RECORDING_STORAGE_AZURE_ACCESS_KEY` - Access key
- `RECORDING_STORAGE_AZURE_PREFIX` - Optional blob prefix

> **Compliance Tip:** Enable `ENABLE_RECORDING_ENCRYPTION=true` to encrypt local files before they are uploaded for PCI/HIPAA workloads.

## Message Queue Configuration

### AMQP Settings
- `AMQP_ENABLED` (default: false) - Enable AMQP message queue
- `AMQP_URL` - AMQP connection URL
- `AMQP_EXCHANGE` (default: "siprec") - Exchange name
- `AMQP_EXCHANGE_TYPE` (default: "topic") - Exchange type
- `AMQP_ROUTING_KEY` (default: "transcription") - Routing key
- `AMQP_QUEUE_DURABLE` (default: true) - Durable queue
- `AMQP_DELIVERY_MODE` (default: 2) - Delivery mode (2=persistent)
- `AMQP_TLS_ENABLED` (default: false) - Enable TLS when using `AMQP_URL`
- `AMQP_TLS_CA_FILE` - Path to CA bundle for server verification
- `AMQP_TLS_CERT_FILE` / `AMQP_TLS_KEY_FILE` - Client certificate/key pair
- `AMQP_TLS_SKIP_VERIFY` (default: false) - Skip server certificate verification (testing only)
- `AMQP_HOSTS` - Comma-separated list of hosts for the enhanced AMQP client pool

## Audio Processing

### Voice Activity Detection
- `VAD_ENABLED` (default: true) - Enable VAD
- `VAD_THRESHOLD` (default: 0.5) - VAD threshold (0.0-1.0)
- `VAD_MIN_SPEECH_DURATION` (default: "200ms") - Minimum speech duration
- `VAD_MAX_SILENCE_DURATION` (default: "2s") - Maximum silence duration

### Noise Reduction
- `NOISE_REDUCTION_ENABLED` (default: false) - Enable noise reduction
- `NOISE_REDUCTION_LEVEL` (default: 0.5) - Reduction level (0.0-1.0)

## Logging

- `LOG_LEVEL` (default: "info") - Log level: "debug", "info", "warn", "error"
- `LOG_FORMAT` (default: "json") - Log format: "json", "text"
- `LOG_FILE` - Log file path (optional, logs to stdout if not set)

## Performance Tuning

### Resource Limits
- `MAX_GOROUTINES` (default: 10000) - Maximum goroutines
- `WORKER_POOL_SIZE` (default: 100) - Worker pool size
- `SESSION_CACHE_SIZE` (default: 10000) - Session cache size
- `SESSION_CACHE_TTL` (default: "1h") - Session cache TTL

### Timeouts
- `SIP_TRANSACTION_TIMEOUT` (default: "32s") - SIP transaction timeout
- `RTP_TIMEOUT` (default: "30s") - RTP inactivity timeout
- `WEBSOCKET_TIMEOUT` (default: "60s") - WebSocket ping timeout

## Security

### Encryption
- `ENCRYPTION_ENABLED` (default: false) - Enable encryption at rest
- `ENCRYPTION_KEY_PATH` - Path to encryption keys directory

### Rate Limiting
- `RATE_LIMIT_ENABLED` (default: true) - Enable rate limiting
- `RATE_LIMIT_REQUESTS` (default: 100) - Requests per window
- `RATE_LIMIT_WINDOW` (default: "1m") - Rate limit window

## Development

- `DEBUG` (default: false) - Enable debug mode
- `PPROF_ENABLED` (default: false) - Enable pprof profiling
- `PPROF_PORT` (default: 6060) - Pprof server port

## Pause/Resume Control API

### API Configuration
- `PAUSE_RESUME_ENABLED` (default: false) - Enable pause/resume API endpoints
- `PAUSE_RESUME_REQUIRE_AUTH` (default: true) - Require API key authentication
- `PAUSE_RESUME_API_KEY` - API key for authentication (required if auth enabled)
- `PAUSE_RESUME_PER_SESSION` (default: true) - Allow per-session control

### Default Behavior
- `PAUSE_RECORDING` (default: true) - Pause recording by default when API called
- `PAUSE_TRANSCRIPTION` (default: true) - Pause transcription by default when API called
- `PAUSE_RESUME_NOTIFICATIONS` (default: true) - Send notification events

### Advanced Options
- `MAX_PAUSE_DURATION` (default: "0" - unlimited) - Maximum pause duration
- `PAUSE_AUTO_RESUME` (default: false) - Auto-resume after max duration
- `PAUSE_RESUME_TIMEOUT` (default: "30s") - API request timeout

### Examples

```bash
# Enable pause/resume API with authentication
PAUSE_RESUME_ENABLED=true
PAUSE_RESUME_REQUIRE_AUTH=true
PAUSE_RESUME_API_KEY=secure-api-key-here

# Allow unlimited pause duration
MAX_PAUSE_DURATION=0

# Auto-resume after 1 hour
MAX_PAUSE_DURATION=1h
PAUSE_AUTO_RESUME=true

# Disable authentication for internal use
PAUSE_RESUME_REQUIRE_AUTH=false
```

## PII Detection & Redaction

Control automatic detection and redaction of personally identifiable information (PII) in transcriptions and audio recordings.

### PII_DETECTION_ENABLED
**Type**: Boolean  
**Default**: `false`  
**Description**: Enable or disable PII detection and redaction features.

```env
# Enable PII detection
PII_DETECTION_ENABLED=true

# Disable PII detection
PII_DETECTION_ENABLED=false
```

### PII_ENABLED_TYPES
**Type**: Comma-separated string  
**Default**: `ssn,credit_card`  
**Options**: `ssn`, `credit_card`, `phone`, `email`  
**Description**: Specify which types of PII to detect and redact.

```env
# Detect all supported types
PII_ENABLED_TYPES=ssn,credit_card,phone,email

# Only detect SSN and credit cards
PII_ENABLED_TYPES=ssn,credit_card

# Only detect phone numbers
PII_ENABLED_TYPES=phone
```

### PII_REDACTION_CHAR
**Type**: String  
**Default**: `*`  
**Description**: Character used for redacting detected PII.

```env
# Use asterisks for redaction
PII_REDACTION_CHAR=*

# Use X for redaction
PII_REDACTION_CHAR=X

# Use dashes for redaction
PII_REDACTION_CHAR=-
```

### PII_APPLY_TO_TRANSCRIPTIONS
**Type**: Boolean  
**Default**: `true`  
**Description**: Apply PII filtering to transcription text.

```env
# Apply PII filtering to transcriptions
PII_APPLY_TO_TRANSCRIPTIONS=true

# Skip transcription filtering
PII_APPLY_TO_TRANSCRIPTIONS=false
```

### PII_APPLY_TO_RECORDINGS
**Type**: Boolean  
**Default**: `true`  
**Description**: Apply PII marking to audio recordings for post-processing.

```env
# Mark PII in audio timeline
PII_APPLY_TO_RECORDINGS=true

# Skip audio marking
PII_APPLY_TO_RECORDINGS=false
```

### PII_PRESERVE_FORMAT
**Type**: Boolean  
**Default**: `true`  
**Description**: Preserve the original format when redacting PII (e.g., keep dashes in phone numbers).

```env
# Preserve format: (555) 123-4567 → (***) ***-****
PII_PRESERVE_FORMAT=true

# Don't preserve format: (555) 123-4567 → **************
PII_PRESERVE_FORMAT=false
```

### PII_CONTEXT_LENGTH
**Type**: Integer  
**Default**: `10`  
**Range**: `0-50`  
**Description**: Number of context characters to include around detected PII for logging and debugging.

```env
# Include 10 characters of context
PII_CONTEXT_LENGTH=10

# Include more context for debugging
PII_CONTEXT_LENGTH=20

# No context
PII_CONTEXT_LENGTH=0
```

### Example PII Configuration

```env
# Complete PII detection setup
PII_DETECTION_ENABLED=true
PII_ENABLED_TYPES=ssn,credit_card,phone,email
PII_REDACTION_CHAR=*
PII_APPLY_TO_TRANSCRIPTIONS=true
PII_APPLY_TO_RECORDINGS=true
PII_PRESERVE_FORMAT=true
PII_CONTEXT_LENGTH=10
```

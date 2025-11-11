# Real-time Transcription with AMQP

The SIPREC server can stream live transcription results to an AMQP message queue (e.g., RabbitMQ) in real time as calls are being recorded.

## How It Works

When STT is enabled, transcription results flow through this pipeline:

```
RTP Audio → STT Provider → TranscriptionService → AMQP Publishers → RabbitMQ Queue
```

The server provides **two complementary publishing modes**:

### 1. Basic AMQP Listener
- Immediate, direct publishing
- Simple message format
- Low latency, no batching
- Best for: Real-time alerts, low-volume scenarios

### 2. Realtime Publisher (Enhanced)
- **Batching support** for high throughput
- **Rich event structure** with full metadata
- Async, non-blocking publishing
- Configurable batch size and timeout
- Best for: Analytics pipelines, high-volume deployments

Both modes run **simultaneously** by default when realtime AMQP is enabled, providing immediate delivery plus efficient batched delivery with rich metadata.

- **Partial transcripts**: Intermediate results as the call progresses
- **Final transcripts**: Completed, finalized transcription segments
- Both types can be published independently based on your configuration

## Quick Start

### 1. Basic AMQP Configuration

Set these environment variables to enable AMQP publishing:

```bash
# Basic AMQP connection
AMQP_URL=amqp://username:password@rabbitmq-host:5672/
AMQP_QUEUE_NAME=transcriptions

# Control what gets published
PUBLISH_PARTIAL_TRANSCRIPTS=true
PUBLISH_FINAL_TRANSCRIPTS=true
```

### 2. Enable Speech-to-Text

Configure at least one STT provider:

```bash
# Example: Google Speech-to-Text
STT_DEFAULT_VENDOR=google
SUPPORTED_VENDORS=google,deepgram
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
```

### 3. Start the Server

```bash
go run ./cmd/siprec
```

The server will:
- Connect to RabbitMQ on startup
- Declare the queue if it doesn't exist
- Publish transcriptions as they arrive from STT providers

## Message Formats

### Basic AMQP Listener Format

Simple, immediate messages:

```json
{
  "call_uuid": "abc-123-def-456",
  "transcription": "Hello, how can I help you today?",
  "timestamp": "2025-10-24T10:30:45Z",
  "metadata": {
    "is_final": true,
    "provider": "google",
    "language": "en-US",
    "confidence": 0.95
  }
}
```

### Realtime Publisher Format (Enhanced)

Rich structured events with full metadata:

```json
{
  "message_id": "msg_1234567890_abcdef",
  "timestamp": "2025-10-24T10:30:45.123Z",
  "event_type": "final_transcript",
  "session_id": "session-abc-123",
  "call_id": "abc-123-def-456",

  "text": "Hello, how can I help you today?",
  "is_final": true,
  "confidence": 0.95,
  "start_time": 12.5,
  "end_time": 15.2,
  "language": "en-US",

  "speaker_id": "speaker_1",
  "speaker_label": "Agent",
  "speaker_count": 2,

  "sentiment": {
    "label": "positive",
    "score": 0.87,
    "magnitude": 0.6
  },

  "keywords": [
    {
      "text": "help",
      "category": "intent",
      "confidence": 0.92,
      "start_time": 14.1,
      "end_time": 14.4
    }
  ],

  "metadata": {
    "event_source": "siprec-realtime",
    "server_timestamp": "2025-10-24T10:30:45.125Z",
    "provider": "google"
  }
}
```

### Batched Messages

When batching is enabled, messages are grouped:

```json
{
  "batch_id": "batch_1234567890",
  "timestamp": "2025-10-24T10:30:45Z",
  "message_count": 10,
  "messages": [
    { /* event 1 */ },
    { /* event 2 */ },
    { /* ... */ }
  ]
}
```

## Advanced Configuration

### Secure AMQP (TLS)

```bash
# Use amqps:// protocol
AMQP_URL=amqps://username:password@rabbitmq-host:5671/

# TLS settings
AMQP_TLS_ENABLED=true
AMQP_TLS_CERT_FILE=/path/to/client-cert.pem
AMQP_TLS_KEY_FILE=/path/to/client-key.pem
AMQP_TLS_CA_FILE=/path/to/ca-cert.pem
AMQP_TLS_SKIP_VERIFY=false
```

### Multiple AMQP Endpoints

Publish to different queues simultaneously (requires JSON config file):

```json
{
  "messaging": {
    "enable_realtime_amqp": true,
    "realtime_amqp_endpoints": [
      {
        "name": "analytics",
        "enabled": true,
        "url": "amqp://localhost:5672/",
        "queue_name": "analytics_queue",
        "publish_partial": false,
        "publish_final": true
      },
      {
        "name": "live_monitoring",
        "enabled": true,
        "url": "amqp://localhost:5672/",
        "queue_name": "live_queue",
        "publish_partial": true,
        "publish_final": true
      }
    ]
  }
}
```

### Realtime Publisher Configuration

Enable enhanced realtime publishing with batching:

```bash
# Enable realtime AMQP (adds rich event publishing alongside basic)
ENABLE_REALTIME_AMQP=true

# Batching configuration
REALTIME_BATCH_SIZE=10              # Messages per batch
REALTIME_BATCH_TIMEOUT=1s           # Max time to wait for batch
REALTIME_QUEUE_SIZE=1000            # Internal queue size

# Event filtering
PUBLISH_PARTIAL_TRANSCRIPTS=true
PUBLISH_FINAL_TRANSCRIPTS=true
PUBLISH_SENTIMENT_UPDATES=true
PUBLISH_KEYWORD_DETECTIONS=true
PUBLISH_SPEAKER_CHANGES=true

# Message settings
AMQP_MESSAGE_TTL=24h
AMQP_PUBLISH_CONFIRM=true
```

**Performance tuning:**
- `BATCH_SIZE=1`: Immediate delivery, no batching (lowest latency)
- `BATCH_SIZE=10-50`: Balanced throughput and latency
- `BATCH_SIZE=100+`: Maximum throughput (higher latency)
- `BATCH_TIMEOUT`: Forces batch send even if not full (prevents stalls)

### Analytics & Sentiment Fan-out

The analytics dispatcher (sentiment, keywords, compliance, agent metrics) runs alongside the transcription service. To publish its output:

1. **Enable persistence + WebSocket telemetry**
   ```bash
   ANALYTICS_ENABLED=true
   ELASTICSEARCH_ADDRESSES=https://es.example.com:9200
   ELASTICSEARCH_INDEX=call-analytics
   ```
   These flags turn on the `/ws/analytics` endpoint and write aggregated snapshots (including running sentiment trend) to Elasticsearch.

2. **Publish sentiment over AMQP (optional)**
   ```bash
   ENABLE_REALTIME_AMQP=true
   PUBLISH_SENTIMENT_UPDATES=true   # default true
   ```

What you receive:
- **Real-time sentiment** with polarity, magnitude, confidence, and emotion/subjectivity hints that account for lexicon hits, intensifiers, punctuation, and negations.
- **Per-speaker tracking** so caller vs. callee sentiment stays separate (sliding 5-message context window).
- **Delivery fan-out** to WebSocket (`/ws/analytics`), AMQP realtime publishers, and Elasticsearch (`call-analytics` index by default) without additional code.

### Connection Pool Settings

For high-volume deployments:

```bash
AMQP_MAX_CONNECTIONS=10
AMQP_MAX_CHANNELS_PER_CONN=100
AMQP_CONNECTION_TIMEOUT=30s
AMQP_HEARTBEAT=10s
AMQP_PUBLISH_TIMEOUT=5s
AMQP_MAX_RETRIES=3
AMQP_RETRY_DELAY=2s
```

### Dead Letter Queue

Failed messages are automatically routed to a dead letter queue:

```bash
AMQP_DLQ_ENABLED=true
AMQP_DLQ_EXCHANGE=dead_letters
AMQP_DLQ_ROUTING_KEY=failed_transcriptions
```

## PII Filtering

Redact sensitive information before publishing to AMQP:

```bash
PII_DETECTION_ENABLED=true
PII_ENABLED_TYPES=ssn,credit_card,phone,email
PII_APPLY_TO_TRANSCRIPTIONS=true
```

Transcriptions will have PII replaced with `[REDACTED]` before being sent to the queue.

## Circuit Breaker

The AMQP client includes automatic circuit breaker protection:

- Opens after 5 consecutive failures
- Prevents cascading failures
- Automatically recovers when RabbitMQ is available
- Server continues recording even if AMQP is down

## Consuming Messages

### Python Example - Basic Messages

```python
import pika
import json

connection = pika.BlockingConnection(
    pika.URLParameters('amqp://username:password@rabbitmq-host:5672/')
)
channel = connection.channel()
channel.queue_declare(queue='transcriptions', durable=True)

def callback(ch, method, properties, body):
    message = json.loads(body)
    print(f"Call: {message['call_uuid']}")
    print(f"Transcript: {message['transcription']}")
    print(f"Final: {message['metadata']['is_final']}")
    ch.basic_ack(delivery_tag=method.delivery_tag)

channel.basic_consume(queue='transcriptions', on_message_callback=callback)
channel.start_consuming()
```

### Python Example - Realtime Events with Batching

```python
import pika
import json

connection = pika.BlockingConnection(
    pika.URLParameters('amqp://username:password@rabbitmq-host:5672/')
)
channel = connection.channel()
channel.queue_declare(queue='transcriptions', durable=True)

def handle_event(event):
    """Process a single realtime event"""
    print(f"Event Type: {event['event_type']}")
    print(f"Call: {event['call_id']}")
    print(f"Text: {event['text']}")
    print(f"Confidence: {event['confidence']}")

    if event.get('speaker_label'):
        print(f"Speaker: {event['speaker_label']}")

    if event.get('sentiment'):
        sent = event['sentiment']
        print(f"Sentiment: {sent['label']} ({sent['score']:.2f})")

    if event.get('keywords'):
        for kw in event['keywords']:
            print(f"Keyword: {kw['text']} ({kw['category']})")

def callback(ch, method, properties, body):
    message = json.loads(body)

    # Check if it's a batch
    if 'batch_id' in message:
        print(f"Processing batch of {message['message_count']} events")
        for event in message['messages']:
            handle_event(event)
    else:
        # Single event
        handle_event(message)

    ch.basic_ack(delivery_tag=method.delivery_tag)

channel.basic_consume(queue='transcriptions', on_message_callback=callback)
channel.start_consuming()
```

### Node.js Example - Realtime Events

```javascript
const amqp = require('amqplib');

(async () => {
  const connection = await amqp.connect('amqp://username:password@rabbitmq-host:5672/');
  const channel = await connection.createChannel();

  await channel.assertQueue('transcriptions', { durable: true });

  const handleEvent = (event) => {
    console.log(`[${event.event_type}] ${event.text}`);
    console.log(`  Call: ${event.call_id}`);
    console.log(`  Confidence: ${event.confidence}`);
    console.log(`  Time: ${event.start_time}s - ${event.end_time}s`);

    if (event.speaker_label) {
      console.log(`  Speaker: ${event.speaker_label}`);
    }

    if (event.sentiment) {
      console.log(`  Sentiment: ${event.sentiment.label} (${event.sentiment.score})`);
    }

    if (event.keywords && event.keywords.length > 0) {
      console.log('  Keywords:', event.keywords.map(k => k.text).join(', '));
    }
  };

  channel.consume('transcriptions', (msg) => {
    const message = JSON.parse(msg.content.toString());

    // Handle batched or single events
    if (message.batch_id) {
      console.log(`Processing batch: ${message.message_count} events`);
      message.messages.forEach(handleEvent);
    } else {
      handleEvent(message);
    }

    channel.ack(msg);
  });
})();
```

## Monitoring

Check AMQP connection status via health endpoint:

```bash
curl http://localhost:8080/healthz
```

Response includes AMQP connection state:

```json
{
  "status": "healthy",
  "amqp_connected": true,
  "amqp_endpoints": 2
}
```

## Troubleshooting

### AMQP not connecting

Check logs for connection errors:

```bash
# Look for these log messages
"AMQP client initialized successfully"
"Failed to connect to AMQP server"
```

If connection fails, the server will continue without AMQP (graceful degradation).

### No messages appearing

1. Verify STT provider is enabled and configured
2. Check `PUBLISH_PARTIAL_TRANSCRIPTS` and `PUBLISH_FINAL_TRANSCRIPTS` settings
3. Ensure calls are actually receiving transcriptions (check WebSocket `/ws/transcriptions`)
4. Verify queue name matches between publisher and consumer

### High latency

- Increase `AMQP_MAX_CONNECTIONS` and `AMQP_MAX_CHANNELS_PER_CONN`
- Enable publish confirmations: `AMQP_PUBLISH_CONFIRM=true`
- Reduce `AMQP_PUBLISH_TIMEOUT` (trades reliability for speed)

## Complete Environment Variables Reference

### Basic AMQP Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `AMQP_URL` | RabbitMQ connection URL | - |
| `AMQP_QUEUE_NAME` | Queue name for transcriptions | - |
| `PUBLISH_PARTIAL_TRANSCRIPTS` | Publish interim results | `true` |
| `PUBLISH_FINAL_TRANSCRIPTS` | Publish final results | `true` |

### Realtime Publisher Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `ENABLE_REALTIME_AMQP` | Enable enhanced realtime publishing | `false` |
| `REALTIME_BATCH_SIZE` | Messages per batch | `10` |
| `REALTIME_BATCH_TIMEOUT` | Max wait for batch | `1s` |
| `REALTIME_QUEUE_SIZE` | Internal queue size | `1000` |
| `PUBLISH_SENTIMENT_UPDATES` | Include sentiment analysis | `true` |
| `PUBLISH_KEYWORD_DETECTIONS` | Include keyword detection | `true` |
| `PUBLISH_SPEAKER_CHANGES` | Include speaker events | `true` |

### Connection Pool & Security

| Variable | Description | Default |
|----------|-------------|---------|
| `AMQP_TLS_ENABLED` | Enable TLS encryption | `false` |
| `AMQP_TLS_CERT_FILE` | Client certificate path | - |
| `AMQP_TLS_KEY_FILE` | Client key path | - |
| `AMQP_TLS_CA_FILE` | CA certificate path | - |
| `AMQP_MAX_CONNECTIONS` | Connection pool size | `10` |
| `AMQP_MAX_CHANNELS_PER_CONN` | Channels per connection | `100` |
| `AMQP_CONNECTION_TIMEOUT` | Connection timeout | `30s` |
| `AMQP_HEARTBEAT` | Heartbeat interval | `10s` |

### Publishing & Reliability

| Variable | Description | Default |
|----------|-------------|---------|
| `AMQP_PUBLISH_TIMEOUT` | Publish timeout | `5s` |
| `AMQP_PUBLISH_CONFIRM` | Wait for broker ACK | `true` |
| `AMQP_MAX_RETRIES` | Max retry attempts | `3` |
| `AMQP_RETRY_DELAY` | Delay between retries | `2s` |
| `AMQP_MESSAGE_TTL` | Message time-to-live | `24h` |
| `AMQP_DLQ_ENABLED` | Enable dead letter queue | `false` |

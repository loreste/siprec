# Real-time Transcription with AMQP

The SIPREC server can stream live transcription results to an AMQP message queue (e.g., RabbitMQ) in real time as calls are being recorded.

## How It Works

When STT is enabled, transcription results flow through this pipeline:

```
RTP Audio → STT Provider → TranscriptionService → AMQP Listener → RabbitMQ Queue
```

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

## Message Format

Each AMQP message contains:

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

### Connection Pool Settings

For high-volume deployments:

```bash
AMQP_MAX_CONNECTIONS=10
AMQP_MAX_CHANNELS_PER_CONN=100
AMQP_CONNECTION_TIMEOUT=30s
AMQP_HEARTBEAT=10s
AMQP_PUBLISH_TIMEOUT=5s
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

### Python Example

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

### Node.js Example

```javascript
const amqp = require('amqplib');

(async () => {
  const connection = await amqp.connect('amqp://username:password@rabbitmq-host:5672/');
  const channel = await connection.createChannel();

  await channel.assertQueue('transcriptions', { durable: true });

  channel.consume('transcriptions', (msg) => {
    const message = JSON.parse(msg.content.toString());
    console.log('Call:', message.call_uuid);
    console.log('Transcript:', message.transcription);
    console.log('Final:', message.metadata.is_final);
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

| Variable | Description | Default |
|----------|-------------|---------|
| `AMQP_URL` | RabbitMQ connection URL | - |
| `AMQP_QUEUE_NAME` | Queue name for transcriptions | - |
| `PUBLISH_PARTIAL_TRANSCRIPTS` | Publish interim results | `true` |
| `PUBLISH_FINAL_TRANSCRIPTS` | Publish final results | `true` |
| `AMQP_TLS_ENABLED` | Enable TLS encryption | `false` |
| `AMQP_TLS_CERT_FILE` | Client certificate path | - |
| `AMQP_TLS_KEY_FILE` | Client key path | - |
| `AMQP_TLS_CA_FILE` | CA certificate path | - |
| `AMQP_MAX_CONNECTIONS` | Connection pool size | `10` |
| `AMQP_MAX_CHANNELS_PER_CONN` | Channels per connection | `100` |
| `AMQP_PUBLISH_TIMEOUT` | Publish timeout | `5s` |
| `AMQP_PUBLISH_CONFIRM` | Wait for broker ACK | `true` |
| `AMQP_DLQ_ENABLED` | Enable dead letter queue | `false` |
| `ENABLE_REALTIME_AMQP` | Enable multiple endpoints | `false` |

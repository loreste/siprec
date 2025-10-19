# AMQP Transcription Guide

This guide explains how to set up and use the AMQP integration for the SIPREC server to receive real-time transcriptions.

## Overview

The SIPREC server can send transcriptions to an AMQP (Advanced Message Queuing Protocol) message queue as they are generated. This allows you to:

- Process transcriptions in real-time by external systems
- Archive transcriptions for later analysis
- Integrate with other services through a message broker

## Production Readiness Features

The AMQP integration includes several features to ensure production readiness:

- **Fault Tolerance**: AMQP connection issues will not crash the main server
- **Timeouts**: All AMQP operations have timeouts to prevent blocking
- **Auto-Reconnection**: The client will automatically attempt to reconnect if the connection is lost
- **Message Expiration**: Messages have a 12-hour expiration to prevent queue overflow
- **Graceful Degradation**: If AMQP is unavailable, the server continues to function without interruption
- **Panic Recovery**: All AMQP operations include panic recovery to prevent cascading failures
- **Multi-Endpoint Fan-out**: Publish live transcriptions to multiple RabbitMQ clusters concurrently
- **TLS Support**: Secure AMQP connections (including client certificates and custom CA bundles)

## Configuration

To enable AMQP integration, you need to set the following environment variables in your `.env` file:

```
# AMQP Configuration
AMQP_URL=amqp://guest:guest@localhost:5672/
AMQP_QUEUE_NAME=transcriptions
```

### Environment Variables

- `AMQP_URL`: The connection URL for your AMQP server (RabbitMQ, etc.)
  - Format: `amqp://username:password@host:port/vhost`
  - Default: Empty (AMQP disabled)

- `AMQP_QUEUE_NAME`: The name of the queue to publish transcriptions to
  - Default: Empty (AMQP disabled)

### TLS (Optional)

To enable TLS for the legacy AMQP client, set the following environment variables:

```
AMQP_TLS_ENABLED=true
AMQP_TLS_CA_FILE=/etc/rabbitmq/ca.pem
AMQP_TLS_CERT_FILE=/etc/rabbitmq/client.pem
AMQP_TLS_KEY_FILE=/etc/rabbitmq/client.key
AMQP_TLS_SKIP_VERIFY=false
```

`AMQP_URL` may use either the `amqps://` or `amqp://` scheme. When TLS variables are supplied, the client automatically negotiates secure connections.

### Multiple AMQP Endpoints & Fan-out

When you need to deliver live transcriptions to more than one RabbitMQ cluster, define additional endpoints inside your configuration file:

```json
{
  "messaging": {
    "enable_realtime_amqp": true,
    "realtime_amqp_endpoints": [
      {
        "name": "primary-rabbit",
        "use_enhanced": true,
        "amqp": {
          "hosts": ["rabbitmq-a.internal:5671", "rabbitmq-b.internal:5671"],
          "username": "siprec",
          "password": "super-secret",
          "virtual_host": "/siprec",
          "default_exchange": "siprec.transcriptions",
          "default_routing_key": "calls.raw",
          "tls": {
            "enabled": true,
            "ca_file": "/etc/rabbitmq/ca.pem"
          }
        }
      },
      {
        "name": "analytics-bus",
        "use_enhanced": false,
        "url": "amqps://analytics:token@analytics-bus.example.com:5672/",
        "queue_name": "analytics.transcriptions",
        "publish_partial": false
      }
    ]
  }
}
```

- Set `use_enhanced` to `true` to leverage the connection pool (`AMQP_HOSTS`, TLS options, load balancing, and retry controls).
- Set `publish_partial` or `publish_final` to limit which events a specific endpoint receives. If omitted, global `PUBLISH_PARTIAL_TRANSCRIPTS` / `PUBLISH_FINAL_TRANSCRIPTS` settings are used.
- Legacy `AMQP_URL` + `AMQP_QUEUE_NAME` remain supported and will be treated as the first endpoint.

## Message Format

Transcriptions are published to the AMQP queue in JSON format with the following structure:

```json
{
  "call_uuid": "abc123",
  "transcription": "This is the transcribed text",
  "timestamp": "2025-03-31T12:34:56Z",
  "is_final": true,
  "metadata": {
    "vendor": "google",
    "language": "en-US",
    "confidence": 0.95
  }
}
```

### Fields

- `call_uuid`: The unique identifier for the call
- `transcription`: The transcribed text
- `timestamp`: ISO 8601 timestamp when the transcription was generated
- `is_final`: Boolean indicating if this is a final transcription (true) or interim result (false)
- `metadata`: Additional information about the transcription:
  - `vendor`: The speech-to-text provider used (e.g., "google", "deepgram", "openai")
  - `language`: The detected or configured language
  - `confidence`: Confidence score (0-1) of the transcription accuracy (if provided by the STT vendor)

## Setting Up RabbitMQ

### Docker Compose Example

Here's an example `docker-compose.yml` file for setting up a RabbitMQ server:

```yaml
version: '3'

services:
  rabbitmq:
    image: rabbitmq:3-management
    ports:
      - "5672:5672"  # AMQP port
      - "15672:15672"  # Management UI
    environment:
      - RABBITMQ_DEFAULT_USER=guest
      - RABBITMQ_DEFAULT_PASS=guest
    volumes:
      - rabbitmq_data:/var/lib/rabbitmq

volumes:
  rabbitmq_data:
```

Start it with:

```bash
docker-compose up -d
```

You can then access the RabbitMQ management UI at `http://localhost:15672`.

## Testing AMQP Connection

To verify your AMQP connection is working:

1. Start the SIPREC server with AMQP configured
2. Check the logs for the message: "AMQP transcription listener registered - transcriptions will be sent to message queue"
3. Use the management UI to monitor the queue for messages

## Consuming Messages

### Python Example

Here's a simple Python example to consume transcription messages:

```python
import pika
import json

def callback(ch, method, properties, body):
    transcription = json.loads(body)
    print(f"Call: {transcription['call_uuid']}")
    print(f"Text: {transcription['transcription']}")
    print(f"Final: {transcription['is_final']}")
    print("---")
    ch.basic_ack(delivery_tag=method.delivery_tag)

# Connect to RabbitMQ
connection = pika.BlockingConnection(pika.ConnectionParameters('localhost'))
channel = connection.channel()

# Declare the queue
channel.queue_declare(queue='transcriptions', durable=True)

# Set up consumption
channel.basic_consume(queue='transcriptions', on_message_callback=callback)

print('Waiting for transcriptions. Press CTRL+C to exit.')
channel.start_consuming()
```

## Troubleshooting

If transcriptions are not being sent to AMQP:

1. Check the server logs for AMQP connection errors
2. Verify your AMQP credentials and server availability
3. Ensure the queue is properly declared and accessible
4. Try restarting the SIPREC server to re-establish the connection

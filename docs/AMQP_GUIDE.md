# AMQP Transcription Guide

This guide explains how to set up and use the AMQP integration for the SIPREC server to receive real-time transcriptions.

## Overview

The SIPREC server can send transcriptions to an AMQP (Advanced Message Queuing Protocol) message queue as they are generated. This allows you to:

- Process transcriptions in real-time by external systems
- Archive transcriptions for later analysis
- Integrate with other services through a message broker

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
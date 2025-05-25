# API Reference

Complete API reference for the SIPREC Server.

## API Overview

- [REST API](REST_API.md) - HTTP REST API endpoints
- [WebSocket API](WEBSOCKET_API.md) - Real-time WebSocket API
- [Configuration API](CONFIG_API.md) - Configuration management API
- [Metrics API](METRICS_API.md) - Monitoring and metrics endpoints
- [RFC Compliance](RFC_COMPLIANCE.md) - Protocol compliance details

## Base URL

```
http://localhost:8080
```

## Authentication

Most endpoints require API key authentication:

```bash
curl -H "X-API-Key: your-api-key" http://localhost:8080/api/sessions
```

## Quick Reference

### Session Management
- `GET /api/sessions` - List active sessions
- `GET /api/sessions/{id}` - Get session details
- `DELETE /api/sessions/{id}` - Terminate session

### Health & Status
- `GET /health` - Health check
- `GET /ready` - Readiness probe
- `GET /live` - Liveness probe
- `GET /metrics` - Prometheus metrics

### WebSocket
- `WS /ws` - Real-time transcription stream

## Response Format

All API responses use JSON format:

```json
{
  "status": "success|error",
  "data": {},
  "message": "Optional message",
  "timestamp": "2024-01-15T10:00:00Z"
}
```

## Error Codes

Standard HTTP status codes:
- 200: Success
- 400: Bad Request
- 401: Unauthorized
- 404: Not Found
- 500: Internal Server Error

## Rate Limiting

API requests are rate limited:
- Default: 100 requests per minute
- Header: `X-RateLimit-Remaining`
- Header: `X-RateLimit-Reset`
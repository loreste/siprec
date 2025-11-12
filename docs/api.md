# API Reference

This document describes every HTTP and WebSocket interface exposed by the SIPREC server. Unless otherwise noted, the base URL is `http://<host>:<http_port>` (default `:8080`).

## Authentication

Most read-only endpoints (health, status, session listing, WebSockets) are open when the HTTP server is bound to a trusted network segment. Mutating endpoints can be protected via the following mechanisms:

- **Pause/Resume API**: enable `PauseResumeConfig.RequireAuth` and set `PauseResumeConfig.APIKey`. Requests must include `X-API-Key: <key>` header or `?api_key=<key>` query parameter.
- **JWT / API keys**: when the global auth middleware is configured (see `AUTH_*` env vars), WebSocket handlers enforce token checks via the `Authorization: Bearer <jwt>` header or `token` query parameter.

Always front the HTTP service with TLS-terminating infrastructure (Ingress, reverse proxy, etc.) in production deployments.

---

## Health & Status

| Endpoint | Method | Description |
| --- | --- | --- |
| `/health` | GET | Aggregated health report with subsystem breakdown (SIP stack, WebSocket hub, Redis, database, STT providers, encryption, RTP port pool, etc.). |
| `/health/live` | GET | Liveness probe (returns 200 when the process is running). |
| `/health/ready` | GET | Readiness probe (fails if critical dependencies are unavailable). |
| `/status` | GET | Provides uptime, version, build metadata, and process stats. |

**Example** (`GET /health`):

```json
{
  "status": "healthy",
  "timestamp": "2025-11-10T12:00:00Z",
  "uptime": "72h32m10s",
  "version": "0.0.34",
  "checks": {
    "sip": { "status": "healthy", "message": "SIP service is running" },
    "websocket": { "status": "healthy" },
    "rtp_ports": { "status": "healthy", "message": "RTP ports available" },
    "amqp": { "status": "degraded", "message": "AMQP disconnected" }
  },
  "system": {
    "goroutines": 182,
    "memory_mb": 512,
    "cpu_count": 8,
    "active_calls": 24,
    "ports_in_use": 40
  }
}
```

---

## Metrics

| Endpoint | Method | Description |
| --- | --- | --- |
| `/metrics` | GET | Prometheus/OpenMetrics endpoint (includes SIP, RTP, STT, AMQP, analytics, CPU/memory gauges). |
| `/metrics/simple` | GET | JSON summary when Prometheus is disabled. |

The server automatically registers `/metrics` only if `HTTP.EnableMetrics=true`.

---

## Session APIs

| Endpoint | Method | Description |
| --- | --- | --- |
| `/api/sessions` | GET | Returns all active sessions (`?id=<call-id>` filters to one session). |
| `/api/sessions/stats` | GET | Aggregate counters (active calls, paused calls, per-provider distribution, etc.). |

**Example** (`GET /api/sessions`):

```json
{
  "count": 2,
  "sessions": [
    {
      "call_id": "B2B.160.1762863004.937031301",
      "state": "connected",
      "created_at": "2025-11-11T12:10:04.493Z",
      "participants": ["sip:agent@pbx", "sip:customer@example.com"],
      "recording_path": "/var/lib/siprec/B2B1601762863004937031301.wav",
      "analytics": {
        "sentiment": "neutral",
        "mos": 4.2
      }
    }
  ]
}
```

With `RECORDING_COMBINE_LEGS=true` (default) the `recording_path` points to the merged multi-channel WAV while the per-leg files remain available alongside it.

---

## Pause / Resume API

These endpoints are available when `PauseResumeConfig.Enabled=true`. Enable per-session control with `PauseResumeConfig.PerSession=true`. Requests can specify `pause_recording` and/or `pause_transcription`; unspecified fields fall back to the config defaults.

| Endpoint | Method | Description |
| --- | --- | --- |
| `/api/sessions/{id}/pause` | POST | Pause a specific session. Body: `{ "pause_recording": true, "pause_transcription": false }`. |
| `/api/sessions/{id}/resume` | POST | Resume a paused session. |
| `/api/sessions/{id}/pause-status` | GET | Current pause state for a call. |
| `/api/sessions/pause-all` | POST | Pause all active sessions. |
| `/api/sessions/resume-all` | POST | Resume all paused sessions. |
| `/api/sessions/pause-status` | GET | Returns pause states for every active session. |

**Response payload (`PauseStatus`)**

```json
{
  "session_id": "B2B.160.1762863004.937031301",
  "is_paused": true,
  "recording_paused": true,
  "transcription_paused": false,
  "paused_at": "2025-11-11T12:15:55.000Z",
  "pause_duration": 12.34,
  "auto_resume_at": null
}
```

---

## Compliance & GDPR

| Endpoint | Method | Description |
| --- | --- | --- |
| `/api/compliance/status` | GET | Snapshot of PCI/GDPR configuration (PII detection, encryption state, SRTP/TLS flags, etc.). |
| `/api/compliance/gdpr/export` | POST | Export call data to the configured GDPR export directory. Body: `{ "call_id": "<call-id>" }`. |
| `/api/compliance/gdpr/erase` | POST / DELETE | Erase all artifacts (local recordings, manifests, remote storage copies) for a call. Body: `{ "call_id": "<call-id>" }`. |

GDPR endpoints are available only when `Compliance.GDPR.Enabled=true` and the GDPR service has been initialized (requires database backing).

---

## Configuration Management

| Endpoint | Method | Description |
| --- | --- | --- |
| `/api/config` | GET | Returns the currently active configuration (full JSON). |
| `/api/config/validate` | POST | Validates a candidate configuration document. Body: `config.Config` JSON. |
| `/api/config/reload` | POST | Triggers a hot-reload when the hot-reload manager is enabled. |
| `/api/config/reload/status` | GET | Reports whether hot-reload is enabled and shows last reload metadata. |

Use these APIs to test configuration files generated by automation before deploying them to disk.

---

## Speech-to-Text Job Control

The async STT processor exposes management endpoints when `stt.AsyncSTTProcessor` is configured.

| Endpoint | Method | Description |
| --- | --- | --- |
| `/api/stt/submit` | POST | Submit a transcription job for an existing recording. |
| `/api/stt/jobs` | GET | List queued / running / completed jobs. |
| `/api/stt/jobs/{job_id}` | GET | Inspect a single job. |
| `/api/stt/stats` | GET | Queue depth, worker utilization, success/error counters. |
| `/api/stt/metrics` | GET | Detailed processing metrics (per-provider latency, cost estimates). |
| `/api/stt/queue/purge` | POST | Clears pending jobs (requires elevated privileges). |

**Submit job request**

```json
{
  "audio_path": "/var/lib/siprec/B2B.160_leg0.wav",
  "call_uuid": "B2B.160.1762863004.937031301",
  "session_id": "B2B.160_leg0",
  "provider": "google",
  "language": "en-US",
  "priority": 2
}
```

**Submit response**

```json
{
  "job_id": "job_01HFNF7VY76S9P7QFG",
  "status": "queued",
  "estimated_cost": 0.0034,
  "message": "Job submitted successfully"
}
```

---

## WebSocket Endpoints

### `/ws/transcriptions`

- Streams real-time transcription events (partial/final) for all active calls or a specific `callUUID`.
- Query parameters:
  - `call_id` – Optional filter. Only events for the specified Call-ID are sent.
  - `token` – Optional JWT for protected deployments.
- Payload follows the schema described in [docs/realtime-transcription.md](realtime-transcription.md) (includes `text`, `is_final`, `speaker_label`, `sentiment`, `keywords`, `metadata`).

### `/ws/analytics`

- Enabled when `ANALYTICS_ENABLED=true`.
- Broadcasts analytics snapshots and streaming events (sentiment trend updates, compliance alerts, agent metrics).
- Query parameters:
  - `call_id` – Optional filter to follow a single call.
  - `session_id` – Optional client identifier (generated if omitted).

**Message format**

```json
{
  "type": "snapshot",
  "call_id": "B2B.160.1762863004.937031301",
  "timestamp": "2025-11-11T12:16:00Z",
  "data": {
    "sentiment_trend": [
      { "speaker": "agent", "label": "positive", "score": 0.72, "confidence": 0.91 },
      { "speaker": "customer", "label": "negative", "score": -0.35, "confidence": 0.87 }
    ],
    "keywords": ["refund", "policy"],
    "compliance": [],
    "agent_metrics": {
      "talk_ratio": 0.58,
      "dead_air_seconds": 2.1
    }
  }
}
```

The handler also emits lightweight `event` messages such as `sentiment_alert` (strong negative streak) or `sentiment_positive`.

---

## AMQP / Realtime Streaming

When `ENABLE_REALTIME_AMQP=true`, the realtime publisher mirrors the same JSON payloads that appear on `/ws/transcriptions` and `/ws/analytics` into RabbitMQ queues/exchanges. Event filters are controlled via:

- `PUBLISH_PARTIAL_TRANSCRIPTS`
- `PUBLISH_FINAL_TRANSCRIPTS`
- `PUBLISH_SENTIMENT_UPDATES`
- `PUBLISH_KEYWORD_DETECTIONS`
- `PUBLISH_SPEAKER_CHANGES`

Refer to [docs/realtime-transcription.md](realtime-transcription.md) for connection examples.

---

## Error Handling

- All REST endpoints return JSON error envelopes (`errors.WriteError`) with HTTP status codes:
  - `400` for validation issues / missing parameters
  - `401/403` for auth failures
  - `404` when the requested session/job/resource is not found
  - `429` when rate limits are enforced (PII/analytics)
  - `5xx` for server-side failures
- WebSocket handlers send `{"type":"error","message":"..."}` before closing the connection when authentication fails or the client sends malformed frames.

---

## Versioning & Compatibility

All endpoints listed here are part of the 0.0.34 release. Backward-incompatible changes will be announced in the `CHANGELOG` and versioned via semantic versioning. For automation, inspect the `Server` header (`siprec/<version>`) or call `GET /status`.

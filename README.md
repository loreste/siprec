# SIPREC Server

> A focused SIP recording service with built-in SIP stack, optional speech-to-text streaming, and shared session storage.

## Overview

This project implements a SIPREC-compliant recording endpoint with a custom SIP transaction layer. The server parses multipart INVITE payloads (SDP + “application/rs-metadata+xml”) as defined in RFC 7865/7866, maintains per-call state, and exposes control/health helpers for automation environments. NAT traversal, session persistence, and downstream speech-to-text streaming are handled inside the same process so deployments can stay lightweight.

## What’s Included

- **SIPREC core** – RFC 7865/7866 metadata parsing, SDP handling, and custom SIP server (UDP/TCP) with support for large metadata payloads.
- **NAT rewriting** – SIP Via/Contact rewriting with automatic external IP discovery and configurable overrides for public deployments.
- **Session management** – A shared persistence layer between the SIP handler and the session manager. Works with the in-memory store by default or any implementation of `pkg/session.SessionStore` (Redis adapter included).
- **Pause / resume controls** – Handler-level APIs for pausing or resuming recording/transcription mid-call.
- **Speech-to-text integration (optional)** – Pluggable STT provider manager with streaming callbacks. The handler routes audio to the selected provider when configured.
- **Operational helpers** – HTTP health/readiness endpoints, telemetry hooks, and resource cleanup for active calls and RTP forwarders.

Everything listed above is part of the executable; features that are only partially implemented or deprecated have been removed from this documentation.

## Quick Start

### Build & Run

```bash
git clone https://github.com/loreste/siprec.git
cd siprec

# Run directly (defaults to UDP/TCP on 0.0.0.0:5060)
go run ./cmd/siprec
```

### Minimal Environment Variables

| Variable | Description | Default |
| --- | --- | --- |
| `SIP_HOST` | Bind address for SIP listeners | `0.0.0.0` |
| `PORTS` | Comma-separated UDP/TCP SIP ports | `5060,5061` |
| `BEHIND_NAT` | Enable NAT rewriting | `false` |
| `EXTERNAL_IP` | Override public IP or `auto` for STUN discovery | `auto` |
| `STUN_SERVER` | STUN server when NAT rewriting is enabled | `stun:stun.l.google.com:19302` |
| `RECORDING_DIR` | Recording output directory | `./recordings` |
| `SESSION_TIMEOUT` | Idle duration before a call is considered stale | `1h` (if session persistence enabled) |

When `BEHIND_NAT=true`, the SIP handler updates Via/Contact headers with the detected external IP or the value you provide.

## Optional Components

### Shared Session Store (Redis)

To persist sessions across processes, configure the session manager and pass the same store to the SIP handler:

```go
redisStore, _ := session.NewRedisSessionStore(redisConfig, logger)

handlerConfig := &sip.Config{
    SessionStore:  redisStore,
    SessionNodeID: "recorder-1",
    // ...other handler options...
}

handler, _ := sip.NewHandler(logger, handlerConfig, sttManager)
```

Redis-specific settings can be supplied through the environment (see `pkg/session/integration.go`), including `REDIS_ENABLED=true`, `REDIS_ADDRESS`, and `REDIS_SESSION_TTL`.

### Speech-to-Text Providers

If you initialise a `stt.ProviderManager`, the handler can stream audio to whichever provider you register. Provider credentials and routing logic live in `pkg/stt`; leave the manager unset to run without transcription.

## Health & Control Endpoints

The HTTP server (enabled by default) exposes:

- `GET /healthz` – Aggregate health state (SIP handler, session store, RTP ports, optional dependencies).
- `GET /readyz` – Readiness probe (fails if SIP handler or session store are unavailable).
- Pause/resume control routes (see `pkg/sip/pause_resume_service.go`) for adjusting active calls.

## Development Notes

- All packages build with Go 1.21 or newer.
- `go test ./...` runs unit/integration tests; some STT integration tests require credentials and may be skipped by default.
- NAT rewriting relies on `sipparser.UDPMTUSize = 4096` to keep SIPREC responses under the UDP MTU when metadata is large.
- Shared session storage uses the adapter in `pkg/sip/session_store_adapter.go` to translate between `CallData` and `session.SessionData`.

## License

GPL-3.0 – see [LICENSE](LICENSE).

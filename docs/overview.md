# Overview

IZI SIPREC provides a compact implementation of RFC 7865/7866 recording flows. The service embeds its own SIP transaction layer so that SIPREC metadata, SDP negotiation, and call lifecycle are all handled inside a single Go binary.

## Core Components

- **Custom SIP stack** – UDP and TCP listeners backed by `github.com/emiago/sipgo`. Responses are rewritten to work through NAT and to tolerate large `application/rs-metadata+xml` payloads.
- **Metadata processor** – Multipart parsing and validation for SIPREC metadata. The handler keeps the decoded `RSMetadata` alongside the SDP describing the recording streams.
- **Session manager bridge** – The SIP handler and the high-level session manager share the same persistence layer. By default, an in-memory store is used; any implementation of `pkg/session.SessionStore` (for example the Redis store) can be injected at runtime.
- **Pause / resume service** – Runtime controls for toggling recording/transcription state without dropping the SIP dialog.
- **High Availability** – Built-in session redundancy and failover handling (configurable via Redis).
- **Optional STT streaming** – A provider manager routes captured audio to providers like Google, Deepgram, or Whisper. Supports **PII Redaction** and **Language Switching**.
- **Multi-vendor support** – Automatic detection and metadata extraction for Oracle, Cisco, Avaya, NICE, Genesys, FreeSWITCH, Asterisk, and OpenSIPS. See [Vendor Integration](vendor-integration.md) for details.

## Data Flow Summary

1. A SIPREC SRC sends an INVITE containing SDP + metadata.
2. The SIP handler parses SDP and `RSMetadata`, creates/updates the shared session record, and acknowledges the dialog.
3. RTP packets are forwarded according to the negotiated SDP (pause/resume can interrupt forwarding).
4. If STT is enabled, audio frames are streamed to the selected provider via the STT manager.
5. Health/readiness endpoints expose status for SIP, session storage, and auxiliary services.

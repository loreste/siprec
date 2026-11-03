# Documentation

This directory contains the current, supported feature set for IZI SIPREC.

## Contents

- [Release artifacts](release-artifacts.md) – Codec capabilities, version metadata, and immutable release publication.

- `installation.md` – Complete installation guide for all platforms.
- [Oracle Linux 10 deployment notes](oracle-linux-10.md) – Pinned builds, native codec prerequisites, systemd, SELinux, and firewall considerations.
- `overview.md` – Feature summary and architecture snapshot.
- `audio-pipeline.md` – RTP audio processing pipeline architecture and fixes.
- `configuration.md` – Configuration options including YAML/JSON config files and environment variables.
- `sessions.md` – How the shared session store works (in-memory vs Redis).
- `stt.md` – Optional speech-to-text integration notes with real-time analytics (sentiment, keywords).
- `realtime-transcription.md` – Real-time transcription streaming via AMQP/RabbitMQ.
- `vendor-integration.md` – Supported SBC vendors (Oracle, Cisco, Avaya, NICE, Genesys, etc.) and metadata extraction.
- `whisper-setup.md` – Local/remote Whisper installation and configuration.
- `cluster-configuration.md` – Multi-instance and high-availability deployments.
- `api.md` – HTTP API reference.

## Quick Start

For production deployments, create a `config.yaml` file:

```yaml
network:
  host: "0.0.0.0"
  ports:
    - 5060

http:
  port: 8080

logging:
  level: "info"
```

Place it in one of these locations (in order of priority):
1. Path specified in `CONFIG_FILE` environment variable
2. `./config.yaml` (current directory)
3. `/etc/siprec/config.yaml`
4. `$HOME/.siprec/config.yaml`

See `configuration.md` for complete configuration reference.

New documents should live alongside these files and reference only functionality that exists in the codebase.

## Incomplete metadata compatibility

Builds after v1.3.4 include opt-in fallback for incomplete SIPREC metadata. The [configuration reference](configuration.md#incomplete-siprec-metadata) explains the separate fill-missing and allow-missing-participants settings, strict validation, and provenance. See the [vendor integration guide](vendor-integration.md#incomplete-metadata-from-an-src) before configuring SBC header manipulation.

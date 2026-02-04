# Documentation

This directory contains the current, supported feature set for IZI SIPREC.

## Contents

- `overview.md` – Feature summary and architecture snapshot.
- `configuration.md` – Environment variables and config snippets for common deployments.
- `sessions.md` – How the shared session store works (in-memory vs Redis).
- `stt.md` – Optional speech-to-text integration notes.
- `realtime-transcription.md` – Real-time transcription streaming via AMQP/RabbitMQ.
- `vendor-integration.md` – Supported SBC vendors (Oracle, Cisco, Avaya, NICE, Genesys, etc.) and metadata extraction.
- `whisper-setup.md` – Local/remote Whisper installation and configuration.

New documents should live alongside these files and reference only functionality that exists in the codebase.

# Configuration

All settings are provided through environment variables. The core service only requires SIP bind details; everything else is optional.

## SIP & Networking

| Variable | Description | Default |
| --- | --- | --- |
| `SIP_HOST` | Listen address for SIP (UDP & TCP) | `0.0.0.0` |
| `PORTS` | Comma-separated list of SIP ports | `5060,5061` |
| `BEHIND_NAT` | Enable NAT rewriting (Via/Contact) | `false` |
| `EXTERNAL_IP` | Public IP override or `auto` for STUN | `auto` |
| `STUN_SERVER` | STUN server used when `EXTERNAL_IP=auto` | `stun:stun.l.google.com:19302` |
| `RTP_PORT_MIN` / `RTP_PORT_MAX` | RTP port range | `10000-20000` |
| `RTP_TIMEOUT` | RTP inactivity timeout before cleanup | `30s` |
| `RTP_BIND_IP` | Bind RTP listener to specific IP (empty = all interfaces) | `` |

## Recording

| Variable | Description | Default |
| --- | --- | --- |
| `RECORDING_DIR` | Folder for recorded media | `./recordings` |
| `RECORDING_MAX_DURATION` | Max duration per call | `4h` |
| `RECORDING_COMBINE_LEGS` | Merge SIPREC legs into one multi-channel WAV | `true` |

## Session Persistence

To persist session data across restarts, initialise a session manager store (e.g. Redis) and pass it to the SIP handler through code:

```go
redisStore, _ := session.NewRedisSessionStore(redisConfig, logger)

handlerConfig := &sip.Config{
    SessionStore:  redisStore,
    SessionNodeID: "recorder-1",
}
handler, _ := sip.NewHandler(logger, handlerConfig, sttManager)
```

Redis connection parameters can be set via:

| Variable | Description | Default |
| --- | --- | --- |
| `REDIS_ENABLED` | Enable Redis-backed session store | `false` |
| `REDIS_ADDRESS` | Redis endpoint | `localhost:6379` |
| `REDIS_PASSWORD` | Password (optional) | `""` |
| `REDIS_DATABASE` | Database index | `0` |
| `REDIS_SESSION_TTL` | TTL for stored sessions | `24h` |

## Speech-to-Text (Optional)

The project exposes hooks for a provider manager. Each provider has its own credentials (see `pkg/stt`). Typical variables include:

| Variable | Description |
| --- | --- |
| `STT_DEFAULT_VENDOR` | Preferred provider (e.g. `google`) |
| `SUPPORTED_VENDORS` | Comma-separated provider list |
| Provider-specific keys | e.g. Google service-account JSON, Deepgram API key |

Leave the manager unset to run the recorder without STT streaming.

### Whisper CLI (on-prem)

| Variable | Description | Default |
| --- | --- | --- |
| `WHISPER_ENABLED` | Enable the `whisper` vendor | `false` |
| `WHISPER_BINARY_PATH` | CLI path (e.g. `/usr/local/bin/whisper`) | `whisper` |
| `WHISPER_MODEL` | Model name (`tiny`, `base`, `small`, `medium`, `large`) | `base` |
| `WHISPER_TASK` | `transcribe` or `translate` | `transcribe` |
| `WHISPER_TRANSLATE` | Force translate mode (overrides task) | `false` |
| `WHISPER_LANGUAGE` | Force language (empty for auto-detect) | `""` |
| `WHISPER_OUTPUT_FORMAT` | CLI output format (`json`, `txt`, `srt`, `vtt`, `tsv`, `verbose_json`) | `json` |
| `WHISPER_SAMPLE_RATE` | Sample rate for the buffered WAV | `16000` |
| `WHISPER_CHANNELS` | Channel count for the WAV | `1` |
| `WHISPER_TIMEOUT` | Maximum runtime per call | `10m` |
| `WHISPER_MAX_CONCURRENT` | Max concurrent calls (`-1`=auto, `0`=unlimited, `N`=limit) | `-1` |
| `WHISPER_EXTRA_ARGS` | Additional CLI arguments (e.g., `--device cuda --fp16 True`) | `""` |

**Remote Server Example**:
```bash
# Whisper running on separate GPU server
WHISPER_ENABLED=true
WHISPER_BINARY_PATH=/usr/local/bin/whisper-remote  # SSH or HTTP wrapper
WHISPER_MODEL=large
WHISPER_TIMEOUT=20m  # Increased for network latency
WHISPER_MAX_CONCURRENT=16  # GPU server can handle more
```

See [Speech-to-Text Integration](stt.md) for detailed Whisper configuration including GPU acceleration, remote servers, and performance tuning.

## HTTP Server

| Variable | Description | Default |
| --- | --- | --- |
| `HTTP_ENABLED` | Expose health/control endpoints | `true` |
| `HTTP_PORT` | HTTP listen port | `8080` |

Health (`/healthz`) and readiness (`/readyz`) checks automatically reflect the SIP handler and shared session store.

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

## Recording

| Variable | Description | Default |
| --- | --- | --- |
| `RECORDING_DIR` | Folder for recorded media | `./recordings` |
| `RECORDING_MAX_DURATION` | Max duration per call | `4h` |

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

## HTTP Server

| Variable | Description | Default |
| --- | --- | --- |
| `HTTP_ENABLED` | Expose health/control endpoints | `true` |
| `HTTP_PORT` | HTTP listen port | `8080` |

Health (`/healthz`) and readiness (`/readyz`) checks automatically reflect the SIP handler and shared session store.

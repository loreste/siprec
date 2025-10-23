# Speech-to-Text Integration (Optional)

Speech-to-text streaming is not required for SIPREC, but the handler exposes hooks so you can forward RTP audio to an external provider.

## Provider Manager

`pkg/stt` contains:

- `ProviderManager` – routes calls to the selected provider, handles retries/fallbacks.
- Provider implementations (e.g. Google, Deepgram). Each provider expects its own credentials/environment variables.

The handler’s STT callback is automatically wired when you pass a manager to `NewHandler`:

```go
sttManager := stt.NewProviderManager(logger, sttConfig)
handler, _ := sip.NewHandler(logger, handlerConfig, sttManager)
```

If you do not supply a manager, the handler returns `ErrNoProviderAvailable` and continues recording without transcription.

## Audio Flow

1. The SIP handler negotiates SDP with the SRC.
2. RTP packets are forwarded internally (pause/resume can stop forwarding).
3. When STT is enabled, audio samples are passed to the provider manager in real time.
4. Provider callbacks can push transcripts via WebSocket or any other channel you configure.

## Configuration Tips

- Set `STT_DEFAULT_VENDOR` to the provider you want to use by default.
- Include vendors in `SUPPORTED_VENDORS` if you plan to route dynamically.
- Review provider-specific environment variables in `pkg/stt` (API keys, endpoints, etc.).
- Integration tests for some providers require credentials; run `go test ./pkg/stt -run Provider` selectively when secrets are available.

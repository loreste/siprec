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

- Set `DEFAULT_SPEECH_VENDOR` to the provider you want to use by default.
- Includes support for: **Google**, **Deepgram**, **ElevenLabs**, **Speechmatics**, **Azure**, **Amazon Transcribe**, **OpenAI (Whisper API)**, and **Local Whisper**.
- Review provider-specific environment variables in `pkg/stt` (API keys, endpoints, etc.).
- Integration tests for some providers require credentials; run `go test ./pkg/stt -run Provider` selectively when secrets are available.

## Advanced Features

### PII Redaction
Some providers (e.g., Deepgram) support real-time redaction of sensitive information.
- **Deepgram**: Configure `DEEPGRAM_REDACT` with a comma-separated list of entities (e.g., `pci,ssn,email`).

### Language Switching
The server supports automatic language detection and switching for providers that offer it.
- **Deepgram**: Enable `DEEPGRAM_REALTIME_SWITCHING=true` to allow the model to switch languages mid-stream.
- **Language Routing**: You can map specific language codes to different providers via configuration if needed.

## Supported Providers & Configuration
(See `configuration.md` for full environment variable list)

### Cloud Providers
- **Google Cloud STT**: `GOOGLE_STT_ENABLED=true`
- **Deepgram**: `DEEPGRAM_STT_ENABLED=true`
- **ElevenLabs**: `ELEVENLABS_STT_ENABLED=true`
- **Speechmatics**: `SPEECHMATICS_STT_ENABLED=true`
- **Azure Speech**: `AZURE_STT_ENABLED=true`
- **Amazon Transcribe**: `AMAZON_STT_ENABLED=true`
- **OpenAI Whisper API**: `OPENAI_STT_ENABLED=true`

## Local Whisper (open-source)

If you need to keep audio on-prem, you can route the PCM stream to the open-source [openai/whisper](https://github.com/openai/whisper) CLI. The provider buffers each call into a temporary WAV file, invokes the CLI with your chosen model, and ingests the generated transcript file.

> **Note on Deployment Flexibility**: The Whisper binary does **not** need to run on the same server as SIPREC. You can deploy Whisper on a dedicated GPU server and access it via SSH wrapper or HTTP API. See [Remote Server Deployment](#remote-server-deployment) section below for configuration examples.

> **Need a step-by-step guide?** See the dedicated [Whisper Setup Guide](whisper-setup.md) for installation options, wrappers, and validation steps.

### Configuration

| Variable | Description | Default |
| --- | --- | --- |
| `WHISPER_ENABLED` | Enable the Whisper CLI provider | `false` |
| `WHISPER_BINARY_PATH` | Path to the `whisper` executable (or `python -m whisper`) | `whisper` |
| `WHISPER_MODEL` | Model name (`tiny`, `base`, `small`, `medium`, `large`) | `base` |
| `WHISPER_TASK` | `transcribe` or `translate` | `transcribe` |
| `WHISPER_LANGUAGE` | Language hint (leave empty for auto) | `""` |
| `WHISPER_OUTPUT_FORMAT` | CLI output (`json`, `txt`, `srt`, `vtt`, `tsv`, `verbose_json`) | `json` |
| `WHISPER_SAMPLE_RATE` / `WHISPER_CHANNELS` | PCM format used for the temporary WAV | `16000` / `1` |
| `WHISPER_TIMEOUT` | Max runtime for the CLI invocation | `10m` |
| `WHISPER_MAX_CONCURRENT` | Max concurrent calls (`-1`=auto, `0`=unlimited) | `-1` |
| `WHISPER_EXTRA_ARGS` | Additional CLI arguments (e.g., `--device cuda`) | `""` |

Add `whisper` to `SUPPORTED_VENDORS` (and optionally `DEFAULT_SPEECH_VENDOR`) to route calls to the local CLI. The PCM stream is still available for WebSocket/AMQP listeners, and the CLI output is published with provider metadata like any other STT vendor.

### Model Selection

Whisper offers 5 model sizes with different accuracy/performance tradeoffs:

| Model | Parameters | Disk Size | Relative Speed | Use Case |
| --- | --- | --- | --- | --- |
| `tiny` | 39 M | 75 MB | ~32x | Testing, low-resource systems |
| `base` | 74 M | 142 MB | ~16x | Default, good balance |
| `small` | 244 M | 466 MB | ~6x | Better accuracy, moderate CPU |
| `medium` | 769 M | 1.5 GB | ~2x | High accuracy, 4+ core CPUs |
| `large` | 1550 M | 2.9 GB | 1x | Best accuracy, 8+ core CPUs/GPU |

**Recommendation**: Start with `base` for testing, use `small` for production on modern hardware, and `large` with GPU acceleration for highest accuracy.

### Performance & Concurrency

**Concurrent Call Limiting**: Whisper is CPU-intensive and loads models into memory. Use `WHISPER_MAX_CONCURRENT` to prevent resource exhaustion:

- `-1` (default): Automatically limits to number of CPU cores
- `0`: Unlimited (only recommended with GPU acceleration)
- `N`: Limit to N concurrent transcriptions

**Example configurations**:
```bash
# 4-core server: auto-limit to 4 concurrent calls
WHISPER_MAX_CONCURRENT=-1

# 16-core server with large model: limit to prevent memory issues
WHISPER_MAX_CONCURRENT=4

# GPU server with CUDA: allow more concurrency
WHISPER_MAX_CONCURRENT=16
```

### Model Auto-Download

On first use, Whisper automatically downloads models from Hugging Face to `~/.cache/whisper/`:

- **Network requirement**: Initial model download requires internet access
- **Disk space**: Ensure sufficient space (see model sizes above)
- **Pre-download**: For air-gapped environments, download models manually:
  ```bash
  whisper --model base --language en --task transcribe /dev/null
  ```
- **Custom cache location**: Set `WHISPER_MODELS_DIR` environment variable

### GPU Acceleration

Whisper supports GPU acceleration via CUDA or OpenCL for dramatically faster transcription:

**CUDA (NVIDIA GPUs)**:
```bash
# Ensure CUDA-enabled PyTorch is installed:
pip install openai-whisper torch torchvision torchaudio --index-url https://download.pytorch.org/whl/cu118

# Enable CUDA in configuration:
WHISPER_EXTRA_ARGS="--device cuda --fp16 True"
```

**Performance improvements**:
- **tiny/base**: 10-20x faster than CPU
- **medium**: 5-10x faster than CPU
- **large**: 3-5x faster than CPU (requires 6+ GB VRAM)

**Multi-GPU**: Whisper CLI uses one GPU. For multiple GPUs, run separate SIPREC instances with `CUDA_VISIBLE_DEVICES`.

### Remote Server Deployment

Whisper can run on a remote server accessed via SSH or HTTP wrapper:

**SSH wrapper approach**:
```bash
# On SIPREC server, create wrapper script: /usr/local/bin/whisper-remote
#!/bin/bash
ssh whisper-server "whisper $@"

# Configure SIPREC:
WHISPER_BINARY_PATH=/usr/local/bin/whisper-remote
WHISPER_TIMEOUT=20m  # Increase timeout for network latency
```

**HTTP API approach** (faster, recommended for production):
1. Deploy Whisper as HTTP service (e.g., [whisper-api](https://github.com/ahmetoner/whisper-asr-webservice))
2. Create wrapper that POSTs audio and returns JSON
3. Point `WHISPER_BINARY_PATH` to the wrapper

**Remote considerations**:
- Increase `WHISPER_TIMEOUT` to account for network latency and queuing
- Use SSH key authentication (no password prompts)
- Monitor network bandwidth (audio files can be large)
- Consider compressing WAV files before transmission

### Temporary File Management

Whisper creates temporary WAV files in the system temp directory:

- **Location**: Uses `os.TempDir()` (typically `/tmp` on Linux)
- **Cleanup**: Files are automatically deleted after transcription
- **Disk space monitoring**: Check Prometheus metric `siprec_whisper_temp_file_bytes`
- **Custom temp location**: Set `TMPDIR` environment variable:
  ```bash
  TMPDIR=/fast-ssd/tmp ./siprec
  ```

**For high-volume deployments**:
- Use fast SSD storage for `TMPDIR` (reduces model loading time)
- Ensure sufficient disk space (calculate: `concurrent_calls * avg_audio_size * 2`)
- Monitor temp directory with `df -h /tmp`

### Metrics & Monitoring

Whisper provider exposes Prometheus metrics:

- `siprec_whisper_cli_duration_seconds{model,status}`: CLI execution time histogram
- `siprec_whisper_temp_file_bytes`: Current temp file disk usage
- `siprec_whisper_timeouts_total{model}`: Timeout counter by model
- `siprec_whisper_output_format_total{format}`: Usage by output format

**Alerting recommendations**:
```yaml
# High timeout rate
- alert: WhisperTimeoutRate
  expr: rate(siprec_whisper_timeouts_total[5m]) > 0.1

# Slow transcription (> 2x realtime)
- alert: WhisperSlowTranscription
  expr: histogram_quantile(0.95, siprec_whisper_cli_duration_seconds_bucket) > 120
```

### Troubleshooting

**Version check fails**:
- For remote servers or custom wrappers, version check failure is expected (logged at DEBUG level)
- Ensure wrapper accepts `--version` flag or ignore the warning

**Model not found**:
```bash
# Manually download model
whisper --model small --language en --task transcribe /dev/null
```

**Timeout on large files**:
```bash
# Increase timeout for longer recordings
WHISPER_TIMEOUT=30m
```

**Out of memory**:
```bash
# Reduce concurrent calls or use smaller model
WHISPER_MAX_CONCURRENT=2
WHISPER_MODEL=small
```

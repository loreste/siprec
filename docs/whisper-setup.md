# Whisper Integration Guide

This walk-through explains how to install and operate [openai/whisper](https://github.com/openai/whisper) alongside the SIPREC server. Whisper can run on the same host or a remote GPU machine—SIPREC simply shells out to whatever executable you point it at.

---

## 1. Choose a Deployment Model

| Scenario | When to use it | Notes |
| --- | --- | --- |
| **Local CPU install** | Small deployments, POCs, dev laptops | Simplest; limited throughput |
| **Local GPU install** | Single box with NVIDIA GPU | Fastest per-call latency |
| **Remote GPU server** | Dedicated Whisper farm | SIPREC invokes Whisper via SSH/HTTP wrapper |
| **Containerized** | Kubernetes / Docker shops | Ship Whisper image separately from SIPREC |

Whichever option you pick, the interface is the same: SIPREC runs `WHISPER_BINARY_PATH` with standard CLI arguments and reads the transcription file it produces.

---

## 2. Install Whisper

### A. Local Python install (CPU/GPU)
```bash
python3 -m venv ~/venvs/whisper
source ~/venvs/whisper/bin/activate
pip install --upgrade pip
pip install openai-whisper

# Optional GPU acceleration (CUDA 11.8 example):
pip install torch torchvision torchaudio --index-url https://download.pytorch.org/whl/cu118
```

Whisper requires FFmpeg; install it via your package manager (`apt install ffmpeg` / `brew install ffmpeg`).

### B. Docker
```bash
docker run --gpus all -it --name whisper \
  -v /opt/whisper-cache:/root/.cache/whisper \
  ghcr.io/ggerganov/whisper.cpp:latest
```

Expose a small wrapper script that shells into the container:
```bash
#!/bin/bash
docker exec whisper whisper "$@"
```

### C. Remote SSH wrapper
On the SIPREC host:
```bash
cat >/usr/local/bin/whisper-remote <<'EOF'
#!/bin/bash
ssh -o BatchMode=yes whisper@gpu-server "/usr/local/bin/whisper $@"
EOF
chmod +x /usr/local/bin/whisper-remote
```

Ensure the remote box already has Whisper + FFmpeg installed and that SSH keys allow password-less access.

### D. HTTP API wrapper
1. Deploy an API such as [ahmetoner/whisper-asr-webservice](https://github.com/ahmetoner/whisper-asr-webservice) or your own Flask/FastAPI layer.
2. On the SIPREC host create a script that uploads the WAV to the API and writes the JSON response to the expected output file.
3. Point `WHISPER_BINARY_PATH` to that script.

#### Example: HTTP wrapper script
```bash
#!/bin/bash
set -euo pipefail

AUDIO_PATH="$1"
OUTPUT_DIR="."
MODEL="base"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output_dir) OUTPUT_DIR="$2"; shift 2;;
    --model) MODEL="$2"; shift 2;;
    --output_format) FORMAT="$2"; shift 2;;
    *) shift ;;
  esac
done

curl -s -X POST "https://whisper-api.example.com/transcribe" \
  -F "file=@${AUDIO_PATH}" \
  -F "model=${MODEL}" \
  -o "${OUTPUT_DIR}/$(basename "${AUDIO_PATH%.*}").json"
```

Make the script executable and set `WHISPER_BINARY_PATH` to it.

### E. Dedicated Whisper Farm (RabbitMQ/HTTP)

For large-scale deployments with independent scaling of transcription resources:

1. Deploy a Whisper dispatcher that accepts jobs over HTTP or a queue (Celery, RabbitMQ, Kafka).
2. Expose a lightweight client binary on the SIPREC nodes that serializes the WAV and posts it to the dispatcher.
3. Configure `WHISPER_BINARY_PATH` to the client executable and set higher `WHISPER_TIMEOUT` to accommodate queueing.
4. Monitor the dispatcher and autoscale GPU workers independently from SIPREC.

**Best Practices:**
- Use an internal load balancer in front of multiple Whisper workers.
- Return the transcription in the same response or provide a callback URL so the client can write the JSON output to disk.
- Enforce auth (mTLS or signed tokens) between SIPREC and the Whisper farm.

---

## 3. Configure SIPREC

Add Whisper to your supported vendors and set the required environment variables:
```bash
export SUPPORTED_VENDORS="google,whisper"
export STT_DEFAULT_VENDOR="whisper"

export WHISPER_ENABLED=true
export WHISPER_BINARY_PATH=/usr/local/bin/whisper-remote   # or real binary
export WHISPER_MODEL=small
export WHISPER_OUTPUT_FORMAT=json
export WHISPER_TIMEOUT=20m
export WHISPER_MAX_CONCURRENT=8    # limit to protect GPU/CPU
export WHISPER_EXTRA_ARGS="--device cuda --fp16 True"
```

Other useful knobs:
- `WHISPER_LANGUAGE` – force a language; leave empty to auto-detect.
- `WHISPER_TASK` / `WHISPER_TRANSLATE` – switch between transcription vs. translation.
- `TMPDIR` – change where temp WAV files are created (use a fast SSD).

Restart SIPREC after changing environment variables. On startup you should see logs like:
```
INFO  whisper provider initialized  binary=... model=small outputFormat=json
INFO  Whisper rate limiting enabled max_concurrent=8
```

---

## 4. Validate the Setup

1. **Manual CLI check**
   ```bash
   whisper sample.wav --model small --language en --task transcribe
   ```
   Ensure the CLI produces a JSON/TXT file and caches the model successfully.

2. **SIPREC dry run**
   - Start SIPREC with Whisper enabled.
   - Place a short SIPREC call.
   - Watch logs for `Whisper transcription completed`.

3. **Prometheus metrics**
   - `siprec_whisper_cli_duration_seconds{model,status}` – runtime histogram.
   - `siprec_whisper_temp_file_bytes` – total temp disk usage.
   - `siprec_whisper_timeouts_total{model}` – verify it stays at zero.

4. **Troubleshoot**
   - `context deadline exceeded` → increase `WHISPER_TIMEOUT` or reduce concurrency.
   - `model not found` → pre-download models or set `WHISPER_MODELS_DIR`.
   - Large temp usage → point `TMPDIR` to larger disk or enable cleanup cron.

---

## 5. Hardening & Operations

- **Model caching**: Pre-warm each server with `whisper --model small --task transcribe /dev/null` so production traffic doesn’t block on downloads.
- **Concurrency planning**: Size `WHISPER_MAX_CONCURRENT` based on CPU/GPU limits. Large models need several GB of RAM/VRAM per invocation.
- **Out-of-process scaling**: If multiple SIPREC instances share one Whisper cluster, deploy Whisper behind an HTTP queue (Celery, RabbitMQ, etc.) and let SIPREC’s binary wrapper enqueue jobs.
- **Monitoring**: Build Grafana panels for the Whisper metrics plus OS-level CPU/GPU utilization. Set alerts for timeout rate, high duration, or temp disk saturation.
- **Upgrades**: Whisper is a Python project; use virtualenvs or containers to keep it isolated from the SIPREC Go binary.

With these steps completed, SIPREC will stream PCM audio to Whisper, ingest the generated transcripts, and forward results through the normal WebSocket/AMQP listeners alongside the other STT providers.

# STT Provider Configuration Guide

This guide explains how to configure different Speech-to-Text (STT) providers with the SIPREC server, including setting up API keys and provider-specific options.

## Overview

The SIPREC server supports multiple STT providers with circuit breaker protection and fallback capabilities:

- **Google Cloud Speech-to-Text** - High accuracy, supports many languages
- **Deepgram** - Fast, cost-effective, specialized for conversation
- **Azure Speech Services** - Microsoft's cloud speech service
- **Amazon Transcribe** - AWS speech recognition service  
- **OpenAI Whisper** - Advanced AI-powered transcription

## Quick Start

1. **Choose your STT provider(s)**
2. **Obtain API credentials** from your chosen provider(s)
3. **Configure environment variables** (see examples below)
4. **Set the default provider** and enable desired providers
5. **Test your configuration**

## General STT Configuration

```bash
# Supported STT vendors (comma-separated list)
SUPPORTED_VENDORS=google,deepgram,azure,amazon,openai

# Default STT vendor to use
DEFAULT_SPEECH_VENDOR=google

# Supported audio codecs
SUPPORTED_CODECS=PCMU,PCMA,G722
```

## Provider-Specific Configuration

### Google Cloud Speech-to-Text

Google STT supports two authentication methods:

#### Method 1: Service Account Credentials (Recommended)

```bash
# Enable Google STT
GOOGLE_STT_ENABLED=true

# Path to service account JSON file
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json

# Google Cloud project ID
GOOGLE_PROJECT_ID=your-project-id
```

#### Method 2: API Key

```bash
# Enable Google STT
GOOGLE_STT_ENABLED=true

# Google STT API key
GOOGLE_STT_API_KEY=your-google-api-key

# Google Cloud project ID
GOOGLE_PROJECT_ID=your-project-id
```

#### Advanced Google STT Settings

```bash
# Language and audio settings
GOOGLE_STT_LANGUAGE=en-US
GOOGLE_STT_SAMPLE_RATE=16000

# Model selection
GOOGLE_STT_ENHANCED_MODELS=false
GOOGLE_STT_MODEL=latest_long  # latest_long, latest_short, command_and_search

# Feature toggles
GOOGLE_STT_AUTO_PUNCTUATION=true
GOOGLE_STT_WORD_TIME_OFFSETS=true
GOOGLE_STT_MAX_ALTERNATIVES=1
GOOGLE_STT_PROFANITY_FILTER=false
```

### Deepgram

Deepgram provides fast, accurate speech recognition optimized for conversations.

```bash
# Enable Deepgram STT
DEEPGRAM_STT_ENABLED=true

# Deepgram API key (required)
DEEPGRAM_API_KEY=your-deepgram-api-key

# API settings
DEEPGRAM_API_URL=https://api.deepgram.com
DEEPGRAM_MODEL=nova-2  # nova-2, nova, enhanced, base
DEEPGRAM_LANGUAGE=en-US
DEEPGRAM_TIER=nova     # nova, enhanced, base
DEEPGRAM_VERSION=latest

# Feature settings
DEEPGRAM_PUNCTUATE=true
DEEPGRAM_DIARIZE=false
DEEPGRAM_NUMERALS=true
DEEPGRAM_SMART_FORMAT=true
DEEPGRAM_PROFANITY_FILTER=false

# Privacy and customization
DEEPGRAM_REDACT=ssn,credit_card_number  # Comma-separated sensitive data types
DEEPGRAM_KEYWORDS=medical,insurance,claim  # Comma-separated keywords to boost
```

### Azure Speech Services

Microsoft Azure's speech recognition service.

```bash
# Enable Azure STT
AZURE_STT_ENABLED=true

# Azure credentials (required)
AZURE_SPEECH_KEY=your-azure-speech-key
AZURE_SPEECH_REGION=eastus

# Language and output settings
AZURE_STT_LANGUAGE=en-US
AZURE_STT_DETAILED_RESULTS=true
AZURE_STT_PROFANITY_FILTER=masked  # masked, removed, raw
AZURE_STT_OUTPUT_FORMAT=detailed   # detailed, simple

# Optional: Custom endpoint
AZURE_STT_ENDPOINT=https://your-custom-endpoint.cognitiveservices.azure.com
```

### Amazon Transcribe

AWS's speech recognition service.

```bash
# Enable Amazon Transcribe
AMAZON_STT_ENABLED=true

# AWS credentials (required)
AWS_ACCESS_KEY_ID=your-aws-access-key
AWS_SECRET_ACCESS_KEY=your-aws-secret-key
AWS_REGION=us-east-1

# Transcription settings
AMAZON_STT_LANGUAGE=en-US
AMAZON_STT_MEDIA_FORMAT=wav
AMAZON_STT_SAMPLE_RATE=16000

# Speaker identification
AMAZON_STT_CHANNEL_ID=false
AMAZON_STT_SPEAKER_ID=false
AMAZON_STT_MAX_SPEAKERS=2

# Optional: Custom vocabulary
AMAZON_STT_VOCABULARY=my-custom-vocabulary
```

### OpenAI Whisper

OpenAI's advanced AI-powered transcription service.

```bash
# Enable OpenAI Whisper
OPENAI_STT_ENABLED=true

# OpenAI credentials (required)
OPENAI_API_KEY=your-openai-api-key

# Optional: Organization ID
OPENAI_ORGANIZATION_ID=your-org-id

# Model and output settings
OPENAI_STT_MODEL=whisper-1
OPENAI_STT_RESPONSE_FORMAT=verbose_json  # json, text, srt, verbose_json, vtt
OPENAI_STT_TEMPERATURE=0.0

# Optional: Language (leave empty for auto-detection)
OPENAI_STT_LANGUAGE=en

# Optional: Context prompt for better accuracy
OPENAI_STT_PROMPT="This is a business call about insurance claims"

# Optional: Custom endpoint
OPENAI_BASE_URL=https://api.openai.com/v1
```

## Circuit Breaker Configuration

Protect against STT provider failures with circuit breakers:

```bash
# Enable circuit breakers
CIRCUIT_BREAKER_ENABLED=true

# STT-specific circuit breaker settings
STT_CB_FAILURE_THRESHOLD=3    # Failures before opening circuit
STT_CB_TIMEOUT=30s           # Time before attempting to close circuit
STT_CB_REQUEST_TIMEOUT=45s   # Individual request timeout
```

## Async STT Processing

Enable asynchronous processing for better performance:

```bash
# Enable async STT processing
STT_ASYNC_ENABLED=true

# Queue configuration
STT_QUEUE_SIZE=1000
STT_WORKER_COUNT=10
STT_BATCH_SIZE=5
STT_BATCH_TIMEOUT=2s
STT_RETRY_COUNT=3
STT_RETRY_DELAY=1s
STT_PRIORITY_QUEUE=true
STT_QUEUE_TIMEOUT=30s
STT_JOB_TIMEOUT=300s
```

## Provider Selection Strategy

The SIPREC server will:

1. **Use the default provider** specified by `DEFAULT_SPEECH_VENDOR`
2. **Fall back to other enabled providers** if the default fails
3. **Apply circuit breaker protection** to avoid cascading failures
4. **Load balance** between providers (if multiple are enabled)

## Best Practices

### Security

- **Never commit API keys** to version control
- **Use environment variables** or secure secret management
- **Rotate API keys** regularly
- **Use service accounts** with minimal required permissions

### Performance

- **Enable async processing** for high-volume scenarios
- **Configure circuit breakers** to handle provider outages
- **Use provider-specific optimizations** (models, features)
- **Monitor latency and accuracy** for each provider

### Cost Optimization

- **Understand pricing models** for each provider
- **Use appropriate models** (basic vs enhanced)
- **Enable only needed features** (diarization, punctuation)
- **Set up monitoring** for usage and costs

### Reliability

- **Enable multiple providers** for redundancy
- **Configure appropriate timeouts** for your use case
- **Test failover scenarios** regularly
- **Monitor circuit breaker status**

## Testing Your Configuration

1. **Start the SIPREC server** with your configuration
2. **Check the logs** for provider initialization messages
3. **Test with a sample call** to verify transcription works
4. **Monitor metrics** at `/metrics` endpoint
5. **Test failover** by temporarily disabling providers

## Provider Comparison

| Provider | Strengths | Use Cases | Pricing Model |
|----------|-----------|-----------|---------------|
| **Google** | High accuracy, many languages | General purpose, multilingual | Per-minute |
| **Deepgram** | Fast, conversation-optimized | Real-time, call centers | Per-minute, volume discounts |
| **Azure** | Enterprise integration | Microsoft ecosystems | Per-minute |
| **Amazon** | AWS integration, custom vocab | AWS workflows, specialized domains | Per-minute |
| **OpenAI** | Advanced AI, context-aware | Complex audio, multilingual | Per-minute |

## Troubleshooting

### Common Issues

**Provider not initializing:**
- Check API key/credentials are correct
- Verify network connectivity to provider
- Check provider service status

**High error rates:**
- Review circuit breaker settings
- Check audio format compatibility
- Verify provider quotas/limits

**Poor transcription quality:**
- Adjust model selection
- Tune provider-specific settings
- Check audio quality and format

**Performance issues:**
- Enable async processing
- Adjust worker count and batch size
- Monitor provider response times

### Debug Settings

```bash
# Enable debug logging
LOG_LEVEL=debug

# Monitor circuit breaker status
CB_MONITORING_ENABLED=true
CB_MONITORING_INTERVAL=30s
```

## Example Configurations

See the [example configuration file](../examples/stt_configuration_example.env) for complete working examples of each provider configuration.

## Support

For provider-specific issues:
- **Google**: Check Google Cloud Console and documentation
- **Deepgram**: Refer to Deepgram developer docs and status page
- **Azure**: Use Azure portal and cognitive services docs
- **Amazon**: Check AWS Transcribe console and documentation
- **OpenAI**: Monitor OpenAI status page and API documentation

For SIPREC-specific configuration issues, check the server logs and metrics endpoints.
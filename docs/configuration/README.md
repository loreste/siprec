# Configuration Guide

This section covers all configuration options for the SIPREC Server.

## Configuration Files

- [Environment Variables](ENVIRONMENT.md) - Complete list of environment variables
- [Configuration Reference](REFERENCE.md) - Detailed configuration parameter reference
- [Example Configurations](EXAMPLES.md) - Sample configurations for different scenarios

## Quick Configuration

The SIPREC Server uses environment variables for configuration. Create a `.env` file in the project root:

```bash
# Required Configuration
SIP_HOST=0.0.0.0
SIP_PORT=5060
HTTP_HOST=0.0.0.0
HTTP_PORT=8080

# STT Provider (choose one)
STT_PROVIDER=openai
OPENAI_API_KEY=your-api-key

# Optional: Message Queue
AMQP_ENABLED=true
AMQP_URL=amqp://guest:guest@localhost:5672/
```

## Configuration Hierarchy

1. Environment variables (highest priority)
2. `.env` file
3. Default values (lowest priority)

## Validation

The server validates all configuration on startup and will fail fast with clear error messages if required configuration is missing or invalid.

## Next Steps

- [Environment Variables Reference](ENVIRONMENT.md) for complete configuration options
- [Examples](EXAMPLES.md) for common configuration scenarios
- [Production Configuration](../operations/PRODUCTION_DEPLOYMENT.md) for production settings
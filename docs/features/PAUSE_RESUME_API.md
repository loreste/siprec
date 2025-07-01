# Pause/Resume Control API

The SIPREC server provides a comprehensive REST API for controlling the recording and transcription of active sessions. This feature allows real-time pause and resume operations for both individual sessions and all active sessions.

## Overview

The Pause/Resume API enables:
- **Per-session control** - Pause/resume individual recording sessions
- **Global control** - Pause/resume all active sessions simultaneously  
- **Granular control** - Pause recording, transcription, or both independently
- **Status monitoring** - Get current pause status and duration metrics
- **Secure access** - Optional API key authentication
- **Real-time operation** - Immediate effect on ongoing streams

## Configuration

### Environment Variables

```bash
# Enable the pause/resume API
PAUSE_RESUME_ENABLED=true

# Authentication settings
PAUSE_RESUME_REQUIRE_AUTH=true
PAUSE_RESUME_API_KEY=your-secure-api-key

# Control granularity
PAUSE_RESUME_PER_SESSION=true

# Default behavior when API is called
PAUSE_RECORDING=true
PAUSE_TRANSCRIPTION=true

# Optional: Maximum pause duration and auto-resume
MAX_PAUSE_DURATION=1h
PAUSE_AUTO_RESUME=false

# Notifications
PAUSE_RESUME_NOTIFICATIONS=true
```

### JSON Configuration

```json
{
  "pause_resume": {
    "enabled": true,
    "require_auth": true,
    "api_key": "your-secure-api-key",
    "per_session": true,
    "pause_recording": true,
    "pause_transcription": true,
    "send_notifications": true,
    "max_pause_duration": "1h",
    "auto_resume": false
  }
}
```

## API Endpoints

### Session-Specific Control

#### Pause Session
**POST** `/api/sessions/{session_id}/pause`

Pause recording and/or transcription for a specific session.

**Headers:**
- `Content-Type: application/json`
- `X-API-Key: your-api-key` (if authentication enabled)

**Request Body:**
```json
{
  "pause_recording": true,     // Optional, defaults to config
  "pause_transcription": false // Optional, defaults to config
}
```

**Response:**
```json
{
  "session_id": "session-123",
  "is_paused": true,
  "recording_paused": true,
  "transcription_paused": false,
  "paused_at": "2023-12-01T10:30:00Z",
  "pause_duration": "0s"
}
```

#### Resume Session
**POST** `/api/sessions/{session_id}/resume`

Resume recording and transcription for a specific session.

**Headers:**
- `X-API-Key: your-api-key` (if authentication enabled)

**Response:**
```json
{
  "session_id": "session-123",
  "is_paused": false,
  "recording_paused": false,
  "transcription_paused": false,
  "paused_at": null,
  "pause_duration": "0s"
}
```

#### Get Pause Status
**GET** `/api/sessions/{session_id}/pause-status`

Get current pause status for a specific session.

**Headers:**
- `X-API-Key: your-api-key` (if authentication enabled)

**Response:**
```json
{
  "session_id": "session-123",
  "is_paused": true,
  "recording_paused": true,
  "transcription_paused": false,
  "paused_at": "2023-12-01T10:30:00Z",
  "pause_duration": "2m30s"
}
```

### Global Control

#### Pause All Sessions
**POST** `/api/sessions/pause-all`

Pause all active recording sessions.

**Headers:**
- `Content-Type: application/json`
- `X-API-Key: your-api-key` (if authentication enabled)

**Request Body:**
```json
{
  "pause_recording": true,
  "pause_transcription": true
}
```

**Response:**
```json
{
  "message": "All sessions paused",
  "statuses": {
    "session-123": {
      "session_id": "session-123",
      "is_paused": true,
      "recording_paused": true,
      "transcription_paused": true,
      "paused_at": "2023-12-01T10:30:00Z"
    },
    "session-456": {
      "session_id": "session-456", 
      "is_paused": true,
      "recording_paused": true,
      "transcription_paused": true,
      "paused_at": "2023-12-01T10:30:00Z"
    }
  }
}
```

#### Resume All Sessions
**POST** `/api/sessions/resume-all`

Resume all paused recording sessions.

**Headers:**
- `X-API-Key: your-api-key` (if authentication enabled)

**Response:**
```json
{
  "message": "All sessions resumed",
  "statuses": {
    "session-123": {
      "session_id": "session-123",
      "is_paused": false,
      "recording_paused": false,
      "transcription_paused": false,
      "paused_at": null
    }
  }
}
```

#### Get All Pause Statuses
**GET** `/api/sessions/pause-status`

Get pause status for all active sessions.

**Headers:**
- `X-API-Key: your-api-key` (if authentication enabled)

**Response:**
```json
{
  "session-123": {
    "session_id": "session-123",
    "is_paused": true,
    "recording_paused": true,
    "transcription_paused": false,
    "paused_at": "2023-12-01T10:30:00Z",
    "pause_duration": "5m12s"
  },
  "session-456": {
    "session_id": "session-456",
    "is_paused": false,
    "recording_paused": false,
    "transcription_paused": false,
    "paused_at": null,
    "pause_duration": "0s"
  }
}
```

## Usage Examples

### Command Line Examples

```bash
# Pause only recording for a specific session
curl -X POST http://localhost:8080/api/sessions/call-abc123/pause \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"pause_recording": true, "pause_transcription": false}'

# Pause only transcription
curl -X POST http://localhost:8080/api/sessions/call-abc123/pause \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"pause_recording": false, "pause_transcription": true}'

# Pause both recording and transcription
curl -X POST http://localhost:8080/api/sessions/call-abc123/pause \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"pause_recording": true, "pause_transcription": true}'

# Resume a session
curl -X POST http://localhost:8080/api/sessions/call-abc123/resume \
  -H "X-API-Key: your-api-key"

# Check status
curl -H "X-API-Key: your-api-key" \
  http://localhost:8080/api/sessions/call-abc123/pause-status

# Emergency pause all sessions
curl -X POST http://localhost:8080/api/sessions/pause-all \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"pause_recording": true, "pause_transcription": true}'

# Resume all sessions
curl -X POST http://localhost:8080/api/sessions/resume-all \
  -H "X-API-Key: your-api-key"
```

### Python Client Example

```python
import requests
import json

class SiprecPauseResumeClient:
    def __init__(self, base_url, api_key=None):
        self.base_url = base_url.rstrip('/')
        self.api_key = api_key
        
    def _headers(self):
        headers = {'Content-Type': 'application/json'}
        if self.api_key:
            headers['X-API-Key'] = self.api_key
        return headers
    
    def pause_session(self, session_id, pause_recording=True, pause_transcription=True):
        """Pause recording and/or transcription for a session."""
        url = f"{self.base_url}/api/sessions/{session_id}/pause"
        data = {
            "pause_recording": pause_recording,
            "pause_transcription": pause_transcription
        }
        response = requests.post(url, headers=self._headers(), json=data)
        return response.json()
    
    def resume_session(self, session_id):
        """Resume a paused session."""
        url = f"{self.base_url}/api/sessions/{session_id}/resume"
        response = requests.post(url, headers=self._headers())
        return response.json()
    
    def get_pause_status(self, session_id):
        """Get pause status for a session."""
        url = f"{self.base_url}/api/sessions/{session_id}/pause-status"
        response = requests.get(url, headers=self._headers())
        return response.json()
    
    def pause_all_sessions(self, pause_recording=True, pause_transcription=True):
        """Pause all active sessions."""
        url = f"{self.base_url}/api/sessions/pause-all"
        data = {
            "pause_recording": pause_recording,
            "pause_transcription": pause_transcription
        }
        response = requests.post(url, headers=self._headers(), json=data)
        return response.json()
    
    def resume_all_sessions(self):
        """Resume all paused sessions."""
        url = f"{self.base_url}/api/sessions/resume-all"
        response = requests.post(url, headers=self._headers())
        return response.json()

# Usage
client = SiprecPauseResumeClient("http://localhost:8080", "your-api-key")

# Pause a session
status = client.pause_session("call-123", pause_recording=True, pause_transcription=False)
print(f"Session paused: {status}")

# Check status
status = client.get_pause_status("call-123")
print(f"Current status: {status}")

# Resume the session
status = client.resume_session("call-123")
print(f"Session resumed: {status}")
```

## Technical Implementation

### Thread Safety
- All pause/resume operations are thread-safe using mutex synchronization
- Concurrent API calls are handled safely without race conditions
- Real-time status updates across multiple goroutines

### Performance Impact
- **Minimal Overhead**: Pause operations have negligible performance impact
- **Immediate Effect**: Changes take effect within milliseconds
- **Memory Efficient**: Pausable streams use minimal additional memory
- **Non-blocking**: Pause operations don't block other session operations

### Stream Behavior
- **Recording Pause**: Audio data is dropped during pause, file writing stops
- **Transcription Pause**: STT provider input stream blocks, preventing processing
- **Resume**: Streams immediately resume normal operation
- **No Data Loss**: Resume operation is instantaneous with no buffering delays

## Error Handling

### HTTP Status Codes

- **200 OK**: Operation successful
- **400 Bad Request**: Invalid session ID or malformed request
- **401 Unauthorized**: Missing or invalid API key
- **404 Not Found**: Session not found
- **405 Method Not Allowed**: Invalid HTTP method
- **500 Internal Server Error**: Server error during operation

### Error Response Format

```json
{
  "error": "session not found: session-123",
  "code": 404
}
```

## Monitoring and Metrics

### Prometheus Metrics

The API automatically records metrics for monitoring:

```
# Session pause events
siprec_sessions_paused_total{session_id="session-123",pause_type="both"}

# Session resume events  
siprec_sessions_resumed_total{session_id="session-123"}

# Pause duration tracking
siprec_session_pause_duration_seconds{session_id="session-123",pause_type="recording"}
```

### Structured Logging

All operations are logged with structured context:

```json
{
  "level": "info",
  "message": "Session paused via API",
  "session_id": "session-123",
  "pause_recording": true,
  "pause_transcription": false,
  "timestamp": "2023-12-01T10:30:00Z"
}
```

## Security Considerations

### Authentication
- API key authentication prevents unauthorized access
- Keys should be rotated regularly and stored securely
- Consider rate limiting for production deployments

### Authorization
- All pause/resume operations are logged for audit trails
- Consider implementing role-based access for different pause capabilities
- Monitor for unusual pause/resume patterns

### Best Practices
- Use HTTPS in production to protect API keys
- Implement proper key management and rotation
- Consider implementing request throttling
- Monitor API usage patterns for security anomalies

## Troubleshooting

### Common Issues

**Session Not Found**
```bash
# Check if session exists
curl -H "X-API-Key: your-api-key" \
  http://localhost:8080/api/sessions
```

**Authentication Failed**
```bash
# Verify API key is correct
curl -H "X-API-Key: wrong-key" \
  http://localhost:8080/api/sessions/pause-status
```

**API Disabled**
```bash
# Check configuration
grep PAUSE_RESUME_ENABLED /etc/siprec/.env
```

### Debug Mode

Enable debug logging to troubleshoot issues:

```bash
# Set log level
LOG_LEVEL=debug

# Check logs
journalctl -u siprec-server -f | grep pause_resume
```

## Integration Examples

### With Monitoring Systems

```bash
# Grafana Alert: Pause all sessions if CPU > 90%
curl -X POST http://siprec:8080/api/sessions/pause-all \
  -H "X-API-Key: $SIPREC_API_KEY" \
  -d '{"pause_recording": false, "pause_transcription": true}'
```

### With Load Balancers

```bash
# Pause sessions before maintenance
for server in siprec-1 siprec-2 siprec-3; do
  curl -X POST http://$server:8080/api/sessions/pause-all \
    -H "X-API-Key: $API_KEY"
done
```

### With Container Orchestration

```yaml
# Kubernetes pre-stop hook
spec:
  containers:
  - name: siprec
    lifecycle:
      preStop:
        exec:
          command: 
          - curl
          - -X POST
          - http://localhost:8080/api/sessions/pause-all
          - -H "X-API-Key: ${SIPREC_API_KEY}"
```
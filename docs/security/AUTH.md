# Authentication & Authorization

Comprehensive guide for securing access to the SIPREC Server.

## Authentication Methods

### API Key Authentication

The primary authentication method for the HTTP API:

```bash
# Configure API key
API_KEY_ENABLED=true
API_KEY_HEADER=X-API-Key
API_KEYS=key1:service1,key2:service2
```

**Usage:**
```bash
curl -H "X-API-Key: key1" http://localhost:8080/api/sessions
```

### SIP Digest Authentication

For SIP endpoints requiring authentication:

```bash
# Enable SIP authentication
SIP_AUTH_ENABLED=true
SIP_AUTH_REALM=siprec.example.com
SIP_AUTH_USERS=user1:pass1,user2:pass2
```

### JWT Authentication (Optional)

For advanced deployments:

```bash
# Enable JWT
JWT_ENABLED=true
JWT_SECRET=your-secret-key
JWT_ISSUER=siprec.example.com
JWT_EXPIRY=24h
```

## Authorization

### Role-Based Access Control (RBAC)

Define roles and permissions:

```yaml
roles:
  admin:
    - sessions:read
    - sessions:write
    - sessions:delete
    - config:read
    - config:write
    
  operator:
    - sessions:read
    - sessions:write
    - config:read
    
  viewer:
    - sessions:read
```

### API Endpoint Authorization

Endpoints and required permissions:

| Endpoint | Method | Permission |
|----------|--------|------------|
| /api/sessions | GET | sessions:read |
| /api/sessions | POST | sessions:write |
| /api/sessions/{id} | DELETE | sessions:delete |
| /api/config | GET | config:read |
| /api/config | PUT | config:write |
| /metrics | GET | metrics:read |

## WebSocket Authentication

### Connection Authentication

WebSocket connections require authentication:

```javascript
// Client-side
const ws = new WebSocket('ws://localhost:8080/ws', {
  headers: {
    'Authorization': 'Bearer ' + token
  }
});
```

### Message Authentication

Each message can include authentication:

```json
{
  "type": "subscribe",
  "session_id": "123",
  "auth": {
    "token": "bearer-token"
  }
}
```

## Security Headers

Configure security headers for HTTP responses:

```bash
# Security headers
SECURITY_HEADERS_ENABLED=true
SECURITY_HEADER_HSTS=max-age=31536000
SECURITY_HEADER_CSP=default-src 'self'
SECURITY_HEADER_FRAME=DENY
SECURITY_HEADER_CONTENT_TYPE=nosniff
SECURITY_HEADER_XSS=1; mode=block
```

## Rate Limiting

### Global Rate Limiting

Prevent abuse with rate limiting:

```bash
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS=100
RATE_LIMIT_WINDOW=1m
RATE_LIMIT_BURST=20
```

### Per-Endpoint Limits

Configure specific endpoints:

```yaml
rate_limits:
  - path: /api/sessions
    method: POST
    requests: 10
    window: 1m
    
  - path: /api/transcribe
    method: POST
    requests: 5
    window: 1m
```

## IP Whitelisting

Restrict access by IP:

```bash
IP_WHITELIST_ENABLED=true
IP_WHITELIST=192.168.1.0/24,10.0.0.0/8
IP_WHITELIST_ENFORCEMENT=strict
```

## Audit Logging

### Enable Audit Logs

Track all authentication events:

```bash
AUDIT_LOG_ENABLED=true
AUDIT_LOG_FILE=/var/log/siprec/audit.log
AUDIT_LOG_EVENTS=auth,access,config
```

### Audit Log Format

```json
{
  "timestamp": "2024-01-15T10:00:00Z",
  "event": "auth.success",
  "user": "service1",
  "ip": "192.168.1.100",
  "endpoint": "/api/sessions",
  "method": "GET",
  "status": 200
}
```

## Integration with External Auth

### LDAP/Active Directory

```bash
AUTH_PROVIDER=ldap
LDAP_URL=ldap://ldap.example.com:389
LDAP_BASE_DN=dc=example,dc=com
LDAP_USER_DN=cn=users
LDAP_BIND_DN=cn=admin,dc=example,dc=com
LDAP_BIND_PASSWORD=secret
```

### OAuth2/OIDC

```bash
AUTH_PROVIDER=oauth2
OAUTH2_PROVIDER=google
OAUTH2_CLIENT_ID=your-client-id
OAUTH2_CLIENT_SECRET=your-secret
OAUTH2_REDIRECT_URL=http://localhost:8080/auth/callback
```

### SAML

```bash
AUTH_PROVIDER=saml
SAML_IDP_URL=https://idp.example.com
SAML_SP_CERT=/etc/siprec/saml/sp.crt
SAML_SP_KEY=/etc/siprec/saml/sp.key
```

## Best Practices

### API Key Management

1. **Generation**
   - Use cryptographically secure random generation
   - Minimum 32 characters
   - Include alphanumeric and special characters

2. **Storage**
   - Never store in plain text
   - Use one-way hashing (bcrypt/scrypt)
   - Separate from application data

3. **Rotation**
   - Regular rotation schedule
   - Support multiple active keys
   - Graceful deprecation

### Session Management

1. **Session Security**
   - Use secure session IDs
   - Implement session timeout
   - Invalidate on logout

2. **Token Management**
   - Short expiration times
   - Refresh token support
   - Revocation capability

## Security Monitoring

### Failed Authentication

Monitor and alert on:
- Multiple failed attempts
- Unusual access patterns
- Expired credential usage
- Invalid API keys

### Access Patterns

Track:
- Unusual access times
- Geographic anomalies
- High-frequency requests
- Unauthorized access attempts

## Example Configurations

### Basic Security

```bash
# .env.secure-basic
API_KEY_ENABLED=true
API_KEYS=prod-key-1:production-service
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS=1000
RATE_LIMIT_WINDOW=1m
AUDIT_LOG_ENABLED=true
```

### Enterprise Security

```bash
# .env.secure-enterprise
AUTH_PROVIDER=oauth2
OAUTH2_PROVIDER=okta
OAUTH2_CLIENT_ID=siprec-app
OAUTH2_CLIENT_SECRET=secret
IP_WHITELIST_ENABLED=true
IP_WHITELIST=10.0.0.0/8
RATE_LIMIT_ENABLED=true
AUDIT_LOG_ENABLED=true
SECURITY_HEADERS_ENABLED=true
```

### Zero Trust Security

```bash
# .env.zero-trust
AUTH_PROVIDER=mtls
MTLS_CA_CERT=/etc/siprec/ca.crt
MTLS_VERIFY_MODE=require
IP_WHITELIST_ENABLED=true
IP_WHITELIST=10.0.1.0/24
SESSION_TIMEOUT=15m
AUDIT_LOG_ENABLED=true
AUDIT_LOG_VERBOSE=true
```
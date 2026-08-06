# Security

> VigilAgent's security architecture, policies, and implementation details.

---

## Table of Contents

- [Security Philosophy](#security-philosophy)
- [Authentication](#authentication)
- [Authorization](#authorization)
- [Encryption](#encryption)
- [Input Validation & Sanitization](#input-validation--sanitization)
- [Credential Scanning & Redaction](#credential-scanning--redaction)
- [Rate Limiting & Abuse Prevention](#rate-limiting--abuse-prevention)
- [Security Headers](#security-headers)
- [IP Filtering](#ip-filtering)
- [Account Lockout](#account-lockout)
- [CORS Policy](#cors-policy)
- [Idempotency & Replay Protection](#idempotency--replay-protection)
- [Webhook Security](#webhook-security)
- [Compliance Engine](#compliance-engine)
- [Audit Logging](#audit-logging)
- [Dependency Security](#dependency-security)
- [Reporting Vulnerabilities](#reporting-vulnerabilities)

---

## Security Philosophy

VigilAgent implements a **defense-in-depth** strategy. Every HTTP request passes through 13+ middleware layers before reaching business logic. The system is designed so that a failure in any single security layer does not compromise the entire system.

**Core Principles:**
1. **Zero Trust**: No request is trusted by default. All requests are authenticated and authorized.
2. **Least Privilege**: API keys support scoped permissions. Users only access resources they own.
3. **Fail Secure**: Authentication failures result in explicit rejection, never silent fallthrough.
4. **Secrets Never in Plaintext**: API keys are stored as SHA-256 hashes. Passwords use bcrypt. Sensitive data uses AES-256-GCM.

---

## Authentication

### JWT Tokens

VigilAgent uses JSON Web Tokens (`golang-jwt/v5`) for session authentication.

**Token Configuration:**

| Parameter             | Description                                      | Default     |
|-----------------------|--------------------------------------------------|-------------|
| `JWT_SECRET`          | HMAC-SHA256 signing secret                       | Required    |
| `JWT_EXPIRATION`      | Token lifetime                                   | 24 hours    |
| `JWT_AUDIENCE`        | Expected audience claim                          | `vigilagent`|
| `JWT_BIND_TO_IP`      | Bind tokens to client IP address                 | `false`     |
| `JWT_BIND_TO_USERAGENT` | Bind tokens to client User-Agent              | `false`     |

**Security Features:**
- **IP Binding**: When enabled, tokens are cryptographically bound to the client's IP address. Stolen tokens are useless from a different IP.
- **User-Agent Binding**: Tokens are bound to the client's User-Agent string, preventing cross-browser token theft.
- **Token Blacklisting**: Logged-out tokens are added to a Redis blacklist and rejected on subsequent requests.
- **Session Tracking**: All active sessions are tracked and can be individually invalidated.

### API Keys

For programmatic access, VigilAgent supports API key authentication.

**Implementation:**
- Keys are generated with a configurable prefix (e.g., `vgl_sk_`)
- The raw key is returned **only once** at creation time
- Keys are stored as **SHA-256 hashes** in the `api_keys` table
- Keys support scoped permissions (`read`, `write`, `scan`, `admin`)
- Key rotation creates a new key and invalidates the old one

**Authentication Flow:**
```
Client sends: X-API-Key: vgl_sk_abc123...
Server computes: SHA-256(vgl_sk_abc123...)
Server looks up: hash in api_keys table
Server validates: scopes, expiration, active status
```

### Password Security

- Passwords are hashed using **bcrypt** with a cost factor of 12
- Password strength policies are enforced at registration and password change
- Password reset tokens are single-use and time-limited

---

## Authorization

### Role-Based Access Control (RBAC)

| Role    | Permissions                                           |
|---------|-------------------------------------------------------|
| `user`  | Own resources (projects, agents, tasks, sessions)     |
| `member`| Organization resources within assigned teams          |
| `admin` | Full system access, user management, feature flags    |

### Scope-Based API Key Permissions

| Scope   | Access Level                                          |
|---------|-------------------------------------------------------|
| `read`  | GET endpoints only                                    |
| `write` | GET, POST, PUT, DELETE on own resources               |
| `scan`  | Code scanning and analysis endpoints                  |
| `admin` | Full API access including admin endpoints             |

### Resource Ownership

All resource access is filtered by ownership. A user can only:
- Access organizations they belong to
- Access projects within their organizations
- Access agents and sessions they created
- Manage API keys they own

---

## Encryption

### Data at Rest

**AES-256-GCM Encryption** (`internal/security/security.go`):
- Algorithm: AES-256-GCM (Galois/Counter Mode) providing authenticated encryption
- Key Derivation: PBKDF2 with **100,000 iterations** and SHA-256
- Salt: Cryptographically random 32-byte salt per encryption operation
- Nonce: Cryptographically random nonce per encryption operation

```go
// Encryption flow
Salt       = crypto/rand (32 bytes)
DerivedKey = PBKDF2(passphrase, salt, 100000, 32, SHA-256)
Nonce      = crypto/rand (GCM nonce size)
Ciphertext = AES-256-GCM.Seal(nonce, plaintext, additionalData)
Output     = Salt || Nonce || Ciphertext
```

### Data in Transit

- TLS termination supported on the proxy binary (`cmd/proxy`)
- HSTS header enforced (`Strict-Transport-Security: max-age=31536000; includeSubDomains`)
- All internal service communication should use TLS in production

---

## Input Validation & Sanitization

The `internal/security/` package provides comprehensive input sanitization:

### `SanitizeInput(input string) string`
- Removes null bytes (`\x00`)
- Strips non-printable control characters (ASCII 0–31, 127) while preserving tabs, newlines, and carriage returns
- Applied to all user-supplied text fields

### `SanitizeFilename(filename string) string`
- Strips path traversal sequences (`..`, `/`, `\`)
- Removes non-alphanumeric characters (preserving `.`, `-`, `_`)
- Prevents directory traversal attacks on file upload endpoints

### `EscapeHTML(input string) string`
- Escapes `<`, `>`, `&`, `"`, `'` characters
- Prevents Cross-Site Scripting (XSS) in any reflected output

### Request Body Size Limiting
- Configurable maximum body size enforced via middleware
- Oversized requests are rejected with `413 Payload Too Large`

---

## Credential Scanning & Redaction

The `internal/security/credentialcheck.go` module automatically detects and redacts secrets in code submitted for analysis:

### Detected Credential Types

| Pattern               | Detection Regex                                    | Redaction Label              |
|-----------------------|---------------------------------------------------|------------------------------|
| AWS Access Keys       | `AKIA[0-9A-Z]{16}`                               | `[REDACTED_AWS_KEY]`         |
| AWS Secret Keys       | `aws_secret_access_key.*`                         | `[REDACTED_AWS_SECRET]`      |
| GitHub PATs           | `ghp_[a-zA-Z0-9]{36}`                            | `[REDACTED_GITHUB_TOKEN]`    |
| Slack Tokens          | `xox[bpors]-[a-zA-Z0-9-]+`                       | `[REDACTED_SLACK_TOKEN]`     |
| Google API Keys       | `AIza[0-9A-Za-z\-_]{35}`                         | `[REDACTED_GOOGLE_KEY]`      |
| Stripe Keys           | `sk_live_[a-zA-Z0-9]{24,}`                       | `[REDACTED_STRIPE_KEY]`      |
| Private Key Blocks    | `-----BEGIN (RSA\|EC\|PRIVATE) KEY-----`          | `[REDACTED_PRIVATE_KEY]`     |
| JWT Tokens            | `eyJ[a-zA-Z0-9_-]*\.eyJ[a-zA-Z0-9_-]*\.[a-zA-Z0-9_-]*` | `[REDACTED_JWT]`     |
| Generic Secrets       | `(password\|secret\|token).*=.*`                  | `[REDACTED_SECRET]`          |

These patterns are applied automatically during:
- Code scanning (`POST /scan`)
- Code review (`POST /review`)
- Knowledge base ingestion (`POST /knowledge`)

---

## Rate Limiting & Abuse Prevention

### Token Bucket Algorithm
The `internal/ratelimit/` package implements a Redis-backed token bucket rate limiter:
- Tokens refill at a configurable rate per minute
- Each request consumes one token
- When the bucket is empty, requests receive `429 Too Many Requests`

### Sliding Window
For more granular control, a sliding window counter tracks requests per time window:
- Prevents burst attacks that game the token bucket
- Configurable window size (default: 1 minute)

### Adaptive Rate Limiting
The `internal/rateguard/` package implements adaptive rate limiting:
- Dynamically adjusts limits based on system load
- Protects against sudden traffic spikes
- Concurrency guards prevent resource exhaustion

### Rate Limit Headers
```
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 45
X-RateLimit-Reset: 1704067260
Retry-After: 30    (only on 429 responses)
```

---

## Security Headers

The `internal/security/security.go` `SecurityHeaders()` function generates the following headers:

| Header                        | Value                                              |
|-------------------------------|----------------------------------------------------|
| `X-Content-Type-Options`      | `nosniff`                                          |
| `X-Frame-Options`             | `DENY`                                             |
| `Content-Security-Policy`     | `default-src 'self'; script-src 'self'`            |
| `Strict-Transport-Security`   | `max-age=31536000; includeSubDomains`              |
| `Permissions-Policy`          | `camera=(), microphone=(), geolocation=()`         |
| `X-XSS-Protection`           | `1; mode=block`                                    |
| `Referrer-Policy`             | `strict-origin-when-cross-origin`                  |

---

## IP Filtering

The `internal/ipfilter/` package provides network-level access control:

- **Allowlisting**: Only specified CIDR subnets can access the API
- **Denylisting**: Specified CIDR subnets are blocked with `403 Forbidden`
- **Trusted Proxies**: Configurable list of trusted proxy IPs for correct client IP extraction from `X-Forwarded-For` and `X-Real-IP` headers
- **Client IP Extraction**: Parses proxy chain headers, skipping trusted proxy hops, falling back to `RemoteAddr`

---

## Account Lockout

The `internal/middleware/lockout.go` middleware implements progressive account lockout:

- Tracks failed login attempts per account
- After configurable threshold (e.g., 5 attempts), the account is temporarily locked
- Lock duration increases with repeated lockouts (progressive backoff)
- Successful authentication resets the failure counter

---

## CORS Policy

The `internal/cors/` package provides configurable Cross-Origin Resource Sharing:

### Development Preset (`DefaultConfig()`)
Permissive configuration for local development:
- All origins allowed
- All standard methods and headers
- Credentials supported

### Production Preset (`ProductionConfig(origins)`)
Strict configuration for production:
- Only specified origins allowed
- Wildcard domain matching (e.g., `*.example.com`)
- Configurable max age for preflight cache
- Specific exposed headers

---

## Idempotency & Replay Protection

The `internal/idempotency/` middleware prevents duplicate request processing:

- Clients send `Idempotency-Key` header with POST/PUT requests
- The key is stored in Redis with a configurable TTL
- Duplicate requests within the TTL return the cached response
- Distributed Redis locks prevent race conditions on concurrent identical requests

---

## Webhook Security

The `internal/webhook/` and `internal/signing/` packages secure outbound webhooks:

- **HMAC-SHA256 Signing**: Every webhook payload is signed with the endpoint's secret
- **Ed25519 Signing**: Optional Ed25519 payload signatures for stronger verification
- **Signature Header**: `X-Webhook-Signature` contains the HMAC signature
- **Delivery Tracking**: Every delivery attempt is logged with status, response code, and retry count
- **Retry Queue**: Failed deliveries are retried with exponential backoff

---

## Compliance Engine

The `internal/compliance/` package provides automated compliance checking against:

| Standard | Coverage                                                    |
|----------|-------------------------------------------------------------|
| SOC2     | Access controls, audit logging, encryption, monitoring      |
| HIPAA    | Data encryption, access controls, audit trails              |
| PCI-DSS  | Cardholder data protection, network security, monitoring    |

The compliance engine scans code and configurations against predefined rule sets and returns a compliance report with pass/fail status per rule.

---

## Audit Logging

The `internal/audit/` package records all security-relevant events:

- **Events Logged**: Authentication attempts, resource access, CRUD operations, admin actions, HITL decisions
- **Fields Captured**: Timestamp, user ID, action, resource type, resource ID, IP address, user agent, request ID
- **Storage**: PostgreSQL `audit_logs` table
- **Retention**: Configurable retention with automated cleanup via `POST /audit/cleanup`
- **Query**: Admin-accessible via `GET /audit/logs`

---

## Dependency Security

### Automated Scanning
- **govulncheck**: Scans Go dependencies for known vulnerabilities (`make security`)
- **gosec**: Go security checker for common coding issues
- **Renovate**: Automated dependency update bot (`renovate.json`) keeps dependencies patched

### Build Security
- **Multi-stage Docker**: Production images use minimal `alpine:3.21` base with only `ca-certificates` and `tzdata`
- **Non-root Execution**: Production containers run as `nobody` user
- **CGO Disabled**: Static compilation (`CGO_ENABLED=0`) eliminates C library attack surface
- **Debug Symbols Stripped**: Production binaries use `-ldflags="-w -s"` to strip debug information

---

## Reporting Vulnerabilities

If you discover a security vulnerability in VigilAgent, please report it responsibly:

1. **Do NOT** open a public GitHub issue
2. Email the security team with details of the vulnerability
3. Include steps to reproduce and potential impact
4. Allow reasonable time for a fix before public disclosure

We appreciate responsible disclosure and will credit reporters in our security advisories.

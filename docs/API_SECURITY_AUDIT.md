# VigilAgent — API Security Audit

> **Status:** Audit only — no code changes. This document identifies vulnerabilities and recommends fixes.

---

## 1. Authentication

### Current Implementation
- JWT tokens with HMAC-SHA256 signing
- API keys with SHA-256 hashing
- bcrypt password hashing
- Account lockout after failed attempts

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| AUTH-001 | HIGH | Default JWT secret `change-me-in-production` in config | Add startup check: fail if default secret in production |
| AUTH-002 | MEDIUM | JWT tokens have no revocation mechanism | Implement JWT blacklist in Redis |
| AUTH-003 | MEDIUM | No token expiry enforcement on refresh | Add `exp` claim validation |
| AUTH-004 | LOW | API key rotation not supported | Add rotation endpoint that creates new key + deactivates old |
| AUTH-005 | LOW | No concurrent session limit | Add session tracking and limit |

### Recommendations

1. **JWT Blacklist**: Store revoked token JTIs in Redis with TTL matching token expiry
2. **Token Rotation**: Implement automatic rotation on every request (header-based)
3. **Session Management**: Track active sessions per user, allow revocation
4. **MFA Support**: Add TOTP/SMS second factor for sensitive operations

---

## 2. Authorization

### Current Implementation
- Role-based: admin vs user
- Organization membership checks
- Project membership checks (via org)
- API key scopes column exists but not enforced

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| AUTHZ-001 | HIGH | API key scopes not enforced | Add scope check middleware |
| AUTHZ-002 | MEDIUM | No project-level RBAC | Add project roles (owner, editor, viewer) |
| AUTHZ-003 | MEDIUM | No resource-level permissions | Add permission matrix per resource type |
| AUTHZ-004 | LOW | Admin role check is simple string comparison | Use role hierarchy with integer levels |

### Recommendations

1. **Scope Enforcement**: Check `scopes` field before allowing operations
2. **RBAC Middleware**: `requireRole("editor")` middleware for sensitive endpoints
3. **Permission Matrix**: Define permissions per role per resource type
4. **Audit Logging**: Log all authorization failures

---

## 3. JWT Security

### Current Implementation
- HMAC-SHA256 signing
- 24-hour expiry (configurable)
- Claims: user_id, email, role, org_id

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| JWT-001 | HIGH | No algorithm confusion protection | Validate `alg` claim is `HS256` only |
| JWT-002 | MEDIUM | No audience (`aud`) claim | Add `aud` claim to prevent token misuse |
| JWT-003 | MEDIUM | No issuer (`iss`) claim | Add `iss` claim |
| JWT-004 | LOW | No `jti` claim for revocation | Add unique token ID |

### Recommendations

1. **Algorithm Validation**: Explicitly validate `alg` is `HS256`, reject `none`
2. **Standard Claims**: Add `iss`, `aud`, `sub`, `jti` claims
3. **Token Binding**: Bind tokens to client fingerprint (optional)

---

## 4. API Key Security

### Current Implementation
- SHA-256 hashing for storage
- `va_*` prefix for identification
- DB-backed lookup
- Last-used tracking

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| KEY-001 | MEDIUM | No key rotation mechanism | Add rotation endpoint |
| KEY-002 | MEDIUM | No key expiration enforcement | Check `expires_at` on every request |
| KEY-003 | LOW | Scopes not validated | Add scope middleware |
| KEY-004 | LOW | No key usage rate limiting | Add per-key rate limits |

### Recommendations

1. **Rotation**: Create new key, mark old as deprecated, delete after grace period
2. **Expiration**: Enforce `expires_at` check in auth middleware
3. **Scope Validation**: Check scopes before allowing operations
4. **Usage Tracking**: Log all API key usage for audit

---

## 5. Secrets Management

### Current Implementation
- JWT secret in config file
- LLM API keys in config/env vars
- Stripe keys in config/env vars
- Database password in config/env vars

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| SEC-001 | HIGH | Secrets in config files (YAML) | Use environment variables or secret managers |
| SEC-002 | MEDIUM | No secret rotation mechanism | Implement secret rotation schedule |
| SEC-003 | LOW | Config file may be committed to git | Add `.env` to `.gitignore` (already done) |

### Recommendations

1. **Environment Variables**: Prefer env vars over config files for secrets
2. **Secret Manager**: Integrate with HashiCorp Vault, AWS Secrets Manager, or similar
3. **Rotation Schedule**: Rotate JWT secret, API keys quarterly
4. **Audit Logging**: Log all secret access

---

## 6. SQL Injection

### Current Implementation
- All queries use parameterized queries (`$1`, `$2`, etc.)
- pgx driver handles parameter escaping

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| SQL-001 | INFO | No SQL injection vulnerabilities found | Maintain parameterized queries |
| SQL-002 | LOW | `StripSQLInjection` in security package is heuristic only | Document as defense-in-depth, not primary defense |

### Assessment
- **Strong**: Parameterized queries used consistently
- **Good**: pgx driver provides automatic escaping
- **Note**: `StripSQLInjection` is display-only sanitization, not security control

---

## 7. XSS (Cross-Site Scripting)

### Current Implementation
- JSON API responses (not HTML)
- `EscapeHTML` function in security package
- Security headers: `X-XSS-Protection: 1; mode=block`

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| XSS-001 | LOW | JSON responses not vulnerable to XSS | Maintain JSON content type |
| XSS-002 | LOW | Error messages may contain user input | Sanitize error messages |
| XSS-003 | INFO | `Content-Security-Policy` set to `default-src 'none'` | Good — prevents inline scripts |

### Assessment
- **Strong**: JSON API, no HTML rendering
- **Good**: CSP header set to restrictive policy
- **Note**: Monitor for future HTML endpoints

---

## 8. CSRF (Cross-Site Request Forgery)

### Current Implementation
- `CSRFProtect` middleware exists in `internal/middleware/security.go`
- Double-submit cookie pattern
- Constant-time token comparison

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| CSRF-001 | HIGH | CSRF middleware not applied to state-changing endpoints | Apply to all POST/PUT/DELETE endpoints |
| CSRF-002 | MEDIUM | API key auth bypasses CSRF (expected) | Document that API key clients don't need CSRF |
| CSRF-003 | LOW | No CSRF token rotation | Rotate tokens on session change |

### Recommendations

1. **Apply CSRF**: Add `CSRFProtect` to all state-changing handlers
2. **Exempt API Keys**: Skip CSRF for API key authentication
3. **Token Rotation**: Rotate CSRF tokens on login/session change

---

## 9. Replay Attacks

### Current Implementation
- JWT tokens have expiry
- API keys have `last_used_at` tracking
- No idempotency keys

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| REPLAY-001 | MEDIUM | No idempotency keys for POST endpoints | Add `Idempotency-Key` header support |
| REPLAY-002 | LOW | JWT tokens can be replayed within expiry | Add `jti` claim and blacklist |
| REPLAY-003 | LOW | No request timestamp validation | Add `iat` claim validation |

### Recommendations

1. **Idempotency Keys**: Store key→response mapping in Redis (24h TTL)
2. **JWT JTI**: Add unique token ID, check blacklist on every request
3. **Timestamp Validation**: Reject tokens with `iat` too far in the past

---

## 10. Rate Limiting

### Current Implementation
- Redis-backed sliding window rate limiting
- Separate limits for auth, API key, and event endpoints
- Rate limit headers on all responses

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| RATE-001 | MEDIUM | In-memory rate limit headers don't work across instances | Use Redis for all rate limiting |
| RATE-002 | MEDIUM | No per-plan rate limiting | Add tier-based limits |
| RATE-003 | LOW | Rate limit bypass possible via IP spoofing | Use `X-Forwarded-For` carefully, trust proxy only |
| RATE-004 | LOW | No rate limit for WebSocket connections | Add connection rate limiting |

### Recommendations

1. **Redis for All**: Move all rate limiting to Redis
2. **Per-Plan Limits**: Differentiate free/pro/enterprise tiers
3. **Proxy Awareness**: Configure trusted proxies for accurate IP detection
4. **WebSocket Limits**: Add connection and message rate limits

---

## 11. Sensitive Data Exposure

### Current Implementation
- Password hash excluded from JSON (`json:"-"`)
- API key hash never returned
- API key plaintext returned only on creation

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| DATA-001 | MEDIUM | Error messages may leak internal details | Sanitize error messages for production |
| DATA-002 | LOW | User email returned in profile | Document as expected behavior |
| DATA-003 | LOW | Organization settings returned as JSONB | Consider sensitivity levels |

### Recommendations

1. **Error Sanitization**: Map internal errors to generic messages in production
2. **Data Classification**: Mark fields as public/internal/sensitive
3. **Response Filtering**: Strip sensitive fields based on requester role

---

## 12. Security Headers

### Current Implementation
```http
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 1; mode=block
Referrer-Policy: strict-origin-when-cross-origin
Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=()
Content-Security-Policy: default-src 'none'; frame-ancestors 'none'; form-action 'none'; base-uri 'self'; object-src 'none'
Strict-Transport-Security: max-age=31536000; includeSubDomains; preload (production only)
```

### Assessment
- **Strong**: All critical headers present
- **Good**: CSP restrictive, HSTS with preload
- **Note**: Consider adding `X-Permitted-Cross-Domain-Policies: none`

---

## 13. CORS

### Current Implementation
- Configurable allowed origins
- Production mode rejects wildcard `*`
- Config validation ensures origins are valid URLs

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| CORS-001 | MEDIUM | Development mode allows `*` origins | Restrict even in development |
| CORS-002 | LOW | No origin validation for WebSocket | Validate origin on WS upgrade |

### Recommendations

1. **Strict Origins**: Always use explicit origins, even in development
2. **WebSocket Origin**: Validate `Origin` header on WebSocket upgrade
3. **Credentials**: Ensure `Access-Control-Allow-Credentials: true` only with specific origins

---

## 14. Input Validation

### Current Implementation
- Manual `json.NewDecoder().Decode()` + string checks
- `SanitizeMiddleware` for SQLi/XSS/path traversal
- `limitBodySize` middleware (2 MiB)

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| VALID-001 | MEDIUM | No structured validation library | Add `go-playground/validator` |
| VALID-002 | MEDIUM | Query parameters not validated | Add query parameter validation |
| VALID-003 | LOW | No file type validation for uploads | Add content-type validation |
| VALID-004 | LOW | No maximum array length validation | Add array size limits |

### Recommendations

1. **Validation Library**: Integrate `go-playground/validator` with struct tags
2. **Query Validation**: Validate all query parameters
3. **File Validation**: Validate content-type and file magic bytes
4. **Array Limits**: Set maximum array sizes for batch operations

---

## 15. Output Sanitization

### Current Implementation
- JSON responses (no HTML rendering)
- `EscapeHTML` function available
- Error messages may contain user input

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| OUTPUT-001 | LOW | Error messages may contain user input | Sanitize error messages |
| OUTPUT-002 | INFO | JSON responses not vulnerable to injection | Maintain JSON content type |

### Recommendations

1. **Error Sanitization**: Strip special characters from error messages
2. **Content-Type Enforcement**: Always set `Content-Type: application/json`

---

## 16. Logging

### Current Implementation
- Structured logging with `slog`
- Request ID propagation
- Log levels: debug, info, warn, error

### Vulnerabilities

| ID | Severity | Finding | Recommendation |
|----|----------|---------|----------------|
| LOG-001 | MEDIUM | No audit trail for security events | Add security event logging |
| LOG-002 | LOW | No request/response body logging | Add optional body logging for debugging |
| LOG-003 | LOW | No log rotation configured | Configure log rotation |

### Recommendations

1. **Audit Logging**: Log all auth events, permission failures, data mutations
2. **Body Logging**: Add optional request/response body logging (configurable)
3. **Log Rotation**: Configure log rotation and retention

---

## 17. OWASP Top 10 Assessment

| OWASP Category | Status | Notes |
|----------------|--------|-------|
| A01: Broken Access Control | ⚠️ Partial | RBAC not fully implemented, scopes not enforced |
| A02: Cryptographic Failures | ✅ Good | bcrypt, SHA-256, AES-GCM used correctly |
| A03: Injection | ✅ Good | Parameterized queries, input sanitization |
| A04: Insecure Design | ⚠️ Partial | No threat modeling, no security requirements |
| A05: Security Misconfiguration | ⚠️ Partial | Default JWT secret, dev CORS |
| A06: Vulnerable Components | ✅ Good | Dependencies up to date |
| A07: Auth Failures | ⚠️ Partial | No MFA, no token revocation |
| A08: Data Integrity Failures | ✅ Good | HMAC signatures on webhooks |
| A09: Logging Failures | ⚠️ Partial | No audit trail |
| A10: SSRF | ✅ Good | SSRF validator on webhooks |

---

## 18. Critical Fixes (Priority Order)

| Priority | ID | Finding | Effort | Impact |
|----------|----|---------|--------|--------|
| P1 | AUTH-001 | Default JWT secret | Low | High |
| P1 | CSRF-001 | CSRF not applied | Medium | High |
| P1 | AUTHZ-001 | API key scopes not enforced | Low | High |
| P1 | RATE-001 | In-memory rate limits | Medium | High |
| P2 | JWT-001 | Algorithm confusion | Low | Medium |
| P2 | AUTH-002 | JWT revocation | Medium | Medium |
| P2 | KEY-001 | Key rotation | Medium | Medium |
| P2 | REPLAY-001 | Idempotency keys | Medium | Medium |
| P2 | LOG-001 | Audit logging | Medium | Medium |
| P3 | AUTHZ-002 | Project RBAC | High | Medium |
| P3 | AUTH-005 | Session management | Medium | Low |
| P3 | VALID-001 | Validation library | Medium | Low |

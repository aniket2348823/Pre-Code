# VigilAgent — Next Action Plan

> Prioritized roadmap for improving the VigilAgent API platform. Based on the comprehensive audit findings.

---

## Executive Summary

The VigilAgent codebase is a solid foundation with clean architecture patterns, comprehensive middleware, and production-ready infrastructure. However, it lacks critical API platform features: pagination, service layer, API documentation, and proper security enforcement. This plan prioritizes improvements by impact and risk.

**Overall Score:** 5.9/10 (C+)
**Target Score:** 8.5/10 (A-) within 6 months

---

## Priority 1: Critical Foundations (Weeks 1-2)

### Effort: Low-Medium | Impact: High | Risk: Low

| Task | Effort | Impact | Risk | Owner |
|------|--------|--------|------|-------|
| Fix default JWT secret in production | 1 day | High | Low | Backend |
| Apply CSRF to all state-changing endpoints | 1 day | High | Low | Backend |
| Enforce API key scopes in middleware | 1 day | High | Low | Backend |
| Add pagination to all list endpoints | 3 days | High | Low | Backend |
| Standardize error response format | 2 days | High | Low | Backend |

### Deliverables
- [ ] Startup validation rejects default JWT secret in production
- [ ] CSRF middleware applied to POST/PUT/DELETE endpoints
- [ ] API key scopes checked before allowing operations
- [ ] Cursor-based pagination on all list endpoints
- [ ] Consistent `{data, meta, errors}` response envelope

### Success Criteria
- All list endpoints return paginated results
- API key scopes enforced on all protected endpoints
- CSRF protection on all state-changing operations
- No default secrets in production

---

## Priority 2: Security Hardening (Weeks 3-4)

### Effort: Medium | Impact: High | Risk: Low-Medium

| Task | Effort | Impact | Risk | Owner |
|------|--------|--------|------|-------|
| Add JWT blacklist (Redis) | 2 days | High | Low | Backend |
| Add idempotency key support | 2 days | High | Low | Backend |
| Add request validation middleware | 3 days | Medium | Low | Backend |
| Add audit logging for security events | 2 days | Medium | Low | Backend |
| Add API key rotation endpoint | 1 day | Medium | Low | Backend |

### Deliverables
- [ ] JWT blacklist in Redis (revoke on logout/password change)
- [ ] Idempotency-Key header support for POST endpoints
- [ ] Structured input validation with go-playground/validator
- [ ] Audit log table for auth events, permission failures
- [ ] API key rotation (create new, deactivate old)

### Success Criteria
- JWT tokens can be revoked
- Duplicate POST requests return cached response
- All input validated with structured error messages
- Security events logged to audit table
- API keys can be rotated without downtime

---

## Priority 3: API Polish (Weeks 5-8)

### Effort: Medium | Impact: Medium | Risk: Low

| Task | Effort | Impact | Risk | Owner |
|------|--------|--------|------|-------|
| Add filtering and sorting middleware | 3 days | Medium | Low | Backend |
| Implement billing endpoints (Stripe) | 5 days | High | Medium | Backend |
| Add API documentation (OpenAPI 3.0) | 3 days | High | Low | Backend |
| Split router.go into domain files | 2 days | Medium | Low | Backend |
| Add per-plan rate limiting | 2 days | Medium | Low | Backend |

### Deliverables
- [ ] Query parameter filtering on list endpoints
- [ ] Sorting support with `?sort=field&order=desc`
- [ ] Stripe checkout, portal, and webhook handlers
- [ ] OpenAPI 3.0 specification with examples
- [ ] Handlers split by domain (auth, projects, tasks, etc.)
- [ ] Rate limit tiers (free/pro/enterprise)

### Success Criteria
- API consumers can filter and sort results
- Billing workflow functional end-to-end
- API documentation available at `/api/v1/docs`
- Rate limits differentiated by plan

---

## Priority 4: Architecture (Weeks 9-12)

### Effort: High | Impact: High | Risk: Medium

| Task | Effort | Impact | Risk | Owner |
|------|--------|--------|------|-------|
| Extract service layer from handlers | 5 days | High | Medium | Backend |
| Add RBAC middleware | 3 days | High | Medium | Backend |
| Add OAuth2/OIDC support | 5 days | High | High | Backend |
| Partition events table | 2 days | Medium | Low | Backend |
| Add materialized views for analytics | 2 days | Medium | Low | Backend |

### Deliverables
- [ ] Service layer for business logic
- [ ] Project-level roles (owner, editor, viewer)
- [ ] OAuth2/OIDC provider integration
- [ ] Events table partitioned by month
- [ ] Materialized views for cost/token analytics

### Success Criteria
- Business logic separated from HTTP handlers
- Granular permissions per project
- Third-party OAuth login working
- Events queries performant at scale

---

## Priority 5: Enterprise Features (Weeks 13-16)

### Effort: High | Impact: Medium | Risk: Medium

| Task | Effort | Impact | Risk | Owner |
|------|--------|--------|------|-------|
| Add MFA support | 3 days | Medium | Medium | Backend |
| Add WebSocket rooms/channels | 3 days | Medium | Medium | Backend |
| Add GraphQL endpoint | 5 days | Medium | High | Backend |
| Add SDK/client libraries | 5 days | High | Low | DX |
| Add load testing suite | 2 days | Medium | Low | QA |

### Deliverables
- [ ] TOTP/SMS second factor authentication
- [ ] WebSocket with room-based pub/sub
- [ ] GraphQL API for complex dashboard queries
- [ ] JavaScript/Python SDK
- [ ] k6 load test suite

### Success Criteria
- MFA available for sensitive accounts
- Real-time updates via WebSocket rooms
- GraphQL queries for dashboard
- SDK available for quick integration
- Performance validated under load

---

## Effort Summary

| Priority | Weeks | Effort (person-days) | Impact |
|----------|-------|---------------------|--------|
| P1: Critical Foundations | 1-2 | 8 | High |
| P2: Security Hardening | 3-4 | 10 | High |
| P3: API Polish | 5-8 | 15 | Medium |
| P4: Architecture | 9-12 | 17 | High |
| P5: Enterprise Features | 13-16 | 18 | Medium |
| **Total** | **16 weeks** | **68 days** | |

---

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| Breaking changes during refactor | Medium | High | Feature flags, gradual rollout |
| Performance regression | Low | Medium | Load testing before/after |
| Security vulnerability during changes | Low | High | Security review for each PR |
| Scope creep | High | Medium | Strict prioritization |
| Team availability | Medium | Medium | Cross-training, documentation |

---

## Dependencies

```
P1 (Foundations)
  └── P2 (Security) — depends on P1 pagination/error format
       └── P3 (Polish) — depends on P2 validation
            └── P4 (Architecture) — depends on P3 service layer extraction
                 └── P5 (Enterprise) — depends on P4 OAuth2, RBAC
```

---

## Success Metrics

| Metric | Current | Target (6 months) |
|--------|---------|-------------------|
| API Quality Score | 5.9/10 | 8.5/10 |
| Test Coverage | ~60% | 80% |
| API Documentation | None | OpenAPI 3.0 |
| Pagination | 0 endpoints | All list endpoints |
| Security Findings | 12 | 0 critical |
| Response Time (p95) | Unknown | < 200ms |

---

## What Should Never Change

| Item | Reason |
|------|--------|
| `/api/v1/health` | Kubernetes liveness probe |
| `/api/v1/ready` | Kubernetes readiness probe |
| JWT token format | All clients depend on it |
| API key prefix `va_` | All integrations depend on it |
| Database schema primary keys | All foreign keys depend on them |
| SSE event types | VS Code extension depends on them |
| Webhook event types | All webhook consumers depend on them |

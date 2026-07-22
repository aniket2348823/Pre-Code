# VigilAgent — API Endpoint Inventory

## Legend

| Field | Values |
|-------|--------|
| Auth | ✅ Required / ❌ Public |
| Rate Limited | ✅ / ❌ |
| Classification | Public / Internal / Admin |
| Streaming | ✅ SSE / ❌ |
| WebSocket | ✅ / ❌ |
| Recommendation | Keep / Modify / Deprecate / Merge |

---

## Health & Infrastructure

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 1 | GET | `/api/v1/health` | Liveness check | ❌ | ❌ | Public | ❌ | ❌ | None | healthHandler | Keep |
| 2 | GET | `/api/v1/ready` | Readiness check (Postgres, Redis, NATS) | ❌ | ❌ | Public | ❌ | ❌ | None | readinessHandler | Keep |
| 3 | GET | `/api/v1/metrics` | Prometheus metrics endpoint | ❌ | ❌ | Public | ❌ | ❌ | None | telemetry | Keep |

---

## Authentication

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 4 | POST | `/api/v1/auth/register` | User registration | ❌ | ✅ auth | Public | ❌ | ❌ | users | registerHandler | Keep |
| 5 | POST | `/api/v1/auth/login` | User login, returns JWT | ❌ | ✅ auth | Public | ❌ | ❌ | users | loginHandler | Keep |
| 6 | POST | `/api/v1/auth/forgot-password` | Request password reset email | ❌ | ✅ auth | Public | ❌ | ❌ | users | forgotPasswordHandler | Keep |
| 7 | POST | `/api/v1/auth/reset-password` | Reset password with token | ❌ | ✅ auth | Public | ❌ | ❌ | users | resetPasswordHandler | Keep |
| 8 | GET | `/api/v1/auth/verify-email` | Verify email with token | ❌ | ❌ | Public | ❌ | ❌ | users | verifyEmailHandler | Keep |
| 9 | POST | `/api/v1/auth/refresh` | Refresh JWT token | ✅ | ✅ apikey | Internal | ❌ | ❌ | users | refreshTokenHandler | Keep |
| 10 | GET | `/api/v1/auth/session-check` | Check DB session variable | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | authSessionMiddleware | Keep |

---

## Users

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 11 | GET | `/api/v1/users/me` | Get current user profile | ✅ | ✅ apikey | Internal | ❌ | ❌ | users | currentUserHandler | Keep |
| 12 | PUT | `/api/v1/users/me` | Update current user profile | ✅ | ✅ apikey | Internal | ❌ | ❌ | users | updateProfileHandler | Keep |

---

## Organizations

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 13 | POST | `/api/v1/organizations` | Create organization | ✅ | ✅ apikey | Internal | ❌ | ❌ | organizations, organization_members | createOrgHandler | Keep |
| 14 | GET | `/api/v1/organizations` | List user's organizations | ✅ | ✅ apikey | Internal | ❌ | ❌ | organizations, organization_members | listOrgsHandler | Keep |
| 15 | GET | `/api/v1/organizations/{orgID}` | Get organization details | ✅ | ✅ apikey | Internal | ❌ | ❌ | organizations, organization_members | getOrgHandler | Keep |
| 16 | PUT | `/api/v1/organizations/{orgID}` | Update organization | ✅ | ✅ apikey | Internal | ❌ | ❌ | organizations | updateOrgHandler | Keep |
| 17 | DELETE | `/api/v1/organizations/{orgID}` | Delete organization | ✅ | ✅ apikey | Internal | ❌ | ❌ | organizations | deleteOrgHandler | Keep |

---

## Projects

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 18 | POST | `/api/v1/projects` | Create project | ✅ | ✅ apikey | Internal | ❌ | ❌ | projects | createProjectHandler | Keep |
| 19 | GET | `/api/v1/projects` | List projects by org | ✅ | ✅ apikey | Internal | ❌ | ❌ | projects | listProjectsHandler | Modify (add pagination) |
| 20 | GET | `/api/v1/projects/{projectID}` | Get project details | ✅ | ✅ apikey | Internal | ❌ | ❌ | projects | getProjectHandler | Keep |
| 21 | PUT | `/api/v1/projects/{projectID}` | Update project | ✅ | ✅ apikey | Internal | ❌ | ❌ | projects | updateProjectHandler | Keep |
| 22 | DELETE | `/api/v1/projects/{projectID}` | Delete project | ✅ | ✅ apikey | Internal | ❌ | ❌ | projects | deleteProjectHandler | Keep |

---

## Agents

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 23 | POST | `/api/v1/projects/{projectID}/agents` | Create agent | ✅ | ✅ apikey | Internal | ❌ | ❌ | agents | createAgentHandler | Keep |
| 24 | GET | `/api/v1/projects/{projectID}/agents` | List agents by project | ✅ | ✅ apikey | Internal | ❌ | ❌ | agents | listAgentsHandler | Modify (add pagination) |
| 25 | GET | `/api/v1/agents/{agentID}` | Get agent details | ✅ | ✅ apikey | Internal | ❌ | ❌ | agents | getAgentHandler | Keep |
| 26 | PUT | `/api/v1/agents/{agentID}` | Update agent | ✅ | ✅ apikey | Internal | ❌ | ❌ | agents | updateAgentHandler | Keep |
| 27 | DELETE | `/api/v1/agents/{agentID}` | Delete agent | ✅ | ✅ apikey | Internal | ❌ | ❌ | agents | deleteAgentHandler | Keep |

---

## Sessions

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 28 | POST | `/api/v1/agents/{agentID}/sessions` | Create session | ✅ | ✅ apikey | Internal | ❌ | ❌ | sessions | createSessionHandler | Keep |
| 29 | GET | `/api/v1/agents/{agentID}/sessions` | List sessions by agent | ✅ | ✅ apikey | Internal | ❌ | ❌ | sessions | listSessionsHandler | Modify (add pagination) |
| 30 | GET | `/api/v1/sessions/{sessionID}` | Get session details | ✅ | ✅ apikey | Internal | ❌ | ❌ | sessions | getSessionHandler | Keep |
| 31 | PUT | `/api/v1/sessions/{sessionID}` | Update session status | ✅ | ✅ apikey | Internal | ❌ | ❌ | sessions | updateSessionHandler | Keep |

---

## Tasks

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 32 | POST | `/api/v1/tasks` | Create task | ✅ | ✅ apikey | Internal | ❌ | ❌ | tasks | createTaskHandler | Keep |
| 33 | GET | `/api/v1/tasks` | List tasks by project | ✅ | ✅ apikey | Internal | ❌ | ❌ | tasks | listTasksHandler | Modify (add pagination) |
| 34 | GET | `/api/v1/tasks/{taskID}` | Get task details | ✅ | ✅ apikey | Internal | ❌ | ❌ | tasks | getTaskHandler | Keep |
| 35 | POST | `/api/v1/tasks/{taskID}/cancel` | Cancel task | ✅ | ✅ apikey | Internal | ❌ | ❌ | tasks | cancelTaskHandler | Keep |
| 36 | GET | `/api/v1/tasks/{taskID}/stream` | Stream task updates (SSE) | ✅ | ✅ apikey | Internal | ✅ | ❌ | tasks | streamTaskHandler | Keep |
| 37 | POST | `/api/v1/tasks/{taskID}/hitl` | Approve HITL checkpoint | ✅ | ✅ apikey | Internal | ❌ | ❌ | tasks | approveHITLHandler | Keep |
| 38 | POST | `/api/v1/tasks/batch` | Batch task operations | ✅ | ✅ apikey | Internal | ❌ | ❌ | tasks | batchTaskHandler | Keep |

---

## Memory

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 39 | POST | `/api/v1/memory/search` | Semantic memory search | ✅ | ✅ apikey | Internal | ❌ | ❌ | memory_episodes, memory_patterns | searchMemoryHandler | Keep |
| 40 | POST | `/api/v1/memory` | Create memory entry | ✅ | ✅ apikey | Internal | ❌ | ❌ | memory_episodes, memory_patterns | createMemoryHandler | Keep |

---

## Scanner & Analysis

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 41 | POST | `/api/v1/scan` | Run static analysis | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | scanHandler | Keep |
| 42 | POST | `/api/v1/review` | Code review | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | reviewHandler | Keep |
| 43 | POST | `/api/v1/requirements` | Requirements resolution | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | requirementsHandler | Keep |
| 44 | POST | `/api/v1/validate` | Input validation | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | validateHandler | Keep |
| 45 | POST | `/api/v1/schema` | Schema validation | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | schemaHandler | Keep |
| 46 | POST | `/api/v1/compliance` | Compliance check | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | complianceHandler | Keep |
| 47 | POST | `/api/v1/validate-full` | Full pipeline validation | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | pipelineHandler | Keep |

---

## Knowledge & Skills Engine

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 48 | POST | `/api/v1/knowledge` | Knowledge graph query | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | knowledgeHandler | Keep |
| 49 | POST | `/api/v1/skills/extract` | Extract skill from finding | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | skillEngineHandler | Keep |
| 50 | POST | `/api/v1/confidence` | Confidence scoring | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | confidenceHandler | Keep |
| 51 | POST | `/api/v1/attack-graph` | Attack graph generation | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | attackGraphHandler | Keep |
| 52 | POST | `/api/v1/audit/trace` | Audit trace | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | auditHandler | Keep |

---

## Middleware Engine

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 53 | POST | `/api/v1/middleware/process` | Process middleware chain | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | middlewareProcessHandler | Keep |
| 54 | GET | `/api/v1/middleware/metrics` | Get middleware metrics | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | middlewareMetricsHandler | Keep |
| 55 | GET | `/api/v1/middleware/patterns` | Get middleware patterns | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | middlewarePatternsHandler | Keep |

---

## Events

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 56 | POST | `/api/v1/sessions/{sessionID}/events` | Create event | ✅ | ✅ events | Internal | ❌ | ❌ | events | createEventsHandler | Keep |
| 57 | POST | `/api/v1/sessions/{sessionID}/events/batch` | Batch create events | ✅ | ✅ events | Internal | ❌ | ❌ | events | batchEventsHandler | Keep |

---

## Analytics

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 58 | GET | `/api/v1/analytics/cost` | Cost analytics by org | ✅ | ✅ apikey | Internal | ❌ | ❌ | events | costAnalyticsHandler | Keep |
| 59 | GET | `/api/v1/analytics/tokens` | Token analytics by org | ✅ | ✅ apikey | Internal | ❌ | ❌ | events | tokenAnalyticsHandler | Keep |
| 60 | GET | `/api/v1/analytics/sessions` | Session analytics by org | ✅ | ✅ apikey | Internal | ❌ | ❌ | events | sessionAnalyticsHandler | Keep |
| 61 | GET | `/api/v1/analytics/cost-intel` | Cost intelligence dashboard | ✅ | ✅ apikey | Internal | ❌ | ❌ | events | costIntelDashboardHandler | Keep |
| 62 | GET | `/api/v1/analytics/cost-intel/forecast` | Cost forecast | ✅ | ✅ apikey | Internal | ❌ | ❌ | events | costIntelForecastHandler | Keep |
| 63 | GET | `/api/v1/analytics/cost-intel/recommendations` | Cost recommendations | ✅ | ✅ apikey | Internal | ❌ | ❌ | events | costIntelRecommendationsHandler | Keep |
| 64 | GET | `/api/v1/analytics/cost-intel/anomalies` | Cost anomalies | ✅ | ✅ apikey | Internal | ❌ | ❌ | events | costIntelAnomaliesHandler | Keep |

---

## Dashboard

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 65 | GET | `/api/v1/dashboard/overview` | Dashboard overview | ✅ | ✅ apikey | Internal | ❌ | ❌ | events | dashboardOverviewHandler | Keep |
| 66 | GET | `/api/v1/dashboard/activity` | Recent activity | ✅ | ✅ apikey | Internal | ❌ | ❌ | events | dashboardActivityHandler | Keep |
| 67 | GET | `/api/v1/dashboard/top-agents` | Top agents by org | ✅ | ✅ apikey | Internal | ❌ | ❌ | events | dashboardTopAgentsHandler | Keep |

---

## Skills Marketplace

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 68 | GET | `/api/v1/skills` | List skills | ✅ | ✅ apikey | Internal | ❌ | ❌ | skills | listSkillsHandler | Keep |
| 69 | GET | `/api/v1/skills/{skillID}` | Get skill details | ✅ | ✅ apikey | Internal | ❌ | ❌ | skills | getSkillHandler | Keep |
| 70 | POST | `/api/v1/skills` | Create skill | ✅ | ✅ apikey | Internal | ❌ | ❌ | skills | createSkillHandler | Keep |
| 71 | PUT | `/api/v1/skills/{skillID}` | Update skill | ✅ | ✅ apikey | Internal | ❌ | ❌ | skills | updateSkillHandler | Keep |
| 72 | DELETE | `/api/v1/skills/{skillID}` | Delete skill | ✅ | ✅ apikey | Internal | ❌ | ❌ | skills | deleteSkillHandler | Keep |
| 73 | POST | `/api/v1/skills/{skillID}/rate` | Rate skill | ✅ | ✅ apikey | Internal | ❌ | ❌ | skill_ratings | rateSkillHandler | Keep |
| 74 | GET | `/api/v1/skills/{skillID}/ratings` | List skill ratings | ✅ | ✅ apikey | Internal | ❌ | ❌ | skill_ratings | listSkillRatingsHandler | Keep |
| 75 | POST | `/api/v1/skills/{skillID}/install` | Install skill | ✅ | ✅ apikey | Internal | ❌ | ❌ | skill_installs | installSkillHandler | Keep |

### Skills RAG Endpoints (Feature Flag: skill_rag)

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 76 | POST | `/api/v1/skills/search` | Hybrid RAG search | ✅ | ✅ apikey | Internal | ❌ | ❌ | skills, skill_embeddings | RAGHandlers | Keep |
| 77 | GET | `/api/v1/skills/trending` | Trending skills | ✅ | ✅ apikey | Internal | ❌ | ❌ | skills | RAGHandlers | Keep |
| 78 | GET | `/api/v1/skills/categories` | Skill categories | ✅ | ✅ apikey | Internal | ❌ | ❌ | skills | RAGHandlers | Keep |
| 79 | GET | `/api/v1/skills/suggest` | Autocomplete suggestions | ✅ | ✅ apikey | Internal | ❌ | ❌ | skills | RAGHandlers | Keep |

---

## Alerts

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 80 | GET | `/api/v1/alerts` | List alerts | ✅ | ✅ apikey | Internal | ❌ | ❌ | alerts | listAlertsHandler | Keep |
| 81 | POST | `/api/v1/alerts` | Create alert | ✅ | ✅ apikey | Internal | ❌ | ❌ | alerts | createAlertHandler | Keep |
| 82 | GET | `/api/v1/alerts/{alertID}` | Get alert | ✅ | ✅ apikey | Internal | ❌ | ❌ | alerts | getAlertHandler | Keep |
| 83 | PUT | `/api/v1/alerts/{alertID}` | Update alert | ✅ | ✅ apikey | Internal | ❌ | ❌ | alerts | updateAlertHandler | Keep |
| 84 | DELETE | `/api/v1/alerts/{alertID}` | Delete alert | ✅ | ✅ apikey | Internal | ❌ | ❌ | alerts | deleteAlertHandler | Keep |

---

## Billing

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 85 | GET | `/api/v1/billing/invoices` | List invoices | ✅ | ✅ apikey | Internal | ❌ | ❌ | invoices | listInvoicesHandler | Modify (implement) |
| 86 | GET | `/api/v1/billing/invoices/{invoiceID}` | Get invoice | ✅ | ✅ apikey | Internal | ❌ | ❌ | invoices | getInvoiceHandler | Modify (implement) |
| 87 | POST | `/api/v1/billing/checkout` | Create Stripe checkout | ✅ | ✅ apikey | Internal | ❌ | ❌ | subscriptions | createCheckoutHandler | Modify (implement) |
| 88 | GET | `/api/v1/billing/subscription` | Get subscription | ✅ | ✅ apikey | Internal | ❌ | ❌ | subscriptions | getSubscriptionHandler | Modify (implement) |
| 89 | POST | `/api/v1/billing/portal` | Create Stripe portal | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | createBillingPortalHandler | Modify (implement) |

---

## API Keys

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 90 | POST | `/api/v1/api-keys` | Create API key | ✅ | ✅ apikey | Internal | ❌ | ❌ | api_keys | createAPIKeyHandler | Keep |
| 91 | GET | `/api/v1/api-keys` | List API keys | ✅ | ✅ apikey | Internal | ❌ | ❌ | api_keys | listAPIKeysHandler | Keep |
| 92 | DELETE | `/api/v1/api-keys/{keyID}` | Delete API key | ✅ | ✅ apikey | Internal | ❌ | ❌ | api_keys | deleteAPIKeyHandler | Keep |

---

## Webhooks

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 93 | POST | `/api/v1/webhooks` | Create webhook endpoint | ✅ | ✅ apikey | Internal | ❌ | ❌ | webhook_endpoints | createWebhookHandler | Keep |
| 94 | GET | `/api/v1/webhooks` | List webhook endpoints | ✅ | ✅ apikey | Internal | ❌ | ❌ | webhook_endpoints | listWebhooksHandler | Keep |
| 95 | GET | `/api/v1/webhooks/stats` | Webhook delivery stats | ✅ | ✅ apikey | Internal | ❌ | ❌ | webhook_endpoints, webhook_deliveries | webhookStatsHandler | Keep |
| 96 | GET | `/api/v1/webhooks/{webhookID}` | Get webhook endpoint | ✅ | ✅ apikey | Internal | ❌ | ❌ | webhook_endpoints | getWebhookHandler | Keep |
| 97 | DELETE | `/api/v1/webhooks/{webhookID}` | Delete webhook endpoint | ✅ | ✅ apikey | Internal | ❌ | ❌ | webhook_endpoints | deleteWebhookHandler | Keep |
| 98 | GET | `/api/v1/webhooks/{webhookID}/deliveries` | Get webhook deliveries | ✅ | ✅ apikey | Internal | ❌ | ❌ | webhook_deliveries | getWebhookDeliveriesHandler | Keep |

---

## Provider Management

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 99 | GET | `/api/v1/providers/health` | LLM provider health stats | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | healthStatsHandler | Keep |
| 100 | POST | `/api/v1/providers/cost-override` | Override model pricing | ✅ | ✅ apikey | Internal | ❌ | ❌ | None | costOverrideHandler | Keep |

---

## Admin

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 101 | GET | `/api/v1/admin/stats` | Platform statistics | ✅+Admin | ✅ apikey | Admin | ❌ | ❌ | users, organizations, projects | adminStatsHandler | Keep |
| 102 | GET | `/api/v1/admin/users` | List all users | ✅+Admin | ✅ apikey | Admin | ❌ | ❌ | users | adminListUsersHandler | Keep |
| 103 | PUT | `/api/v1/admin/users/{userID}/role` | Update user role | ✅+Admin | ✅ apikey | Admin | ❌ | ❌ | users | adminUpdateUserRoleHandler | Keep |
| 104 | DELETE | `/api/v1/admin/users/{userID}` | Delete user | ✅+Admin | ✅ apikey | Admin | ❌ | ❌ | users | adminDeleteUserHandler | Keep |

---

## WebSocket

| # | Method | Route | Purpose | Auth | Rate Limited | Class | Stream | WS | DB Tables | Service | Recommendation |
|---|--------|-------|---------|------|-------------|-------|--------|-----|-----------|---------|----------------|
| 105 | GET | `/api/v1/ws` | WebSocket connection | ✅ | ❌ | Internal | ❌ | ✅ | None | handleWebSocket | Modify (add rooms/channels) |

---

## Summary

| Category | Count | Status |
|----------|-------|--------|
| Health & Infrastructure | 3 | ✅ Complete |
| Authentication | 7 | ✅ Complete |
| Users | 2 | ✅ Complete |
| Organizations | 5 | ✅ Complete |
| Projects | 5 | ✅ Complete (needs pagination) |
| Agents | 5 | ✅ Complete (needs pagination) |
| Sessions | 4 | ✅ Complete (needs pagination) |
| Tasks | 7 | ✅ Complete (needs pagination) |
| Memory | 2 | ✅ Complete |
| Scanner & Analysis | 7 | ✅ Complete |
| Knowledge & Skills Engine | 5 | ✅ Complete |
| Middleware Engine | 3 | ✅ Complete |
| Events | 2 | ✅ Complete |
| Analytics | 7 | ✅ Complete |
| Dashboard | 3 | ✅ Complete |
| Skills Marketplace | 8 + 4 RAG | ✅ Complete |
| Alerts | 5 | ✅ Complete |
| Billing | 5 | ⚠️ Placeholder |
| API Keys | 3 | ✅ Complete (scopes not enforced) |
| Webhooks | 6 | ✅ Complete |
| Provider Management | 2 | ✅ Complete |
| Admin | 4 | ✅ Complete |
| WebSocket | 1 | ⚠️ Underutilized |
| **Total** | **105** | |

### Recommendations Summary

| Action | Count | Endpoints |
|--------|-------|-----------|
| **Keep** | 92 | Most endpoints |
| **Modify (add pagination)** | 7 | List endpoints for projects, agents, sessions, tasks |
| **Modify (implement)** | 5 | All billing endpoints |
| **Modify (add rooms/channels)** | 1 | WebSocket endpoint |
| **Deprecate** | 0 | None |
| **Merge** | 0 | None currently identified |

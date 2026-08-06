# VRP One-Click Deposit — System Design Document

**Domain**: Pay-by-Bank / Open Banking  
**Inspired by**: Paramount Commerce (Merchant Identity Engineering)  
**Purpose**: Interview preparation demo — production-grade patterns in Go  
**Language**: Go 1.23+  
**Date**: 2026-08-06

---

## Table of Contents

1. [Business Context](#1-business-context)
2. [Requirements](#2-requirements)
3. [High-Level Architecture](#3-high-level-architecture)
4. [Service Breakdown](#4-service-breakdown)
5. [Low-Level Design](#5-low-level-design)
6. [Data Models](#6-data-models)
7. [API Contracts](#7-api-contracts)
8. [Infrastructure Stack](#8-infrastructure-stack)
9. [Go Library Choices](#9-go-library-choices)
10. [Key Design Decisions](#10-key-design-decisions)
11. [Implementation Task List](#11-implementation-task-list)

---

## 1. Business Context

A **merchant** (e.g. Bet365, an iGaming operator) integrates with the platform to accept bank payments from their customers. A **consumer** visits the merchant site, deposits funds via Open Banking ("Pay by Bank"). On first deposit, the consumer authenticates with their bank. On subsequent deposits, the system uses a pre-authorised **VRP consent** to pull funds instantly — no redirect, no Face ID, no friction.

### Core User Journeys

**Journey A — First-time deposit (consent creation):**
```
Consumer clicks "Add Funds"
  → Redirected to bank (Open Banking auth)
  → Consumer approves consent (max £200/tx, £1000/month)
  → Consent stored, first payment processed
  → Merchant notified via webhook
```

**Journey B — Repeat deposit (one-click):**
```
Consumer clicks "Add Funds"
  → System validates active consent + limits
  → Risk score checked (< 200ms)
  → Payment initiated against bank API
  → Ledger updated (double-entry)
  → Merchant notified via webhook
```

**Journey C — Consent revocation:**
```
Consumer revokes consent via bank app or merchant portal
  → Consent marked REVOKED
  → Pending payments cancelled
  → Merchant notified
```

---

## 2. Requirements

### Functional
- Merchant onboarding (registration, API key issuance)
- VRP consent lifecycle: PENDING → ACTIVE → REVOKED / EXPIRED
- Payment lifecycle: INITIATED → RISK_CHECK → AUTHORISING → SETTLED / FAILED
- Idempotent payment initiation (duplicate button press = one charge)
- Per-consent limits: max per transaction, max per rolling 30-day window
- Real-time risk scoring before fund authorisation
- Double-entry ledger: every payment = debit consumer, credit merchant escrow
- Reliable webhook delivery to merchant with retry + HMAC signature
- Consent revocation — immediate, propagates to in-flight payments

### Non-Functional
- **Latency**: P99 < 500ms for payment initiation (risk check included)
- **Throughput**: 1,000 TPS sustained, 5,000 TPS burst
- **Availability**: 99.95% uptime (< 4.4h downtime/year)
- **Idempotency**: guaranteed exactly-once payment effect
- **Auditability**: every state transition persisted and replayable
- **Security**: mTLS between services, HMAC on webhooks, secrets in Vault

---

## 3. High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         EXTERNAL                                │
│                                                                 │
│   Merchant App          Consumer Browser        Bank API        │
│   (REST client)         (OAuth2 redirect)       (Open Banking)  │
└────────┬─────────────────────┬──────────────────────┬──────────┘
         │ HTTPS               │ HTTPS                │ HTTPS
         ▼                     ▼                      │
┌────────────────────────────────────────────────┐   │
│              API Gateway (:8080)               │   │
│         Chi router · JWT auth · Rate limit     │   │
└─┬──────────────┬──────────────┬───────────────┘   │
  │ gRPC         │ gRPC         │ gRPC               │
  ▼              ▼              ▼                    │
┌──────────┐ ┌──────────┐ ┌──────────────────┐     │
│ Merchant │ │ Consent  │ │ Payment          │     │
│ Service  │ │ Service  │ │ Orchestrator     │     │
│ :50051   │ │ :50052   │ │ :50053           │     │
└──────────┘ └──────────┘ └──┬───────┬───────┘     │
                              │ gRPC  │ gRPC         │
                              ▼       ▼              │
                    ┌──────────┐ ┌──────────┐       │
                    │ Risk     │ │ Ledger   │       │
                    │ Service  │ │ Service  │       │
                    │ :50054   │ │ :50055   │       │
                    └──────────┘ └──────────┘       │
                                      │             │
                              ┌───────┘             │
                              ▼                     ▼
                    ┌──────────────────┐  ┌──────────────────┐
                    │  Bank Adapter    │  │  Bank Adapter    │
                    │  :50056          │◀─│  (mock server)   │
                    └──────────────────┘  └──────────────────┘
                              │
                              │ Kafka (payment.events)
                              ▼
                    ┌──────────────────┐
                    │  Notification    │
                    │  Service         │
                    │  (webhook)       │
                    └──────────────────┘

┌───────────────────────────────────────────────────────────────┐
│                      INFRASTRUCTURE                           │
│                                                               │
│  PostgreSQL (per-service schema)   Redis (idempotency/cache)  │
│  Kafka (events/webhooks)           Prometheus + Grafana       │
│  Jaeger (tracing)                  Vault (secrets)            │
└───────────────────────────────────────────────────────────────┘
```

### Communication Patterns

| Caller → Callee | Protocol | Why |
|-----------------|----------|-----|
| External → Gateway | HTTPS/REST | Standard external API |
| Gateway → Services | gRPC | Strong typing, low latency, streaming |
| Payment Orchestrator → Risk | gRPC (sync) | Must complete before authorising |
| Payment Orchestrator → Ledger | gRPC (sync) | Double-entry must succeed atomically |
| Payment Orchestrator → Bank | gRPC → HTTP | Adapter pattern over Open Banking API |
| Payment → Notification | Kafka (async) | Fire-and-forget, retry independent |

### Saga: Payment Initiation (Orchestration Pattern)

The Payment Orchestrator owns the saga. No choreography — single brain, easier to debug.

```
PaymentOrchestrator
        │
        ├─1─▶ ConsentService.ValidateAndReserveLimit()
        │         └─ FAIL → return CONSENT_LIMIT_EXCEEDED
        │
        ├─2─▶ RiskService.Score()
        │         └─ HIGH_RISK → compensate: ConsentService.ReleaseReservation()
        │                      → return RISK_DECLINED
        │
        ├─3─▶ BankAdapter.InitiatePayment()
        │         └─ FAIL → compensate: ConsentService.ReleaseReservation()
        │                 → return BANK_REJECTED
        │
        ├─4─▶ LedgerService.PostDoubleEntry()
        │         └─ FAIL → compensate: BankAdapter.Reverse() + ConsentService.ReleaseReservation()
        │
        └─5─▶ Kafka.Publish(payment.settled)
                  └─ NotificationService picks up → delivers webhook to merchant
```

**Compensation is always attempted.** If compensation also fails, the payment lands in a `MANUAL_REVIEW` state and triggers a dead-letter alert.

---

## 4. Service Breakdown

### 4.1 API Gateway
- **Responsibility**: Auth, rate limiting, request routing, TLS termination
- **Stack**: `go-chi/chi/v5`, `golang-jwt/jwt/v5`
- **Rate limiting**: Redis token bucket per merchant API key
- **No business logic** — pure routing and auth

### 4.2 Merchant Service
- **Responsibility**: Merchant registration, API key lifecycle, KYB status
- **Entities**: `Merchant`, `ApiKey`
- **State machine**: `PENDING_KYB → KYB_APPROVED → ACTIVE → SUSPENDED`
- **Database**: PostgreSQL (own schema: `merchant.*`)

### 4.3 Consent Service
- **Responsibility**: VRP consent lifecycle, limit enforcement, reservation
- **Entities**: `Consent`, `ConsentUsage`
- **State machine**: `PENDING → ACTIVE → REVOKED | EXPIRED`
- **Limit enforcement**: optimistic locking on rolling window usage
- **Critical path**: must be fast — Redis cache for hot reads
- **Database**: PostgreSQL + Redis

### 4.4 Payment Orchestrator
- **Responsibility**: Saga coordinator, idempotency, payment state machine
- **Entities**: `Payment`, `PaymentEvent` (append-only audit log)
- **State machine**: `INITIATED → CONSENT_RESERVED → RISK_PASSED → AUTHORISING → SETTLED | FAILED | MANUAL_REVIEW`
- **Idempotency**: Redis `SET NX EX` on `idempotency_key` before any mutation
- **Outbox**: Transactional outbox pattern for Kafka publish reliability
- **Database**: PostgreSQL

### 4.5 Risk Service
- **Responsibility**: Real-time fraud scoring before fund movement
- **Inputs**: merchant_id, consumer_id, amount, consent history, velocity
- **Output**: `ALLOW | REVIEW | DECLINE` + score (0–100)
- **Latency target**: P99 < 50ms (must not dominate the saga)
- **Implementation**: rule engine + Redis velocity counters (no ML for demo)
- **Rules**:
  - Amount > consent max_per_tx → DECLINE
  - > 5 transactions in 60s from same consumer → REVIEW
  - New consumer + amount > £500 → REVIEW
  - Known bad actor list (Redis SET) → DECLINE

### 4.6 Ledger Service
- **Responsibility**: Double-entry bookkeeping
- **Entities**: `Account`, `JournalEntry`, `JournalLine`
- **Accounts**: `CONSUMER_ESCROW`, `MERCHANT_ESCROW`, `PLATFORM_FEE`
- **Invariant**: sum of all debits = sum of all credits (enforced in DB)
- **No updates, only inserts** (append-only journal)
- **Database**: PostgreSQL (own schema: `ledger.*`)

### 4.7 Bank Adapter
- **Responsibility**: Abstract over Open Banking APIs (mock for demo)
- **Pattern**: Adapter/Anti-Corruption Layer
- **Interface**: `InitiatePayment`, `GetStatus`, `ReversePayment`
- **Resilience**: Circuit breaker (`sony/gobreaker`), retry with backoff
- **Mock**: Simple HTTP server simulating bank responses

### 4.8 Notification Service
- **Responsibility**: Reliable webhook delivery to merchants
- **Input**: Kafka topic `payment.events`
- **Delivery**: HTTPS POST to merchant webhook URL
- **Signature**: HMAC-SHA256 (`X-PC-Signature` header)
- **Retry**: exponential backoff (1s, 2s, 4s, 8s, 30s, 5min)
- **Dead letter**: After 6 attempts → `webhook.dlq` Kafka topic + alert

---

## 5. Low-Level Design

### 5.1 Idempotency (Payment Orchestrator)

```
Client sends: POST /payments  {idempotency_key: "cli-uuid-123", ...}

1. Redis SET NX EX 86400 "idempotency:cli-uuid-123" "PROCESSING"
   → If key exists → fetch payment from DB by idempotency_key → return cached result
   → If SET succeeds → proceed with saga

2. After saga completes:
   Redis SET "idempotency:cli-uuid-123" "payment-id:pay_abc123" EX 86400
```

Race condition: Two requests with same key arrive simultaneously.
- Both attempt `SET NX` — only one wins.
- Loser polls Redis for up to 5s, then returns 202 Accepted with a `Retry-After` header.

### 5.2 VRP Limit Enforcement (Consent Service)

```sql
-- Rolling 30-day window check (PostgreSQL)
SELECT COALESCE(SUM(amount_pence), 0)
FROM payment
WHERE consent_id = $1
  AND status = 'SETTLED'
  AND settled_at > NOW() - INTERVAL '30 days'
FOR UPDATE;  -- pessimistic lock during reservation
```

**Reservation pattern** (prevent concurrent overspend):
1. `ReserveLimit(consent_id, amount)` — write `consent_reservation` row (expires in 10 min)
2. Saga proceeds
3. On `SETTLED` → convert reservation to confirmed usage
4. On `FAILED` → delete reservation (compensating action)

### 5.3 Outbox Pattern (Payment → Kafka)

Prevents: "payment saved to DB but Kafka publish failed → merchant never notified."

```sql
-- Same DB transaction as payment update:
INSERT INTO outbox (id, topic, key, payload, created_at)
VALUES (gen_random_uuid(), 'payment.events', $payment_id, $payload, NOW());

UPDATE payment SET status = 'SETTLED' WHERE id = $payment_id;
-- COMMIT
```

A separate outbox relay goroutine polls `outbox` table, publishes to Kafka, deletes row.
**At-least-once delivery guaranteed.** Notification Service must be idempotent.

### 5.4 Circuit Breaker (Bank Adapter)

```go
// sony/gobreaker configuration
cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
    Name:        "bank-api",
    MaxRequests: 5,        // half-open: allow 5 requests
    Interval:    60 * time.Second,
    Timeout:     10 * time.Second,
    ReadyToTrip: func(counts gobreaker.Counts) bool {
        return counts.ConsecutiveFailures > 10
    },
})
```

States: `CLOSED` → `OPEN` (after 10 consecutive failures) → `HALF_OPEN` (after 10s) → `CLOSED`

When `OPEN`: fail fast, return `BANK_UNAVAILABLE`, saga compensates immediately.

### 5.5 Webhook Delivery (Notification Service)

```
Kafka consumer (payment.events)
        │
        ▼
  Fetch merchant webhook URL from Merchant Service
        │
        ▼
  Build payload + sign with HMAC-SHA256
  X-PC-Signature: sha256=<hex>
  X-PC-Timestamp: <unix>
        │
        ▼
  HTTP POST with 5s timeout
        │
     ┌──┴────────────────┐
  2xx OK              Non-2xx / timeout
     │                    │
  Commit Kafka offset  Retry with exponential backoff
                          │ (max 6 attempts)
                       Dead-letter → webhook.dlq
```

**Merchant verifies signature:**
```
expected = HMAC-SHA256(secret, timestamp + "." + body)
```

---

## 6. Data Models

### Merchant Service

```sql
CREATE TABLE merchant (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    webhook_url TEXT,
    kyb_status  TEXT NOT NULL DEFAULT 'PENDING_KYB',
    status      TEXT NOT NULL DEFAULT 'PENDING_KYB',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE api_key (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchant(id),
    key_hash    TEXT NOT NULL UNIQUE,  -- bcrypt hash, never store plaintext
    status      TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ
);
```

### Consent Service

```sql
CREATE TABLE consent (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id         UUID NOT NULL,
    consumer_id         TEXT NOT NULL,             -- external bank user ref
    bank_consent_ref    TEXT NOT NULL UNIQUE,      -- Open Banking consent ID
    status              TEXT NOT NULL DEFAULT 'PENDING',
    max_amount_pence    BIGINT NOT NULL,           -- per-transaction limit
    max_monthly_pence   BIGINT NOT NULL,           -- rolling 30-day limit
    currency            CHAR(3) NOT NULL DEFAULT 'GBP',
    valid_from          TIMESTAMPTZ NOT NULL,
    valid_until         TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE consent_reservation (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    consent_id  UUID NOT NULL REFERENCES consent(id),
    payment_id  UUID NOT NULL,
    amount_pence BIGINT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_consent_consumer ON consent(consumer_id, merchant_id);
```

### Payment Orchestrator

```sql
CREATE TABLE payment (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key  TEXT NOT NULL UNIQUE,
    merchant_id      UUID NOT NULL,
    consent_id       UUID NOT NULL,
    consumer_id      TEXT NOT NULL,
    amount_pence     BIGINT NOT NULL,
    currency         CHAR(3) NOT NULL DEFAULT 'GBP',
    status           TEXT NOT NULL DEFAULT 'INITIATED',
    bank_payment_ref TEXT,               -- returned by bank on authorisation
    risk_score       INT,
    risk_decision    TEXT,
    failure_reason   TEXT,
    initiated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at       TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Append-only audit trail — never update, only insert
CREATE TABLE payment_event (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id  UUID NOT NULL REFERENCES payment(id),
    from_status TEXT NOT NULL,
    to_status   TEXT NOT NULL,
    reason      TEXT,
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Transactional outbox
CREATE TABLE outbox (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    topic       TEXT NOT NULL,
    key         TEXT NOT NULL,
    payload     JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### Ledger Service

```sql
CREATE TABLE account (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type        TEXT NOT NULL,   -- CONSUMER_ESCROW | MERCHANT_ESCROW | PLATFORM_FEE
    owner_ref   TEXT NOT NULL,   -- merchant_id or consumer_id
    currency    CHAR(3) NOT NULL DEFAULT 'GBP',
    UNIQUE (type, owner_ref, currency)
);

CREATE TABLE journal_entry (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id  UUID NOT NULL UNIQUE,
    description TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE journal_line (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    journal_entry_id UUID NOT NULL REFERENCES journal_entry(id),
    account_id       UUID NOT NULL REFERENCES account(id),
    direction        CHAR(2) NOT NULL CHECK (direction IN ('DR', 'CR')),
    amount_pence     BIGINT NOT NULL CHECK (amount_pence > 0),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Enforce double-entry balance: debits = credits per journal entry
CREATE OR REPLACE FUNCTION check_journal_balance() RETURNS TRIGGER AS $$
BEGIN
    IF (
        SELECT SUM(CASE WHEN direction = 'DR' THEN amount_pence ELSE -amount_pence END)
        FROM journal_line WHERE journal_entry_id = NEW.journal_entry_id
    ) != 0 THEN
        RAISE EXCEPTION 'Journal entry % is not balanced', NEW.journal_entry_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
```

---

## 7. API Contracts

### 7.1 REST (API Gateway)

```
POST   /v1/merchants                    Register merchant
GET    /v1/merchants/:id                Get merchant

POST   /v1/consents                     Create VRP consent
GET    /v1/consents/:id                 Get consent
DELETE /v1/consents/:id                 Revoke consent

POST   /v1/payments                     Initiate payment (idempotency_key required)
GET    /v1/payments/:id                 Get payment status
POST   /v1/payments/:id/retry           Manual retry (FAILED → re-attempt)
```

**Payment initiation request:**
```json
{
  "idempotency_key": "mer_abc-txn-20260806-001",
  "consent_id": "con_xyz789",
  "amount_pence": 5000,
  "currency": "GBP",
  "description": "Deposit for game session"
}
```

**Payment response:**
```json
{
  "id": "pay_abc123",
  "status": "SETTLED",
  "amount_pence": 5000,
  "currency": "GBP",
  "bank_payment_ref": "FPS-20260806-XYZ",
  "settled_at": "2026-08-06T10:00:01Z"
}
```

### 7.2 gRPC (Internal)

```protobuf
// consent.proto
service ConsentService {
  rpc GetConsent(GetConsentRequest) returns (Consent);
  rpc ValidateAndReserve(ReserveRequest) returns (ReserveResponse);
  rpc ConfirmReservation(ConfirmRequest) returns (google.protobuf.Empty);
  rpc ReleaseReservation(ReleaseRequest) returns (google.protobuf.Empty);
  rpc RevokeConsent(RevokeRequest) returns (google.protobuf.Empty);
}

// risk.proto
service RiskService {
  rpc Score(ScoreRequest) returns (ScoreResponse);
}

message ScoreRequest {
  string merchant_id = 1;
  string consumer_id = 2;
  string consent_id  = 3;
  int64  amount_pence = 4;
}

message ScoreResponse {
  int32  score    = 1;   // 0-100
  string decision = 2;   // ALLOW | REVIEW | DECLINE
  string reason   = 3;
}

// ledger.proto
service LedgerService {
  rpc PostDoubleEntry(PostEntryRequest) returns (JournalEntry);
  rpc ReverseEntry(ReverseRequest) returns (google.protobuf.Empty);
}

// bank_adapter.proto
service BankAdapter {
  rpc InitiatePayment(InitiateRequest) returns (InitiateResponse);
  rpc GetPaymentStatus(StatusRequest) returns (StatusResponse);
  rpc ReversePayment(ReverseRequest) returns (google.protobuf.Empty);
}
```

### 7.3 Kafka Topics

| Topic | Key | Producer | Consumer | Payload |
|-------|-----|----------|----------|---------|
| `payment.events` | `payment_id` | Payment Orchestrator (outbox) | Notification Service | `PaymentEventPayload` |
| `consent.events` | `consent_id` | Consent Service | Payment Orchestrator | `ConsentEventPayload` |
| `webhook.dlq` | `merchant_id` | Notification Service | Ops alerting | `WebhookFailurePayload` |

---

## 8. Infrastructure Stack

```yaml
# docker-compose.yml services (demo environment)

postgresql:
  image: postgres:16-alpine
  # one instance, multiple schemas per service
  # prod: separate DB per service

redis:
  image: redis:7-alpine
  # Used for:
  # - Idempotency keys (TTL 24h)
  # - Rate limiting (token bucket per API key)
  # - Consent hot cache (TTL 5min)
  # - Risk velocity counters (TTL 60s)

kafka:
  image: confluentinc/cp-kafka:7.6.0
  # Single broker for demo, 3-node cluster for prod
  # Topics: payment.events, consent.events, webhook.dlq

prometheus:
  image: prom/prometheus:v2.50.0
  # Scrapes /metrics from all services

grafana:
  image: grafana/grafana:10.3.0
  # Dashboards: payment funnel, risk score distribution,
  #             webhook delivery rate, saga step latencies

jaeger:
  image: jaegertracing/all-in-one:1.55
  # Distributed tracing (OpenTelemetry → Jaeger)
  # Critical: trace entire saga across 5 service hops

vault:
  image: hashicorp/vault:1.16
  # Demo: dev mode
  # Stores: DB passwords, API keys, HMAC secrets, bank certs
```

**Production additions (not in demo):**
- Separate PostgreSQL per service (connection pooling via PgBouncer)
- Kafka 3-node cluster with replication factor 3, min ISR 2
- Redis Cluster (3 primary + 3 replica)
- Kubernetes (CKAD-level: HPA, readiness probes, PodDisruptionBudget)
- mTLS between all services (cert-manager + Istio or manual)

---

## 9. Go Library Choices

### Core

| Purpose | Library | Why |
|---------|---------|-----|
| HTTP router | `go-chi/chi/v5` | Lightweight, idiomatic, middleware support |
| gRPC | `google.golang.org/grpc` | Standard, battle-tested |
| Protobuf | `google.golang.org/protobuf` | Code gen from .proto |
| PostgreSQL | `jackc/pgx/v5` | Fastest Go PG driver, native protocol |
| DB migrations | `golang-migrate/migrate/v4` | SQL-file based, CI-friendly |
| Redis | `redis/go-redis/v9` | Official client, cluster support |
| Kafka | `segmentio/kafka-go` | Pure Go, no librdkafka dependency |
| Config | `spf13/viper` | Env + file + flags, 12-factor |
| CLI / flags | `spf13/cobra` | Standard for Go CLIs and services |
| DI | `uber-go/fx` | Lifecycle hooks, module wiring |

### Observability

| Purpose | Library |
|---------|---------|
| Structured logging | `log/slog` (stdlib, Go 1.21+) |
| Slog handlers | `samber/slog-multi` + `samber/slog-zap` |
| Metrics | `prometheus/client_golang` |
| Tracing | `go.opentelemetry.io/otel` + `go.opentelemetry.io/otel/exporters/jaeger` |
| gRPC metrics | `grpc-ecosystem/go-grpc-prometheus` |
| gRPC tracing | `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc` |

### Resilience

| Purpose | Library |
|---------|---------|
| Circuit breaker | `sony/gobreaker` |
| Retry | `avast/retry-go/v4` |
| Rate limiting | custom Redis token bucket (< 50 lines) |

### Quality

| Purpose | Library |
|---------|---------|
| Error handling | `samber/oops` (structured errors with stack) |
| Testing | `stretchr/testify` |
| Integration tests | `testcontainers/testcontainers-go` |
| Functional helpers | `samber/lo` |
| Linting | `golangci-lint` (staticcheck, errcheck, revive, govet) |

### JWT / Security

| Purpose | Library |
|---------|---------|
| JWT | `golang-jwt/jwt/v5` |
| HMAC webhook | `crypto/hmac` + `crypto/sha256` (stdlib) |
| Password/key hash | `golang.org/x/crypto/bcrypt` |

---

## 10. Key Design Decisions

### Decision 1: Orchestration Saga vs. Choreography

**Chosen**: Orchestration (Payment Orchestrator owns the flow)

**Why**: With 5 saga steps and complex compensations, choreography via events creates a "distributed spaghetti" — hard to debug and reason about. Orchestration gives a single place to read the entire saga flow. Debugging: one service's logs tell the full story.

**Trade-off**: Payment Orchestrator is a coupling point. Mitigated by thin gRPC interfaces.

### Decision 2: Optimistic vs. Pessimistic Locking (Consent Limits)

**Chosen**: Pessimistic (`SELECT ... FOR UPDATE`) for reservation, then optimistic confirm.

**Why**: Limit enforcement is a hard invariant — overspend is a regulatory and financial risk. The lock window is narrow (< 100ms for the saga's consent step). Optimistic locking would require retries that complicate the saga.

### Decision 3: Outbox vs. Kafka Transactions

**Chosen**: Outbox pattern (application-level)

**Why**: Kafka transactions (exactly-once with `transactional.id`) require Kafka 0.11+ and add broker-side complexity. Outbox is simpler, works with any Kafka version, and keeps the "publish" concern inside the service boundary. The relay goroutine is < 50 lines.

### Decision 4: Redis for Idempotency Keys

**Chosen**: Redis `SET NX EX` (not database unique constraint)

**Why**: Idempotency check must be the first thing that happens — before any DB write. Redis check is < 1ms. A DB unique constraint would still require a round-trip and doesn't handle the "in-progress" state (where a second identical request should wait, not fail).

### Decision 5: One PostgreSQL instance vs. One per Service

**Chosen for demo**: One instance, separate schemas.
**Chosen for prod**: One instance per service.

**Why for demo**: Reduces operational complexity — single connection string, easy to inspect in pgAdmin. Service boundaries still enforced via separate connection pools per service.

---

## 11. Implementation Task List

Work in this order. Each task is independently shippable.

---

### Phase 1 — Foundation

- [ ] **T01**: Scaffold monorepo layout
  ```
  vrp-demo/
  ├── cmd/
  │   ├── gateway/
  │   ├── merchant-svc/
  │   ├── consent-svc/
  │   ├── payment-svc/
  │   ├── risk-svc/
  │   ├── ledger-svc/
  │   ├── bank-adapter/
  │   └── notification-svc/
  ├── internal/
  │   ├── shared/          (domain types, money, errors)
  │   └── testutil/        (testcontainers helpers)
  ├── proto/
  │   ├── consent/
  │   ├── payment/
  │   ├── risk/
  │   ├── ledger/
  │   └── bank/
  ├── migrations/
  │   ├── merchant/
  │   ├── consent/
  │   ├── payment/
  │   └── ledger/
  ├── docker-compose.yml
  ├── Makefile
  └── go.work              (Go workspace — one module per service)
  ```

- [ ] **T02**: `docker-compose.yml` with PostgreSQL, Redis, Kafka, Jaeger, Prometheus, Grafana

- [ ] **T03**: `Makefile` targets: `make proto`, `make migrate`, `make run-all`, `make test`, `make lint`

- [ ] **T04**: Shared `internal/shared/` package
  - `money.Money` type (amount in pence + currency — never use float64)
  - `errors.go` — domain error codes (CONSENT_EXPIRED, LIMIT_EXCEEDED, etc.)
  - `idempotency.go` — Redis idempotency key helper
  - `pagination.go` — cursor-based pagination helpers

- [ ] **T05**: Generate all `.proto` files and run `protoc` code gen
  - Write `proto/consent/consent.proto`, `proto/risk/risk.proto`, etc.
  - Add `make proto` target using `protoc` + `protoc-gen-go` + `protoc-gen-go-grpc`

---

### Phase 2 — Merchant Service

- [ ] **T06**: DB migrations (`migrations/merchant/`)
  - `001_create_merchant.sql`
  - `002_create_api_key.sql`

- [ ] **T07**: Merchant domain logic
  - `Merchant` struct, KYB state machine
  - `ApiKey` generation (`crypto/rand`, store bcrypt hash)

- [ ] **T08**: Merchant gRPC server (`proto/merchant/merchant.proto`)
  - `RegisterMerchant`, `GetMerchant`, `GetMerchantByApiKey`

- [ ] **T09**: Unit tests with testify + integration test with testcontainers (real PostgreSQL)

---

### Phase 3 — Consent Service

- [ ] **T10**: DB migrations (`migrations/consent/`)
  - `001_create_consent.sql`
  - `002_create_consent_reservation.sql`

- [ ] **T11**: Consent state machine
  - States: `PENDING → ACTIVE → REVOKED | EXPIRED`
  - Guard: only ACTIVE consents can be reserved

- [ ] **T12**: Limit enforcement
  - `ValidateAndReserve`: rolling 30-day window query + `SELECT FOR UPDATE`
  - `ConfirmReservation`: reservation → confirmed usage
  - `ReleaseReservation`: compensating action (delete reservation row)

- [ ] **T13**: Redis consent cache
  - Cache hot consent reads (TTL 5 min)
  - Invalidate on status change

- [ ] **T14**: gRPC server + unit + integration tests

---

### Phase 4 — Risk Service

- [ ] **T15**: Rule engine (pure Go, no external dependency)
  - Rule interface: `type Rule interface { Evaluate(ctx, req) (Decision, error) }`
  - Implement: `MaxAmountRule`, `VelocityRule`, `NewConsumerRule`, `BlocklistRule`

- [ ] **T16**: Redis velocity counters
  - `INCR consumer:{id}:tx_count EX 60` — transactions per minute
  - `INCRBY consumer:{id}:monthly_pence EX 2592000` — monthly spend

- [ ] **T17**: gRPC server + unit tests (table-driven, test each rule in isolation)

---

### Phase 5 — Ledger Service

- [ ] **T18**: DB migrations (`migrations/ledger/`)
  - `001_create_account.sql`
  - `002_create_journal.sql`
  - `003_create_balance_check_trigger.sql`

- [ ] **T19**: Double-entry posting
  - `PostDoubleEntry(payment_id, amount)`:
    - DR: Consumer Escrow account
    - CR: Merchant Escrow account
  - Balance invariant enforced by DB trigger

- [ ] **T20**: `ReverseEntry` (compensating action for bank failure)

- [ ] **T21**: gRPC server + integration tests (verify trigger fires on unbalanced entry)

---

### Phase 6 — Bank Adapter (Mock)

- [ ] **T22**: Mock bank HTTP server
  - `POST /bank/payments` → returns `FPS-ref`, 200ms simulated latency
  - `GET /bank/payments/:ref` → returns status
  - `POST /bank/payments/:ref/reverse` → reversal
  - Configurable failure rate (env var `MOCK_BANK_FAIL_RATE=0.1` = 10% failure)

- [ ] **T23**: Bank Adapter gRPC service
  - Circuit breaker (`sony/gobreaker`) wrapping HTTP calls
  - Retry with backoff (`avast/retry-go`) — max 3 attempts, 100ms base
  - Timeout: 5s per attempt

---

### Phase 7 — Payment Orchestrator (Saga)

- [ ] **T24**: DB migrations (`migrations/payment/`)
  - `001_create_payment.sql`
  - `002_create_payment_event.sql`
  - `003_create_outbox.sql`

- [ ] **T25**: Payment state machine
  - States: `INITIATED → CONSENT_RESERVED → RISK_PASSED → AUTHORISING → SETTLED | FAILED | MANUAL_REVIEW`
  - Every transition writes to `payment_event` (append-only)

- [ ] **T26**: Idempotency middleware
  - `Redis SET NX EX` before saga start
  - Concurrent duplicate: poll and wait max 5s

- [ ] **T27**: Saga orchestrator (the core)
  - Step 1: `ConsentService.ValidateAndReserve`
  - Step 2: `RiskService.Score`
  - Step 3: `BankAdapter.InitiatePayment`
  - Step 4: `LedgerService.PostDoubleEntry`
  - Step 5: write outbox row (same DB transaction as payment SETTLED update)
  - Compensation for each step on failure

- [ ] **T28**: Outbox relay goroutine
  - Polls `outbox` every 100ms
  - Publishes to Kafka `payment.events`
  - Deletes row on successful publish
  - Uses `kafka-go` writer

- [ ] **T29**: Manual retry endpoint `POST /v1/payments/:id/retry`
  - Only allowed from `FAILED` status
  - Re-runs saga with same idempotency key → skips idempotency guard (internal retry flag)

- [ ] **T30**: gRPC server + unit tests for saga (mock all downstream services)
  - Test: happy path SETTLED
  - Test: risk DECLINE → consent released
  - Test: bank failure → ledger not called, consent released
  - Test: duplicate request → idempotent response
  - Test: consent limit exceeded → immediate failure

---

### Phase 8 — Notification Service

- [ ] **T31**: Kafka consumer (`segmentio/kafka-go`)
  - Consumer group: `notification-service`
  - Topic: `payment.events`
  - Manual offset commit after successful delivery

- [ ] **T32**: Webhook delivery
  - Fetch merchant webhook URL via Merchant Service gRPC call
  - Build payload, sign with HMAC-SHA256
  - HTTP POST with 5s timeout

- [ ] **T33**: Retry loop
  - Exponential backoff: 1s, 2s, 4s, 8s, 30s, 300s (6 attempts)
  - After 6 failures: publish to `webhook.dlq` + log alert

- [ ] **T34**: Idempotency on consumer side
  - Store delivered `payment_id` in Redis SET (TTL 24h)
  - Skip re-delivery of already-delivered event

---

### Phase 9 — API Gateway

- [ ] **T35**: Chi router setup
  - Middleware stack: Logger → RequestID → Auth → RateLimit → Timeout

- [ ] **T36**: JWT auth middleware
  - Validate Bearer token, extract `merchant_id`
  - For demo: generate tokens via `POST /v1/auth/token` with API key

- [ ] **T37**: Rate limiting middleware
  - Redis token bucket: 100 req/s per merchant API key
  - Return `429 Too Many Requests` with `Retry-After` header

- [ ] **T38**: Route handlers (thin — just marshal/unmarshal, delegate to gRPC)
  - Merchant CRUD
  - Consent CRUD + revoke
  - Payment initiate + status + retry

---

### Phase 10 — Observability

- [ ] **T39**: OpenTelemetry setup (shared package)
  - Tracer provider → Jaeger exporter
  - gRPC interceptors (unary + stream) for auto-trace propagation
  - HTTP middleware for gateway

- [ ] **T40**: Prometheus metrics (per service)
  - `payment_saga_duration_seconds` (histogram, by final status)
  - `risk_score_distribution` (histogram)
  - `webhook_delivery_attempts_total` (counter, by result)
  - `consent_limit_reservations_active` (gauge)

- [ ] **T41**: Grafana dashboards
  - Payment funnel: INITIATED → SETTLED conversion rate
  - Saga step P99 latencies
  - Webhook delivery success rate
  - Risk decision distribution

- [ ] **T42**: Structured logging (`log/slog` throughout)
  - Every saga step logs: payment_id, step, duration, result
  - Add `trace_id` and `span_id` to every log line

---

### Phase 11 — Integration Tests & CI

- [ ] **T43**: End-to-end integration test (testcontainers)
  - Spin up: PostgreSQL, Redis, Kafka, all 7 services
  - Test: full happy path — register merchant → create consent → initiate payment → verify SETTLED → verify webhook delivered
  - Test: consent limit exceeded
  - Test: duplicate payment (idempotency)
  - Test: bank failure → FAILED → retry → SETTLED

- [ ] **T44**: `golangci-lint` configuration (`.golangci.yml`)
  - Enable: `errcheck`, `staticcheck`, `revive`, `govet`, `gocyclo`, `exhaustive`

- [ ] **T45**: GitHub Actions CI pipeline
  - `go test ./...` with race detector (`-race`)
  - `golangci-lint run`
  - `docker-compose up -d` + integration tests

---

### Bonus (if time allows)

- [ ] **T46**: Admin CLI (`cobra` + `viper`)
  - `vrp-admin merchants list`
  - `vrp-admin consents revoke <id>`
  - `vrp-admin payments retry <id>`
  - `vrp-admin webhook replay <payment_id>`

- [ ] **T47**: Consumer self-service consent portal (pure HTML + HTMX, no React)
  - View active consents
  - Revoke consent (calls DELETE /v1/consents/:id)

- [ ] **T48**: Prometheus alert rules
  - `PaymentSagaP99 > 500ms` → critical
  - `WebhookDeliverySuccessRate < 0.95` → warning
  - `CircuitBreakerOpen{service="bank-adapter"}` → critical

---

## Quick Reference — Key Interview Talking Points

| Topic | This Demo Covers |
|-------|-----------------|
| Idempotency | Redis SET NX + DB unique constraint |
| Distributed transactions | Orchestration Saga with compensation |
| Event-driven | Kafka outbox pattern, at-least-once delivery |
| Limit enforcement | Pessimistic locking + reservation pattern |
| Resilience | Circuit breaker, retry, dead-letter |
| Observability | OTel traces across 5 services, Prometheus, Grafana |
| Security | HMAC webhook signing, JWT, bcrypt key hashing |
| Double-entry ledger | DB trigger enforces balance invariant |
| State machines | Payment + Consent + Merchant all have explicit states |
| gRPC | Service-to-service, proto contracts, interceptors |

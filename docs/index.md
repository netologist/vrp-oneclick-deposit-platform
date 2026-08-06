# VRP One-Click Deposit System

Welcome to the documentation for the **Variable Recurring Payments (VRP) One-Click Deposit Platform**.

This system is an enterprise-grade, event-driven microservices backend implemented in **Go (Go 1.23+)**. It demonstrates production-ready patterns for handling Pay-by-Bank / Open Banking payment initiation, real-time risk evaluation, limit enforcement, double-entry financial accounting, and reliable webhook delivery.

---

## What Does This System Do?

In traditional e-commerce or iGaming platforms, depositing funds via a bank usually requires consumer redirection to their mobile banking app or web portal on **every single transaction** (SCA - Strong Customer Authentication).

**VRP (Variable Recurring Payments)** changes this model:
1. **First-time deposit**: The consumer approves a **VRP Consent** with specified parameters (e.g., maximum £200 per transaction, maximum £1,000 per rolling 30-day window).
2. **Subsequent deposits (One-Click)**: The merchant calls the API Gateway to pull funds immediately using the pre-authorized consent. The payment executes in **< 200ms** with zero user friction (no redirect, no Face ID, no SMS OTP).

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            CORE USER JOURNEYS                               │
│                                                                             │
│ Journey A: First Deposit (Consent Creation)                                 │
│ Consumer -> Merchant Site -> Open Banking Auth -> Consent Stored (ACTIVE)   │
│                                                                             │
│ Journey B: Repeat Deposit (One-Click Payment)                               │
│ Consumer clicks "Deposit £50" -> API Gateway -> Payment Saga -> Settled    │
│   ├─ Check Idempotency (Redis SET NX)                                       │
│   ├─ Consent Service (Pessimistic Lock Limit Reservation)                   │
│   ├─ Risk Service (Velocity & Blocklist Check)                             │
│   ├─ Bank Adapter (Initiate Payment via Open Banking API)                  │
│   ├─ Ledger Service (Double-Entry Bookkeeping: Debit Consumer/Credit Escrow) │
│   └─ Outbox Relay -> Kafka -> Notification Service -> Merchant Webhook     │
│                                                                             │
│ Journey C: Consent Revocation                                               │
│ Consumer revokes in Bank App -> Consent Marked REVOKED -> Saga Rejects      │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Business Requirements & Core Guarantees

| Requirement | Target & Enforcement Strategy |
|-------------|-------------------------------|
| **P99 Latency** | **< 500ms** total saga execution time |
| **Availability** | **99.95%** uptime with circuit breakers & retries |
| **Idempotency** | Exactly-once financial mutation guaranteed via Redis distributed locks & DB constraints |
| **Financial Integrity** | Double-entry accounting with DB-enforced zero-imbalance invariant |
| **Limit Safeguards** | Pessimistic locking (`SELECT FOR UPDATE`) on rolling 30-day window limits |
| **Auditability** | Append-only event history for all payment state transitions |

---

## Monorepo Layout

The project uses a Go workspace (`go.work`) containing 8 isolated microservices plus shared libraries and generated code:

```
vrp-system/
├── cmd/ (entry points within services/)
├── gen/                    # Auto-generated gRPC code (Protobuf + gRPC-Go)
├── pkg/shared/             # Shared domain types, money, errors, JWT, HMAC, DB & Redis helpers
├── services/
│   ├── gateway/            # REST API Gateway (Chi Router, JWT Auth, Rate Limiter)
│   ├── merchant-svc/       # Merchant management & API Key hash verification
│   ├── consent-svc/        # VRP consent lifecycle & pessimistic reservation
│   ├── payment-svc/        # Saga Orchestrator & Transactional Outbox Relay
│   ├── risk-svc/           # Rule engine & velocity counters
│   ├── ledger-svc/         # Double-entry ledger with balance constraint triggers
│   ├── bank-adapter/       # Anti-Corruption layer to Open Banking (Mock Bank HTTP + Circuit Breaker)
│   └── notification-svc/   # Kafka consumer for reliable HMAC-signed webhooks
├── migrations/             # Per-service PostgreSQL migrations
├── k8s/                    # Production Kubernetes & Kind manifests
├── deploy/kind/            # Kind cluster creation and deployment scripts
├── .github/workflows/      # Path-filtered fan-out/fan-in GitHub Actions CI pipeline
└── docs/                   # Documentation & MkDocs website
```

# Unimplemented Tasks & Future Roadmap

This document lists the tasks, features, and infrastructure items from `vrp-system-design.md` and `vrp-contracts-and-infra.md` that remain **unimplemented or deferred** in the current repository.

---

## Executive Summary

The platform has a **100% complete and fully working core runtime** (API Gateway, 7 Microservices, Orchestration Saga, Double-Entry Ledger, Idempotency, Transactional Outbox, HMAC Webhooks, Unit Tests, Kind K8s Cluster, and GitHub Actions CI/CD).

The items below represent production readiness hardening, advanced observability, operational tooling, and optional bonus features.

---

## Unimplemented Items by Category

### 1. Observability & Telemetry (Phase 10)

- [ ] **T39: OpenTelemetry Distributed Tracing Setup**
  - **Spec**: Initialize OTel TracerProvider with OTLP/Jaeger exporter in a shared package. Wire gRPC unary/stream interceptors and HTTP middleware across all 5 service hops.
  - **Current State**: Jaeger is running in Docker Compose and Kind, but Go microservices currently do not propagate OTel trace contexts via gRPC metadata.

- [ ] **T40: Custom Prometheus Metrics**
  - **Spec**: Instrument Go microservices with `prometheus/client_golang` metrics:
    - `payment_saga_duration_seconds` (Histogram by status)
    - `risk_score_distribution` (Histogram)
    - `webhook_delivery_attempts_total` (Counter by status code)
    - `consent_limit_reservations_active` (Gauge)
  - **Current State**: Services expose standard `/metrics` or `/healthz` endpoints; custom business metric counters/histograms are not yet registered.

- [ ] **T41: Grafana Dashboard JSON Manifests**
  - **Spec**: Pre-configured JSON dashboard files stored in `deploy/grafana/dashboards/` for Payment Funnel conversion rate, Saga step P99 latencies, Webhook success rate, and Risk distribution.
  - **Current State**: Grafana container runs with Prometheus data source configured, but dashboards are not pre-provisioned via JSON files.

- [ ] **T42: Trace-Correlated Structured Logging**
  - **Spec**: Extract `trace_id` and `span_id` from OpenTelemetry context and attach them to every `slog` log record.
  - **Current State**: Services use `log/slog` for structured logging, but trace IDs are not injected into log attributes.

---

### 2. Testing & Quality Assurance (Phase 11)

- [ ] **T43: Programmatic `testcontainers-go` Integration Suite**
  - **Spec**: In-code Go integration test file using `testcontainers/testcontainers-go` to programmatically spin up PostgreSQL, Redis, Kafka containers during `go test -tags=integration ./...`.
  - **Current State**: E2E integration testing is fully covered by `./scripts/e2e-smoke.sh` (against Docker Compose or Kind cluster) and table-driven unit tests with `miniredis` & mocks.

- [ ] **T44: `golangci-lint` Configuration File**
  - **Spec**: `.golangci.yml` file configuring linters (`errcheck`, `staticcheck`, `revive`, `govet`, `gocyclo`, `exhaustive`).
  - **Current State**: Linter target exists in Makefile (`make lint`), but `.golangci.yml` is not custom-configured.

---

### 3. Bonus Features & Operational Tooling

- [ ] **T46: Admin CLI Tool (`vrp-admin`)**
  - **Spec**: Operational CLI tool built with `spf13/cobra` + `spf13/viper`:
    - `vrp-admin merchants list`
    - `vrp-admin consents revoke <id>`
    - `vrp-admin payments retry <id>`
    - `vrp-admin webhook replay <payment_id>`
  - **Current State**: Operational tasks are executed via API Gateway REST endpoints or `psql`/`redis-cli` commands.

- [ ] **T47: Consumer Self-Service Consent Portal**
  - **Spec**: Lightweight HTMX + HTML user interface allowing end-consumers to view active VRP consents and revoke them.
  - **Current State**: Consent creation and revocation are fully implemented in `consent-svc` and exposed via API Gateway REST API.

- [ ] **T48: Prometheus Alerting Rules (`alerts.yml`)**
  - **Spec**: Prometheus alert definitions:
    - `PaymentSagaP99 > 500ms` (Critical)
    - `WebhookDeliverySuccessRate < 0.95` (Warning)
    - `CircuitBreakerOpen{service="bank-adapter"}` (Critical)
  - **Current State**: Circuit breaker and retry logic exist in code; alert manager rules are not deployed.

---

### 4. Schema Registry & Production Security

- [ ] **Remote `buf.build` Schema Registry Push**
  - **Spec**: Run `buf push` in CI pipeline to publish Protobuf contracts to `buf.build/org/vrp-demo`.
  - **Current State**: Local code generation uses `buf generate` and `proto/buf.gen.yaml`.

- [ ] **Vault & External Secrets Operator Integration**
  - **Spec**: Real Vault integration with `ExternalSecrets` operator for dynamic secrets injection in K8s.
  - **Current State**: Secrets are managed via `k8s/secrets.yaml` (Base64 stringData placeholder).

- [ ] **Service Mesh mTLS (Istio / Linkerd)**
  - **Spec**: Automatic mutual TLS between microservices using service mesh or cert-manager certificates.
  - **Current State**: Service-to-service gRPC uses unencrypted TCP in local/kind cluster setups.

---

## Summary Checklist

```
[X] Phase 1  — Foundation (Go workspace, Protos, Migrations, Shared Libs, Compose)
[X] Phase 2  — Merchant Service (Registration, API Key bcrypt, KYB status)
[X] Phase 3  — Consent Service (Limit reservation, SELECT FOR UPDATE, Redis cache)
[X] Phase 4  — Risk Service (Rule engine, Redis velocity, Blocklist)
[X] Phase 5  — Ledger Service (Double-entry, Deferred constraint trigger, Balance)
[X] Phase 6  — Bank Adapter (Mock Bank HTTP, Circuit Breaker, Retries)
[X] Phase 7  — Payment Orchestrator (5-step Saga, Idempotency, Outbox Relay)
[X] Phase 8  — Notification Service (Kafka consumer, HMAC webhooks, DLQ)
[X] Phase 9  — API Gateway (Chi router, JWT auth, Rate limit, Swagger UI)
[ ] Phase 10 — Advanced Observability (OTel tracing interceptors & Prometheus custom metrics)
[X] Phase 11 — CI & Integration (Kind cluster, E2E acceptance tests, GitHub Actions fan-out/fan-in)
[ ] Bonus    — Admin CLI (vrp-admin), HTMX Consumer Portal, Prometheus Alert Rules
```

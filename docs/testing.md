# Testing Strategy & Execution Guide

This document outlines the testing architecture, unit testing practices, and end-to-end acceptance testing procedures for the VRP platform.

---

## Testing Pyramid

```
           /\
          /  \         End-to-End Acceptance Tests (e2e-smoke.sh)
         /    \        Real API Gateway, gRPC, PostgreSQL, Redis, Kafka
        /------\
       /        \      Microservice Unit & Integration Tests
      /          \     Table-driven, miniredis, mock gRPC servers, fake repos
     /------------\
    /              \   Shared Package Unit Tests (pkg/shared)
   /________________\  Money arithmetic, domain errors, HMAC, JWT
```

---

## 1. Unit Testing Strategy

Unit tests are written using idiomatic **Table-Driven Tests** with standard library `testing` and `stretchr/testify` assertions. External network and database dependencies are replaced with in-memory mocks:

- **Redis**: Mocked using `github.com/alicebob/miniredis/v2`.
- **gRPC Clients**: Mocked using in-memory `grpc.NewServer()` listeners (`startMockGRPC` helper).
- **PostgreSQL Repositories**: Mocked using thread-safe in-memory maps (`fakeRepo`).

### Running Unit Tests

Run all unit tests across shared libraries and microservices:
```bash
make test
```

Or run tests for a specific service:
```bash
cd services/payment-svc
go test ./internal/... -v -count=1
```

### Key Test Coverage Highlights

- **`payment-svc/internal/saga_test.go`**:
  - `TestSagaOrchestrator_HappyPath`: Full 5-step saga execution verifying state transitions to `SETTLED`.
  - `TestSagaOrchestrator_RiskDecline`: Verifies automatic reservation release compensation when risk declines.
  - `TestSagaOrchestrator_ConsentLimitExceeded`: Verifies immediate failure mapping on limit breaches.
  - `TestPlatformFee`: Validates 1% platform fee calculation logic ($50.00 \rightarrow £0.50$ fee).
- **`merchant-svc/internal/service_test.go`**: Validation rules and API Key `vrp_` format verification.
- **`consent-svc/internal/service_test.go`**: Input validation and rolling limit boundary conditions.
- **`gateway/internal/httpapi/router_test.go`**: `httptest` routes, `/healthz` endpoints, and JWT Auth middleware protection.
- **`notification-svc/internal/deliver_test.go`**: HMAC-SHA256 signature verification over HTTP.
- **`bank-adapter/internal/mockbank/server_test.go`**: Mock Bank HTTP server response generation.
- **`ledger-svc/internal/store_test.go`**: Account type and Direction enum database converters.

---

## 2. End-to-End (E2E) Acceptance Testing

The `./scripts/e2e-smoke.sh` script executes a full black-box acceptance suite against a live system (either local process stack or Kind Kubernetes cluster).

### Acceptance Scenario Workflow

```
1. GET /healthz/live -> Verify Gateway is healthy
2. POST /v1/merchants -> Register merchant & acquire plaintext API Key
3. POST /v1/auth/token -> Exchange API Key for JWT Bearer Token
4. POST /v1/consents -> Create active VRP Consent (max £200/tx, £1000/month)
5. POST /v1/payments -> Initiate payment with Idempotency-Key
   - Assert HTTP 201 Created & status == SETTLED
6. POST /v1/payments (Replay) -> Resend request with same Idempotency-Key
   - Assert identical payment ID returned (no double charge)
7. Python HTTP Listener -> Catch async webhook POST delivered by Notification Service
   - Assert HMAC-SHA256 signature X-PC-Signature matches
```

### Executing E2E Smoke Test Locally

```bash
# Against local Docker Compose stack:
make up
make run-all
./scripts/e2e-smoke.sh

# Against Kind Kubernetes cluster:
./deploy/kind/deploy.sh
```

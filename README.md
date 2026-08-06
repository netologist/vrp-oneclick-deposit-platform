# VRP One-Click Deposit Demo

Pay-by-Bank / Open Banking VRP platform - interview-grade Go microservices demo.

## Architecture

```
Merchant → API Gateway (:8080)
              ├─ Merchant Service  (:50051)
              ├─ Consent Service   (:50052)
              └─ Payment Orchestrator (:50053)
                      ├─ Risk      (:50054)
                      ├─ Ledger    (:50055)
                      └─ Bank Adapter (:50056) → mock bank HTTP
                              │
                              ▼ Kafka payment.events
                      Notification Service → merchant webhook
```

## Quick start

```bash
# 1) Infra (Postgres, Redis, Redpanda/Kafka, Jaeger, Prometheus, Grafana)
make up

# 2) Generate protos (only if .proto changed)
make proto

# 3) Build binaries into ./bin
make build

# 4) Run all 8 processes (logs/ + pids)
./scripts/run-all.sh
# or: make run-all

# 5) Smoke test: register → JWT → consent → payment SETTLED → webhook
./scripts/e2e-smoke.sh

# Stop app processes
./scripts/stop-all.sh
```

Default secrets/URLs are demo-local (`vrp:vrp@localhost`, `JWT_SECRET=super-secret-jwt-key`).

| Service | Port |
|---------|------|
| Gateway HTTP | 8080 |
| Merchant gRPC | 50051 |
| Consent gRPC | 50052 |
| Payment gRPC | 50053 |
| Risk gRPC | 50054 |
| Ledger gRPC | 50055 |
| Bank Adapter gRPC | 50056 |
| Mock Bank HTTP | 18080 |
| Postgres | 5432 |
| Redis | 6379 |
| Kafka (Redpanda) | 19092 |
| Jaeger UI | 16686 |
| Grafana | 3000 |

## Module layout

Go workspace (`go.work`) - one module per service + `gen` + `pkg/shared`.

# Local Development & Kind Guide

This guide details how to run, test, and deploy the VRP platform locally using **Docker Compose** or **Kind (Kubernetes in Docker)**.

---

## Prerequisites

Ensure the following tools are installed on your workstation:
- **Go 1.23+** (`go version`)
- **Docker & Docker Compose** (`docker compose version`)
- **Kind** (`kind version`)
- **kubectl** (`kubectl version --client`)
- **make** (`make --version`)

---

## Option 1: Development via Docker Compose & Local Binaries

This is the fastest method for active feature development.

### 1. Start Infrastructure Dependencies
Spin up PostgreSQL, Redis, Redpanda (Kafka), Jaeger, Prometheus, and Grafana:
```bash
make up
```

### 2. Build Binaries
Compile all 8 microservices into `./bin`:
```bash
make build
```

### 3. Start Application Services
Launch all background processes in parallel with logging redirected to `./logs/`:
```bash
./scripts/run-all.sh
```

### 4. Run End-to-End Acceptance Tests
Executes merchant registration, JWT token generation, VRP consent creation, idempotent payment initiation, and HMAC webhook verification:
```bash
./scripts/e2e-smoke.sh
```

### 5. Stop Local Processes
```bash
./scripts/stop-all.sh
```

---

## Option 2: Kubernetes Deployment via Kind Cluster

This environment reproduces production Kubernetes behavior including ingress routing, Secret mounting, and database migration jobs.

### 1. Create Kind Cluster & Ingress Controller
```bash
./deploy/kind/create-cluster.sh
```
This provisions a 3-node Kind cluster (`vrp-demo`) with port mappings for `8080:80` (HTTP) and `8443:443` (HTTPS), and installs `ingress-nginx`.

### 2. Build, Load & Deploy
```bash
./deploy/kind/deploy.sh
```
This automated script:
1. Builds multi-stage Docker images (`vrp-demo/<service>:dev`) for all 8 microservices.
2. Loads images directly into the Kind cluster nodes.
3. Applies base Kubernetes manifests from `k8s/` via Kustomize.
4. Executes the one-shot DB migration job (`k8s/jobs/migrate.yaml`).
5. Waits for pod readiness and executes the `e2e-smoke.sh` acceptance suite against the Ingress controller.

### 3. Verify Cluster Status
```bash
kubectl -n vrp-demo get pods,svc,ingress,hpa
```

### 4. Teardown Kind Cluster
```bash
kind delete cluster --name vrp-demo
```

---

## Environment Variables Reference

| Variable | Default Value | Description |
|----------|---------------|-------------|
| `GATEWAY_HTTP_ADDR` | `:8080` | API Gateway listen address |
| `MERCHANT_SVC_ADDR` | `localhost:50051` | Merchant Service gRPC endpoint |
| `CONSENT_SVC_ADDR` | `localhost:50052` | Consent Service gRPC endpoint |
| `PAYMENT_SVC_ADDR` | `localhost:50053` | Payment Orchestrator gRPC endpoint |
| `RISK_SVC_ADDR` | `localhost:50054` | Risk Service gRPC endpoint |
| `LEDGER_SVC_ADDR` | `localhost:50055` | Ledger Service gRPC endpoint |
| `BANK_ADAPTER_ADDR` | `localhost:50056` | Bank Adapter gRPC endpoint |
| `MOCK_BANK_HTTP_ADDR` | `localhost:18080` | Mock Bank HTTP endpoint |
| `REDIS_ADDR` | `localhost:6379` | Redis server address |
| `KAFKA_BROKERS` | `localhost:19092` | Kafka / Redpanda broker list |
| `JWT_SECRET` | `super-secret-jwt-key` | JWT HMAC signing secret |

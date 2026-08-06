# Design Spec Gaps & Technical Solutions

This document highlights the technical ambiguities, edge cases, and gaps present in the initial design documents (`vrp-system-design.md` and `vrp-contracts-and-infra.md`), and explains how they were resolved in the Go implementation.

---

## Summary of Design Refinements

| # | Topic | Initial Spec Ambiguity / Problem | Production Implementation Solution |
|---|-------|----------------------------------|------------------------------------|
| 1 | **Ledger Balance Trigger** | `AFTER INSERT` trigger failed on line 1 before full entry was inserted | Implemented **`DEFERRABLE INITIALLY DEFERRED` Constraint Trigger** |
| 2 | **API Key Lookups** | $O(N)$ bcrypt comparisons across all stored key hashes | Added **`key_prefix` column** for $O(1)$ indexed lookup |
| 3 | **Consent Limit Tracking** | Mixed temporary holds with settled transactions in queries | Separated **`consent_reservation` (HELD/CONFIRMED/RELEASED)** and **`consent_usage`** |
| 4 | **Kubernetes Probes** | gRPC readiness probes configured without health protocol | Created **`pkg/shared/grpcutil` wrapping `grpc_health_v1`** |
| 5 | **Outbox Kafka Schema** | Generic `JSONB` without unified consumer contract | Defined strict **`PaymentEvent` contract** shared across Saga & Webhooks |

---

## Detailed Technical Analysis

### 1. PostgreSQL Deferred Constraint Trigger for Ledger

**The Issue in Design Spec**:
The initial design specified an `AFTER INSERT` trigger on `journal_line` to enforce $\sum \text{Debits} = \sum \text{Credits}$:
```sql
-- Initial spec snippet
CREATE TRIGGER check_journal_balance AFTER INSERT ON journal_line ...
```
In PostgreSQL, an `AFTER INSERT` trigger executes **immediately after each row is inserted**. When posting a 3-line journal entry, line 1 (Debit £50) is inserted first. The trigger fires immediately, observes an imbalance of +£50, and aborts the entire transaction before lines 2 and 3 can be inserted!

**Our Solution**:
We changed the trigger to a **Constraint Trigger** configured with `DEFERRABLE INITIALLY DEFERRED`:
```sql
CREATE CONSTRAINT TRIGGER trg_journal_balance
    AFTER INSERT ON journal_line
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW
    EXECUTE FUNCTION check_journal_balance();
```
With `DEFERRABLE INITIALLY DEFERRED`, PostgreSQL defers trigger execution until the application issues `COMMIT;`. All journal lines land in the table first, and the balance validation checks the complete entry before committing.

---

### 2. $O(1)$ Indexed API Key Lookup

**The Issue in Design Spec**:
`vrp-system-design.md` suggested storing bcrypt hashes of API keys in the `api_key` table:
```sql
CREATE TABLE api_key (
    id UUID PRIMARY KEY,
    key_hash TEXT NOT NULL UNIQUE
);
```
Bcrypt is intentionally slow (cost factor 10 takes ~100ms CPU time). If an API Gateway receives an API Key `vrp_abc123...`, searching for it by running `bcrypt.CompareHashAndPassword` against all rows in `api_key` would require $N \times 100\text{ms}$ CPU time per request, completely destroying throughput ($O(N)$ complexity).

**Our Solution**:
We updated the schema and domain logic to store a **Key Prefix**:
```sql
CREATE TABLE api_key (
    id UUID PRIMARY KEY,
    merchant_id UUID NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,
    key_prefix TEXT NOT NULL,  -- first 8 characters of plaintext key
    status TEXT NOT NULL DEFAULT 'ACTIVE'
);
CREATE INDEX idx_api_key_prefix ON api_key(key_prefix);
```
When an API request arrives:
1. Extract the prefix: `prefix = apiKey[:8]` (e.g. `vrp_dfe9`).
2. Fast indexed SQL query: `SELECT * FROM api_key WHERE key_prefix = $1 AND status = 'ACTIVE';` ($O(1)$ time).
3. Run bcrypt comparison **only** against the candidate matches matching that prefix.

---

### 3. Consent Limit & Reservation State Machine

**The Issue in Design Spec**:
The spec suggested querying total spending in a rolling 30-day window:
```sql
SELECT SUM(amount_pence) FROM payment WHERE consent_id = $1 AND status IN ('AUTHORISING', 'SETTLED');
```
This had two flaws:
- In-flight payments in `CONSENT_RESERVED` or `RISK_PASSED` status were ignored, allowing concurrent requests to bypass limit checks.
- If a saga failed, temporary holds remained stuck until status updated, causing phantom limit failures.

**Our Solution**:
We established a strict state machine for reservations:
1. `consent_reservation`: Stores active reservations with status `HELD` and explicit `expires_at` timestamp (10 minutes).
2. `ValidateAndReserve`: Locks the consent row (`SELECT ... FOR UPDATE`), sums active `HELD` reservations + settled `consent_usage` in the 30-day window, and inserts a `HELD` reservation.
3. `ConfirmReservation`: On saga success, transitions reservation to `CONFIRMED` and inserts a permanent row in `consent_usage`.
4. `ReleaseReservation`: On saga failure, transitions reservation to `RELEASED`, instantly freeing headroom for subsequent requests.

---

### 4. Native gRPC Health Checking for Kubernetes

**The Issue in Design Spec**:
The Kubernetes manifests defined `readinessProbe` with `grpc: { port: 50052 }`. Kubernetes probes require containers to implement the standard Protobuf health checking protocol defined in `google.golang.org/grpc/health/grpc_health_v1`.

**Our Solution**:
We created a reusable helper package `pkg/shared/grpcutil`:
```go
func NewServer(addr string, opts ...grpc.ServerOption) *Server {
    gs := grpc.NewServer(opts...)
    hs := health.NewServer()
    healthpb.RegisterHealthServer(gs, hs)
    reflection.Register(gs)
    return &Server{GRPC: gs, Health: hs, addr: addr}
}
```
Each microservice registers its serving status on startup (`SetServingStatus(service, SERVING)`) and updates to `NOT_SERVING` during graceful shutdown, allowing Kubernetes to seamlessly stop routing traffic before pod termination.

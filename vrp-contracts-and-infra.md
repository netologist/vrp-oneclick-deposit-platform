# VRP Demo — Contracts, gRPC Docs, SQL Transactions, OpenAPI & Kubernetes

Supplement to `vrp-system-design.md`.

---

## Table of Contents

1. [gRPC Contracts — Full Proto Files](#1-grpc-contracts--full-proto-files)
2. [gRPC Documentation Tools](#2-grpc-documentation-tools)
3. [SQL Transactions with pgx/v5](#3-sql-transactions-with-pgxv5)
4. [OpenAPI Specification (API Gateway)](#4-openapi-specification-api-gateway)
5. [Kubernetes Manifests](#5-kubernetes-manifests)

---

## 1. gRPC Contracts — Full Proto Files

### Directory layout

```
proto/
├── buf.yaml                 ← buf.build config
├── buf.gen.yaml             ← code generation config
├── common/
│   └── v1/
│       └── common.proto     ← shared types (Money, Pagination)
├── merchant/
│   └── v1/
│       └── merchant.proto
├── consent/
│   └── v1/
│       └── consent.proto
├── risk/
│   └── v1/
│       └── risk.proto
├── ledger/
│   └── v1/
│       └── ledger.proto
├── bank/
│   └── v1/
│       └── bank.proto
└── payment/
    └── v1/
        └── payment.proto    ← orchestrator's internal state (not a service)
```

---

### `proto/common/v1/common.proto`

```protobuf
syntax = "proto3";
package common.v1;
option go_package = "github.com/yourname/vrp-demo/gen/common/v1;commonv1";

// Money is always stored as pence/cents (integer) + ISO 4217 currency code.
// NEVER use float or double for monetary values.
message Money {
  int64  amount_pence = 1;   // e.g. 5000 = £50.00
  string currency     = 2;   // e.g. "GBP", "CAD"
}

message PageRequest {
  int32  page_size  = 1;  // max 100
  string page_token = 2;  // cursor (base64-encoded last ID)
}

message PageResponse {
  string next_page_token = 1;  // empty = last page
  int32  total_count     = 2;
}
```

---

### `proto/merchant/v1/merchant.proto`

```protobuf
syntax = "proto3";
package merchant.v1;
option go_package = "github.com/yourname/vrp-demo/gen/merchant/v1;merchantv1";

import "google/protobuf/timestamp.proto";

service MerchantService {
  // Public: called by API Gateway
  rpc RegisterMerchant(RegisterMerchantRequest) returns (RegisterMerchantResponse);
  rpc GetMerchant    (GetMerchantRequest)     returns (Merchant);
  rpc SuspendMerchant(SuspendMerchantRequest) returns (Merchant);

  // Internal: called by other services
  rpc GetMerchantByApiKey(GetMerchantByApiKeyRequest) returns (Merchant);
  rpc GetWebhookConfig   (GetWebhookConfigRequest)    returns (WebhookConfig);
}

// ── Merchant ──────────────────────────────────────────────────────────────────

message Merchant {
  string id          = 1;
  string name        = 2;
  string kyb_status  = 3;  // PENDING_KYB | KYB_APPROVED | ACTIVE | SUSPENDED
  string status      = 4;
  string webhook_url = 5;
  google.protobuf.Timestamp created_at = 6;
  google.protobuf.Timestamp updated_at = 7;
}

message RegisterMerchantRequest {
  string name        = 1;  // required
  string webhook_url = 2;  // required; must be HTTPS
  string contact_email = 3;
}

message RegisterMerchantResponse {
  Merchant merchant = 1;
  string   api_key  = 2;  // plaintext — shown ONCE, then hashed; never stored again
}

message GetMerchantRequest {
  string merchant_id = 1;
}

message SuspendMerchantRequest {
  string merchant_id = 1;
  string reason      = 2;
}

message GetMerchantByApiKeyRequest {
  string api_key = 1;  // plaintext; service will bcrypt-compare
}

message GetWebhookConfigRequest {
  string merchant_id = 1;
}

message WebhookConfig {
  string merchant_id   = 1;
  string webhook_url   = 2;
  string hmac_secret   = 3;  // used by Notification Service to sign payloads
}
```

---

### `proto/consent/v1/consent.proto`

```protobuf
syntax = "proto3";
package consent.v1;
option go_package = "github.com/yourname/vrp-demo/gen/consent/v1;consentv1";

import "common/v1/common.proto";
import "google/protobuf/empty.proto";
import "google/protobuf/timestamp.proto";

service ConsentService {
  // Called by API Gateway (via Payment Orchestrator proxy)
  rpc CreateConsent (CreateConsentRequest)  returns (Consent);
  rpc GetConsent    (GetConsentRequest)     returns (Consent);
  rpc RevokeConsent (RevokeConsentRequest)  returns (Consent);
  rpc ListConsents  (ListConsentsRequest)   returns (ListConsentsResponse);

  // Called by Payment Orchestrator (saga steps)
  rpc ValidateAndReserve   (ReserveRequest)   returns (ReserveResponse);
  rpc ConfirmReservation   (ConfirmRequest)   returns (google.protobuf.Empty);
  rpc ReleaseReservation   (ReleaseRequest)   returns (google.protobuf.Empty);
  rpc GetRollingUsage      (UsageRequest)     returns (UsageResponse);
}

// ── Consent ───────────────────────────────────────────────────────────────────

enum ConsentStatus {
  CONSENT_STATUS_UNSPECIFIED = 0;
  PENDING                    = 1;  // waiting for bank auth
  ACTIVE                     = 2;  // ready for payments
  REVOKED                    = 3;  // cancelled by consumer or merchant
  EXPIRED                    = 4;  // valid_until passed
}

message Consent {
  string        id               = 1;
  string        merchant_id      = 2;
  string        consumer_id      = 3;  // external ref (bank user ID or email hash)
  string        bank_consent_ref = 4;  // Open Banking consent ID from bank
  ConsentStatus status           = 5;
  common.v1.Money max_per_transaction  = 6;
  common.v1.Money max_per_month        = 7;
  google.protobuf.Timestamp valid_from  = 8;
  google.protobuf.Timestamp valid_until = 9;
  google.protobuf.Timestamp created_at  = 10;
  google.protobuf.Timestamp updated_at  = 11;
}

message CreateConsentRequest {
  string merchant_id      = 1;  // injected from JWT by gateway
  string consumer_id      = 2;
  string bank_consent_ref = 3;
  common.v1.Money max_per_transaction = 4;  // e.g. {amount_pence: 20000, currency: "GBP"}
  common.v1.Money max_per_month       = 5;  // e.g. {amount_pence: 100000, currency: "GBP"}
  google.protobuf.Timestamp valid_until = 6;
}

message GetConsentRequest {
  string consent_id  = 1;
  string merchant_id = 2;  // used for ownership check
}

message RevokeConsentRequest {
  string consent_id  = 1;
  string merchant_id = 2;
  string reason      = 3;  // CONSUMER_REQUEST | MERCHANT_REQUEST | FRAUD | EXPIRED
}

message ListConsentsRequest {
  string consumer_id = 1;
  string merchant_id = 2;
  ConsentStatus status = 3;  // filter; 0 = all
  common.v1.PageRequest page = 4;
}

message ListConsentsResponse {
  repeated Consent consents = 1;
  common.v1.PageResponse page = 2;
}

// ── Reservation (saga step 1) ─────────────────────────────────────────────────

message ReserveRequest {
  string consent_id  = 1;
  string payment_id  = 2;  // used as idempotency key for the reservation
  common.v1.Money amount = 3;
}

message ReserveResponse {
  string reservation_id     = 1;
  common.v1.Money remaining_monthly = 2;  // for logging/debugging
}

message ConfirmRequest {
  string reservation_id = 1;
  string payment_id     = 2;
}

message ReleaseRequest {
  string reservation_id = 1;  // compensating action: called on saga failure
  string reason         = 2;
}

message UsageRequest {
  string consent_id = 1;
}

message UsageResponse {
  common.v1.Money used_this_month      = 1;
  common.v1.Money remaining_this_month = 2;
  int32           tx_count_this_month  = 3;
}
```

---

### `proto/risk/v1/risk.proto`

```protobuf
syntax = "proto3";
package risk.v1;
option go_package = "github.com/yourname/vrp-demo/gen/risk/v1;riskv1";

import "common/v1/common.proto";
import "google/protobuf/timestamp.proto";

service RiskService {
  rpc Score(ScoreRequest) returns (ScoreResponse);

  // Admin: manage blocklist
  rpc AddToBlocklist    (BlocklistRequest) returns (BlocklistEntry);
  rpc RemoveFromBlocklist(BlocklistRequest) returns (BlocklistEntry);
}

enum RiskDecision {
  RISK_DECISION_UNSPECIFIED = 0;
  ALLOW                     = 1;
  REVIEW                    = 2;  // flag for manual review but allow
  DECLINE                   = 3;
}

message ScoreRequest {
  string merchant_id  = 1;
  string consumer_id  = 2;
  string consent_id   = 3;
  string payment_id   = 4;  // for idempotency / audit
  common.v1.Money amount = 5;
  string ip_address   = 6;  // optional; passed from gateway
  string user_agent   = 7;  // optional
}

message ScoreResponse {
  int32        score    = 1;   // 0–100 (100 = highest risk)
  RiskDecision decision = 2;
  string       reason   = 3;   // human-readable; logged in payment_event
  repeated string rules_triggered = 4;  // e.g. ["VELOCITY_EXCEEDED", "NEW_CONSUMER_HIGH_VALUE"]
}

message BlocklistRequest {
  string type  = 1;  // CONSUMER | MERCHANT | IP
  string value = 2;
  string reason = 3;
}

message BlocklistEntry {
  string id        = 1;
  string type      = 2;
  string value     = 3;
  string reason    = 4;
  google.protobuf.Timestamp created_at = 5;
}
```

---

### `proto/ledger/v1/ledger.proto`

```protobuf
syntax = "proto3";
package ledger.v1;
option go_package = "github.com/yourname/vrp-demo/gen/ledger/v1;ledgerv1";

import "common/v1/common.proto";
import "google/protobuf/empty.proto";
import "google/protobuf/timestamp.proto";

service LedgerService {
  rpc PostDoubleEntry (PostEntryRequest)  returns (JournalEntry);
  rpc ReverseEntry    (ReverseRequest)    returns (google.protobuf.Empty);
  rpc GetBalance      (BalanceRequest)    returns (BalanceResponse);
  rpc GetJournalEntry (JournalEntryRequest) returns (JournalEntry);
}

enum Direction {
  DIRECTION_UNSPECIFIED = 0;
  DR                    = 1;  // Debit  (money out of account)
  CR                    = 2;  // Credit (money into account)
}

enum AccountType {
  ACCOUNT_TYPE_UNSPECIFIED = 0;
  CONSUMER_ESCROW          = 1;
  MERCHANT_ESCROW          = 2;
  PLATFORM_FEE             = 3;
}

message JournalLine {
  string      id          = 1;
  AccountType account_type = 2;
  string      owner_ref   = 3;  // merchant_id or consumer_id
  Direction   direction   = 4;
  common.v1.Money amount  = 5;
}

message JournalEntry {
  string   id          = 1;
  string   payment_id  = 2;  // FK to payment; idempotency key
  string   description = 3;
  repeated JournalLine lines = 4;  // always even; DR total = CR total
  google.protobuf.Timestamp created_at = 5;
}

// PostEntryRequest: caller provides all lines; service validates balance
message PostEntryRequest {
  string   payment_id  = 1;  // idempotency key — safe to retry
  string   description = 2;
  repeated JournalLine lines = 3;
  // Typical settlement entry (4 lines):
  // DR consumer_escrow £50
  // CR merchant_escrow £49.50
  // CR platform_fee    £0.50
  // (DR total = CR total = £50)
}

message ReverseRequest {
  string payment_id = 1;  // reverses the entry for this payment_id
  string reason     = 2;
}

message BalanceRequest {
  AccountType account_type = 1;
  string      owner_ref    = 2;
  string      currency     = 3;
}

message BalanceResponse {
  common.v1.Money balance = 1;
  google.protobuf.Timestamp as_of = 2;
}

message JournalEntryRequest {
  string payment_id = 1;
}
```

---

### `proto/bank/v1/bank.proto`

```protobuf
syntax = "proto3";
package bank.v1;
option go_package = "github.com/yourname/vrp-demo/gen/bank/v1;bankv1";

import "common/v1/common.proto";
import "google/protobuf/timestamp.proto";

service BankAdapter {
  rpc InitiatePayment  (InitiateRequest)  returns (InitiateResponse);
  rpc GetPaymentStatus (StatusRequest)    returns (StatusResponse);
  rpc ReversePayment   (ReverseRequest)   returns (ReverseResponse);
}

enum BankPaymentStatus {
  BANK_PAYMENT_STATUS_UNSPECIFIED = 0;
  PENDING                         = 1;
  AUTHORISED                      = 2;
  SETTLED                         = 3;
  REJECTED                        = 4;
  REVERSED                        = 5;
}

message InitiateRequest {
  string payment_id       = 1;  // our internal ID; used as bank's end-to-end ref
  string bank_consent_ref = 2;  // Open Banking consent ID
  string consumer_id      = 3;
  common.v1.Money amount  = 4;
  string description      = 5;
}

message InitiateResponse {
  string            bank_payment_ref = 1;  // e.g. "FPS-20260806-XYZ999"
  BankPaymentStatus status           = 2;
  string            failure_reason   = 3;  // populated on REJECTED
  google.protobuf.Timestamp initiated_at = 4;
}

message StatusRequest {
  string bank_payment_ref = 1;
}

message StatusResponse {
  string            bank_payment_ref = 1;
  BankPaymentStatus status           = 2;
  google.protobuf.Timestamp updated_at = 3;
}

message ReverseRequest {
  string bank_payment_ref = 1;
  string reason           = 2;
}

message ReverseResponse {
  string bank_payment_ref  = 1;
  string reversal_ref      = 2;
  BankPaymentStatus status = 3;
}
```

---

### `buf.yaml` + `buf.gen.yaml`

```yaml
# buf.yaml — root of proto/
version: v2
modules:
  - path: .
lint:
  use:
    - STANDARD
  except:
    - UNARY_RPC         # we use streaming only in future
breaking:
  use:
    - FILE              # detect breaking changes at file level
```

```yaml
# buf.gen.yaml
version: v2
plugins:
  - plugin: go
    out: ../gen
    opt: paths=source_relative
  - plugin: go-grpc
    out: ../gen
    opt:
      - paths=source_relative
      - require_unimplemented_servers=true
  - plugin: grpc-gateway         # generates REST proxy + OpenAPI from proto
    out: ../gen
    opt: paths=source_relative
  - plugin: openapiv2             # generates swagger.json from proto
    out: ../docs/swagger
```

---

## 2. gRPC Documentation Tools

### The Swagger Equivalents for gRPC

| Tool | Role | Command |
|------|------|---------|
| **buf.build** | Schema registry, breaking change detection, doc hosting | `buf push` → auto-published at `buf.build/yourname/vrp-demo` |
| **grpcui** | Web UI (like Swagger UI) — browse services, call RPCs | `grpcui -plaintext localhost:50051` |
| **Evans** | CLI client (like curl/httpie for gRPC) | `evans --host localhost --port 50051 repl` |
| **grpc-gateway** | Generates REST proxy + OpenAPI 2.0 spec from proto annotations | `buf generate` |
| **Postman** | Native gRPC support since v10 | Import `.proto` files |

### buf.build — Recommended for Production

```bash
# Install buf
brew install bufbuild/buf/buf

# From proto/ directory:
buf lint              # check proto style
buf breaking --against .git#branch=main    # detect breaking changes vs main
buf generate          # generate Go code + OpenAPI
buf push              # publish to buf.build schema registry (free for public)
```

After `buf push`, your schema is browsable at `https://buf.build/yourname/vrp-demo` with auto-generated documentation — every service, every RPC, every field documented.

### grpcui — Instant Web UI (use during development)

```bash
go install github.com/fullstorydev/grpcui/cmd/grpcui@latest

# Requires gRPC server reflection enabled (add in development only):
# grpc.NewServer(grpc.ChainUnaryInterceptor(...), reflection.Register(s))

grpcui -plaintext localhost:50051
# Opens http://localhost:8080 — dropdown of all services and RPCs,
# form fields for request, live response display
```

### Evans — CLI REPL for gRPC

```bash
go install github.com/ktr0731/evans@latest

evans --host localhost --port 50052 --proto proto/consent/v1/consent.proto repl

# Inside REPL:
> package consent.v1
> service ConsentService
> call GetConsent
consent_id (TYPE_STRING) => con_abc123
merchant_id (TYPE_STRING) => mer_xyz789
{
  "id": "con_abc123",
  "status": "ACTIVE",
  ...
}
```

### Enable Server Reflection (dev only — remove in prod)

```go
// cmd/consent-svc/main.go
import "google.golang.org/grpc/reflection"

s := grpc.NewServer(opts...)
consentv1.RegisterConsentServiceServer(s, handler)
reflection.Register(s)   // ← enables grpcui + Evans to discover services
```

---

## 3. SQL Transactions with pgx/v5

### Pattern 1: `BeginTx` (explicit, most control)

Use when you need specific isolation level or read-only mode.

```go
func (r *PaymentRepository) SettlePayment(
    ctx context.Context,
    paymentID string,
    bankRef string,
) error {
    tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{
        IsoLevel:   pgx.ReadCommitted,  // default; use Serializable for financial invariants
        AccessMode: pgx.ReadWrite,
    })
    if err != nil {
        return oops.Wrap(err, "begin tx")
    }
    defer tx.Rollback(ctx) // no-op if already committed

    // Step 1: update payment status
    _, err = tx.Exec(ctx, `
        UPDATE payment
        SET status = 'SETTLED', bank_payment_ref = $1, settled_at = NOW(), updated_at = NOW()
        WHERE id = $2 AND status = 'AUTHORISING'
    `, bankRef, paymentID)
    if err != nil {
        return oops.Wrap(err, "update payment")
    }

    // Step 2: write audit event (same tx — either both land or neither)
    _, err = tx.Exec(ctx, `
        INSERT INTO payment_event (id, payment_id, from_status, to_status, reason)
        VALUES (gen_random_uuid(), $1, 'AUTHORISING', 'SETTLED', 'bank confirmed')
    `, paymentID)
    if err != nil {
        return oops.Wrap(err, "insert event")
    }

    // Step 3: write outbox row (same tx — guaranteed Kafka will get this)
    _, err = tx.Exec(ctx, `
        INSERT INTO outbox (id, topic, key, payload)
        VALUES (gen_random_uuid(), 'payment.events', $1, $2)
    `, paymentID, buildOutboxPayload(paymentID))
    if err != nil {
        return oops.Wrap(err, "insert outbox")
    }

    return tx.Commit(ctx)
}
```

---

### Pattern 2: `pgx.BeginFunc` (auto-rollback on error — less boilerplate)

```go
func (r *ConsentRepository) ReserveLimitTx(
    ctx context.Context,
    req ReserveRequest,
) (ReserveResponse, error) {
    var resp ReserveResponse

    err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
        // SELECT FOR UPDATE: lock the consent row + sum reservations
        // No other concurrent saga can reserve on this consent until we commit
        var usedPence int64
        err := tx.QueryRow(ctx, `
            SELECT COALESCE(SUM(amount_pence), 0)
            FROM payment
            WHERE consent_id = $1
              AND status IN ('AUTHORISING', 'SETTLED')
              AND settled_at > NOW() - INTERVAL '30 days'
            FOR UPDATE
        `, req.ConsentID).Scan(&usedPence)
        if err != nil {
            return oops.Wrap(err, "query rolling usage")
        }

        // fetch consent limits
        var maxMonthly int64
        err = tx.QueryRow(ctx, `
            SELECT max_monthly_pence FROM consent WHERE id = $1 AND status = 'ACTIVE'
        `, req.ConsentID).Scan(&maxMonthly)
        if err != nil {
            return oops.Wrap(err, "fetch consent limits")
        }

        if usedPence+req.AmountPence > maxMonthly {
            return ErrLimitExceeded
        }

        // insert reservation (expires in 10 min if saga doesn't complete)
        var reservationID string
        err = tx.QueryRow(ctx, `
            INSERT INTO consent_reservation (id, consent_id, payment_id, amount_pence, expires_at)
            VALUES (gen_random_uuid(), $1, $2, $3, NOW() + INTERVAL '10 minutes')
            RETURNING id
        `, req.ConsentID, req.PaymentID, req.AmountPence).Scan(&reservationID)
        if err != nil {
            return oops.Wrap(err, "insert reservation")
        }

        resp = ReserveResponse{
            ReservationID:    reservationID,
            RemainingMonthly: maxMonthly - usedPence - req.AmountPence,
        }
        return nil
    })
    // pgx.BeginFunc automatically calls Rollback if the func returns error,
    // or Commit if it returns nil.

    return resp, err
}
```

---

### Pattern 3: Serializable Isolation (Ledger — balance invariant)

Use `Serializable` when you need full ACID: no phantom reads, no write skew.
The DB trigger already enforces balance, but Serializable prevents concurrent
entries that could bypass the trigger window.

```go
func (r *LedgerRepository) PostEntry(
    ctx context.Context,
    req PostEntryRequest,
) (*JournalEntry, error) {
    var entry *JournalEntry

    err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{
        IsoLevel: pgx.Serializable,
    }, func(tx pgx.Tx) error {
        // Idempotency check: if entry for this payment_id exists, return it
        existing, err := r.findEntryByPaymentID(ctx, tx, req.PaymentID)
        if err == nil {
            entry = existing
            return nil // already posted — idempotent success
        }
        if !errors.Is(err, pgx.ErrNoRows) {
            return oops.Wrap(err, "check existing entry")
        }

        // Insert journal_entry header
        var entryID string
        err = tx.QueryRow(ctx, `
            INSERT INTO journal_entry (id, payment_id, description)
            VALUES (gen_random_uuid(), $1, $2)
            RETURNING id
        `, req.PaymentID, req.Description).Scan(&entryID)
        if err != nil {
            return oops.Wrap(err, "insert journal_entry")
        }

        // Insert journal_lines (batch insert)
        batch := &pgx.Batch{}
        for _, line := range req.Lines {
            batch.Queue(`
                INSERT INTO journal_line (id, journal_entry_id, account_id, direction, amount_pence)
                VALUES (gen_random_uuid(), $1, $2, $3, $4)
            `, entryID, line.AccountID, line.Direction, line.AmountPence)
        }
        results := tx.SendBatch(ctx, batch)
        defer results.Close()
        for range req.Lines {
            if _, err := results.Exec(); err != nil {
                return oops.Wrap(err, "insert journal_line")
            }
        }

        // DB trigger fires here and raises exception if DR != CR
        // If trigger fires → tx.Rollback() is called automatically by BeginTxFunc

        entry = &JournalEntry{ID: entryID, PaymentID: req.PaymentID}
        return nil
    })

    return entry, err
}
```

---

### Pattern 4: Savepoints (nested rollback without losing the outer tx)

```go
// Use savepoints when you want to try something inside a tx,
// roll back only that part on failure, and continue the outer tx.

func (r *Repo) TryOptimisticUpdate(ctx context.Context, tx pgx.Tx, id string) error {
    // Create savepoint
    _, err := tx.Exec(ctx, "SAVEPOINT sp_optimistic")
    if err != nil {
        return err
    }

    _, err = tx.Exec(ctx, `UPDATE consent SET status='EXPIRED' WHERE id=$1`, id)
    if err != nil {
        // Roll back only to savepoint — outer tx is still alive
        tx.Exec(ctx, "ROLLBACK TO SAVEPOINT sp_optimistic")
        return ErrConcurrentModification
    }

    _, err = tx.Exec(ctx, "RELEASE SAVEPOINT sp_optimistic")
    return err
}
```

---

### Pattern 5: Passing tx via explicit parameter (NOT via context)

```go
// ✅ CORRECT — pass tx explicitly; clean, testable, no magic
func (r *PaymentRepository) UpdateStatus(ctx context.Context, tx pgx.Tx, id, status string) error {
    _, err := tx.Exec(ctx, `UPDATE payment SET status=$1 WHERE id=$2`, status, id)
    return err
}

// ❌ AVOID — storing tx in context; breaks type safety, hard to test
func SaveToContext(ctx context.Context, tx pgx.Tx) context.Context {
    return context.WithValue(ctx, txKey{}, tx) // don't do this
}
```

---

### Pattern 6: Connection pool health in Kubernetes

```go
// Readiness probe: only return 200 if DB is reachable
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
    defer cancel()

    if err := h.pool.Ping(ctx); err != nil {
        slog.ErrorContext(ctx, "db ping failed", "err", err)
        w.WriteHeader(http.StatusServiceUnavailable)
        return
    }
    w.WriteHeader(http.StatusOK)
}

// Pool config (tune per service)
pool, err := pgxpool.New(ctx, connString)
pool.Config().MaxConns = 20
pool.Config().MinConns = 2
pool.Config().MaxConnLifetime = 1 * time.Hour
pool.Config().MaxConnIdleTime = 30 * time.Minute
pool.Config().HealthCheckPeriod = 1 * time.Minute
```

---

## 4. OpenAPI Specification (API Gateway)

### Tooling Choice

Use **`swaggo/swag`** — annotate handlers with comments, auto-generate `swagger.json`.

```bash
go install github.com/swaggo/swag/cmd/swag@latest
swag init -g cmd/gateway/main.go -o docs/
# generates docs/swagger.json + docs/swagger.yaml + docs/docs.go
```

Then serve Swagger UI:
```go
import httpSwagger "github.com/swaggo/http-swagger/v2"
_ "github.com/yourname/vrp-demo/docs"  // side-effect: register generated docs

r.Get("/docs/*", httpSwagger.Handler(
    httpSwagger.URL("/docs/doc.json"),
))
```

---

### OpenAPI 3.0 Spec (condensed, hand-written for demo clarity)

```yaml
# docs/openapi.yaml
openapi: "3.0.3"
info:
  title: VRP One-Click Deposit API
  description: |
    Pay-by-Bank / VRP payment platform.
    All monetary values are in pence (integer). £50.00 = 5000 pence.
    All requests to /v1/* require Bearer JWT.
    Idempotency-Key header is required for POST /v1/payments.
  version: "1.0.0"
  contact:
    name: Platform Team
    email: platform@vrp-demo.internal

servers:
  - url: https://api.vrp-demo.internal/v1
    description: Production
  - url: http://localhost:8080/v1
    description: Local development

security:
  - bearerAuth: []

components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT

  schemas:
    Money:
      type: object
      required: [amount_pence, currency]
      properties:
        amount_pence:
          type: integer
          format: int64
          example: 5000
          description: Amount in smallest currency unit (pence). £50.00 = 5000.
        currency:
          type: string
          example: GBP
          pattern: "^[A-Z]{3}$"

    Error:
      type: object
      required: [code, message]
      properties:
        code:
          type: string
          example: CONSENT_LIMIT_EXCEEDED
          enum:
            - CONSENT_LIMIT_EXCEEDED
            - CONSENT_EXPIRED
            - CONSENT_REVOKED
            - RISK_DECLINED
            - BANK_REJECTED
            - BANK_UNAVAILABLE
            - DUPLICATE_IDEMPOTENCY_KEY
            - MERCHANT_SUSPENDED
            - VALIDATION_ERROR
            - INTERNAL_ERROR
        message:
          type: string
          example: "Monthly spending limit of £1000.00 would be exceeded"
        request_id:
          type: string
          format: uuid

    Merchant:
      type: object
      properties:
        id:          { type: string, format: uuid }
        name:        { type: string }
        kyb_status:  { type: string, enum: [PENDING_KYB, KYB_APPROVED, ACTIVE, SUSPENDED] }
        status:      { type: string, enum: [PENDING_KYB, KYB_APPROVED, ACTIVE, SUSPENDED] }
        webhook_url: { type: string, format: uri }
        created_at:  { type: string, format: date-time }

    Consent:
      type: object
      properties:
        id:               { type: string, format: uuid }
        merchant_id:      { type: string, format: uuid }
        consumer_id:      { type: string }
        bank_consent_ref: { type: string }
        status:
          type: string
          enum: [PENDING, ACTIVE, REVOKED, EXPIRED]
        max_per_transaction: { $ref: "#/components/schemas/Money" }
        max_per_month:       { $ref: "#/components/schemas/Money" }
        valid_from:  { type: string, format: date-time }
        valid_until: { type: string, format: date-time }
        created_at:  { type: string, format: date-time }

    Payment:
      type: object
      properties:
        id:               { type: string, format: uuid }
        idempotency_key:  { type: string }
        merchant_id:      { type: string, format: uuid }
        consent_id:       { type: string, format: uuid }
        consumer_id:      { type: string }
        amount:           { $ref: "#/components/schemas/Money" }
        status:
          type: string
          enum: [INITIATED, CONSENT_RESERVED, RISK_PASSED, AUTHORISING, SETTLED, FAILED, MANUAL_REVIEW]
        bank_payment_ref: { type: string, example: "FPS-20260806-XYZ999" }
        risk_score:       { type: integer, minimum: 0, maximum: 100 }
        risk_decision:    { type: string, enum: [ALLOW, REVIEW, DECLINE] }
        failure_reason:   { type: string }
        initiated_at:     { type: string, format: date-time }
        settled_at:       { type: string, format: date-time }

  headers:
    X-Request-Id:
      description: Unique request identifier for tracing
      schema: { type: string, format: uuid }
    X-PC-Signature:
      description: HMAC-SHA256 webhook signature
      schema: { type: string, example: "sha256=abc123..." }

  responses:
    Unauthorized:
      description: Missing or invalid JWT
      content:
        application/json:
          schema: { $ref: "#/components/schemas/Error" }
    TooManyRequests:
      description: Rate limit exceeded
      headers:
        Retry-After: { schema: { type: integer } }
      content:
        application/json:
          schema: { $ref: "#/components/schemas/Error" }
    InternalError:
      description: Unexpected server error
      content:
        application/json:
          schema: { $ref: "#/components/schemas/Error" }

paths:
  # ── Auth ──────────────────────────────────────────────────────────────────────
  /auth/token:
    post:
      summary: Exchange API key for JWT
      security: []  # no auth required
      tags: [Auth]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [api_key]
              properties:
                api_key: { type: string }
      responses:
        "200":
          description: JWT token
          content:
            application/json:
              schema:
                type: object
                properties:
                  token:      { type: string }
                  expires_at: { type: string, format: date-time }
        "401": { $ref: "#/components/responses/Unauthorized" }

  # ── Merchants ─────────────────────────────────────────────────────────────────
  /merchants:
    post:
      summary: Register a new merchant
      security: []  # open — KYB happens async after registration
      tags: [Merchants]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name, webhook_url, contact_email]
              properties:
                name:          { type: string, example: "Bet365 UK" }
                webhook_url:   { type: string, format: uri, example: "https://bet365.com/webhooks/vrp" }
                contact_email: { type: string, format: email }
      responses:
        "201":
          description: Merchant registered; api_key shown once — store it securely
          content:
            application/json:
              schema:
                type: object
                properties:
                  merchant: { $ref: "#/components/schemas/Merchant" }
                  api_key:  { type: string, description: "Shown once. Hash and store." }
        "400":
          description: Validation error
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Error" }

  /merchants/{merchant_id}:
    get:
      summary: Get merchant details
      tags: [Merchants]
      parameters:
        - name: merchant_id
          in: path
          required: true
          schema: { type: string, format: uuid }
      responses:
        "200":
          description: Merchant
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Merchant" }
        "404":
          description: Not found

  # ── Consents ──────────────────────────────────────────────────────────────────
  /consents:
    post:
      summary: Create a VRP consent
      tags: [Consents]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [consumer_id, bank_consent_ref, max_per_transaction, max_per_month, valid_until]
              properties:
                consumer_id:
                  type: string
                  description: Merchant's internal user reference (hashed email or UUID)
                bank_consent_ref:
                  type: string
                  description: Consent ID returned by the Open Banking auth flow
                max_per_transaction:
                  $ref: "#/components/schemas/Money"
                max_per_month:
                  $ref: "#/components/schemas/Money"
                valid_until:
                  type: string
                  format: date-time
      responses:
        "201":
          description: Consent created
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Consent" }
    get:
      summary: List consents for a consumer
      tags: [Consents]
      parameters:
        - name: consumer_id
          in: query
          required: true
          schema: { type: string }
        - name: status
          in: query
          schema: { type: string, enum: [PENDING, ACTIVE, REVOKED, EXPIRED] }
        - name: page_size
          in: query
          schema: { type: integer, default: 20, maximum: 100 }
        - name: page_token
          in: query
          schema: { type: string }
      responses:
        "200":
          description: Paginated list
          content:
            application/json:
              schema:
                type: object
                properties:
                  consents:        { type: array, items: { $ref: "#/components/schemas/Consent" } }
                  next_page_token: { type: string }
                  total_count:     { type: integer }

  /consents/{consent_id}:
    get:
      summary: Get consent by ID
      tags: [Consents]
      parameters:
        - name: consent_id
          in: path
          required: true
          schema: { type: string, format: uuid }
      responses:
        "200":
          description: Consent
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Consent" }
    delete:
      summary: Revoke a consent
      tags: [Consents]
      parameters:
        - name: consent_id
          in: path
          required: true
          schema: { type: string, format: uuid }
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                reason: { type: string, enum: [CONSUMER_REQUEST, MERCHANT_REQUEST, FRAUD] }
      responses:
        "200":
          description: Consent revoked
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Consent" }

  # ── Payments ──────────────────────────────────────────────────────────────────
  /payments:
    post:
      summary: Initiate a one-click payment
      description: |
        Idempotent. Include `Idempotency-Key` header on every call.
        If a payment with the same key already exists, returns the existing payment (no duplicate charge).
        Returns 202 if the saga is still in progress (poll /payments/{id}).
      tags: [Payments]
      parameters:
        - name: Idempotency-Key
          in: header
          required: true
          schema: { type: string, maxLength: 128 }
          example: "mer_abc-session-20260806-001"
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [consent_id, amount]
              properties:
                consent_id:
                  type: string
                  format: uuid
                amount:
                  $ref: "#/components/schemas/Money"
                description:
                  type: string
                  example: "Deposit for game session #12345"
                  maxLength: 140
      responses:
        "200":
          description: Payment already exists (idempotent response)
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Payment" }
        "201":
          description: Payment settled
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Payment" }
        "202":
          description: Payment in progress — poll GET /payments/{id}
          headers:
            Location: { schema: { type: string } }
            Retry-After: { schema: { type: integer, example: 2 } }
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Payment" }
        "422":
          description: Business rule violation (limit exceeded, consent revoked, risk declined)
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Error" }
        "429": { $ref: "#/components/responses/TooManyRequests" }
        "500": { $ref: "#/components/responses/InternalError" }

  /payments/{payment_id}:
    get:
      summary: Get payment status
      tags: [Payments]
      parameters:
        - name: payment_id
          in: path
          required: true
          schema: { type: string, format: uuid }
      responses:
        "200":
          description: Payment
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Payment" }

  /payments/{payment_id}/retry:
    post:
      summary: Manually retry a failed payment
      description: Only allowed when payment status is FAILED. Re-runs the saga.
      tags: [Payments]
      parameters:
        - name: payment_id
          in: path
          required: true
          schema: { type: string, format: uuid }
      responses:
        "202":
          description: Retry initiated
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Payment" }
        "409":
          description: Payment is not in FAILED state

  # ── Webhooks ─────────────────────────────────────────────────────────────────
  # (documentation only — merchants receive these, not call them)
  /webhooks/payment-settled:
    post:
      summary: "[Merchant receives] Payment settled notification"
      description: |
        Sent by the Notification Service to merchant's webhook_url on SETTLED.
        Verify signature: HMAC-SHA256(secret, X-PC-Timestamp + "." + body)
      tags: [Webhooks]
      parameters:
        - name: X-PC-Signature
          in: header
          schema: { type: string }
        - name: X-PC-Timestamp
          in: header
          schema: { type: string }
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                event_type: { type: string, example: "payment.settled" }
                payment:    { $ref: "#/components/schemas/Payment" }
                delivered_at: { type: string, format: date-time }
      responses:
        "200":
          description: |
            Merchant must return 2xx to acknowledge.
            Any non-2xx triggers retry with exponential backoff.
```

---

## 5. Kubernetes Manifests

### Directory layout

```
k8s/
├── namespace.yaml
├── configmap.yaml
├── secrets.yaml              ← placeholder; real secrets via External Secrets + Vault
├── services/
│   ├── gateway/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   ├── hpa.yaml
│   │   └── ingress.yaml
│   ├── merchant-svc/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── hpa.yaml
│   ├── consent-svc/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── hpa.yaml
│   ├── payment-svc/          (same structure for remaining services)
│   ├── risk-svc/
│   ├── ledger-svc/
│   ├── bank-adapter/
│   └── notification-svc/
└── infra/
    ├── postgres-statefulset.yaml
    ├── redis-statefulset.yaml
    └── kafka-statefulset.yaml
```

---

### `k8s/namespace.yaml`

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: vrp-demo
  labels:
    app.kubernetes.io/part-of: vrp-demo
```

---

### `k8s/configmap.yaml`

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: vrp-config
  namespace: vrp-demo
data:
  # Service addresses (internal ClusterIP DNS)
  MERCHANT_SVC_ADDR:     "merchant-svc:50051"
  CONSENT_SVC_ADDR:      "consent-svc:50052"
  PAYMENT_SVC_ADDR:      "payment-svc:50053"
  RISK_SVC_ADDR:         "risk-svc:50054"
  LEDGER_SVC_ADDR:       "ledger-svc:50055"
  BANK_ADAPTER_ADDR:     "bank-adapter:50056"

  # Infrastructure
  REDIS_ADDR:            "redis:6379"
  KAFKA_BROKERS:         "kafka:9092"
  JAEGER_ENDPOINT:       "http://jaeger:14268/api/traces"

  # App config
  LOG_LEVEL:             "info"
  ENVIRONMENT:           "production"
  PAYMENT_SAGA_TIMEOUT:  "10s"
  WEBHOOK_RETRY_MAX:     "6"
  CONSENT_CACHE_TTL:     "300s"
  RATE_LIMIT_RPS:        "100"
```

---

### `k8s/secrets.yaml` (placeholder — use External Secrets in real clusters)

```yaml
# In production: use External Secrets Operator pointing to Vault
# https://external-secrets.io
#
# For demo only — base64 encode values: echo -n "value" | base64
apiVersion: v1
kind: Secret
metadata:
  name: vrp-secrets
  namespace: vrp-demo
type: Opaque
data:
  MERCHANT_DB_URL:   cG9zdGdyZXM6Ly91c2VyOnBhc3NAcG9zdGdyZXM6NTQzMi9tZXJjaGFudA==
  CONSENT_DB_URL:    cG9zdGdyZXM6Ly91c2VyOnBhc3NAcG9zdGdyZXM6NTQzMi9jb25zZW50
  PAYMENT_DB_URL:    cG9zdGdyZXM6Ly91c2VyOnBhc3NAcG9zdGdyZXM6NTQzMi9wYXltZW50
  LEDGER_DB_URL:     cG9zdGdyZXM6Ly91c2VyOnBhc3NAcG9zdGdyZXM6NTQzMi9sZWRnZXI=
  JWT_SECRET:        c3VwZXItc2VjcmV0LWp3dC1rZXk=
  HMAC_SECRET:       c3VwZXItc2VjcmV0LWhtYWMta2V5
```

---

### `k8s/services/gateway/deployment.yaml`

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gateway
  namespace: vrp-demo
  labels:
    app: gateway
    app.kubernetes.io/version: "1.0.0"
spec:
  replicas: 2
  selector:
    matchLabels:
      app: gateway
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0        # zero-downtime deployment
  template:
    metadata:
      labels:
        app: gateway
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port:   "9090"
        prometheus.io/path:   "/metrics"
    spec:
      serviceAccountName: vrp-svc-account

      # Graceful shutdown: let in-flight requests complete
      terminationGracePeriodSeconds: 30

      containers:
        - name: gateway
          image: yourregistry/vrp-gateway:latest
          imagePullPolicy: Always
          ports:
            - name: http
              containerPort: 8080
            - name: metrics
              containerPort: 9090

          env:
            # ConfigMap values
            - name: MERCHANT_SVC_ADDR
              valueFrom:
                configMapKeyRef:
                  name: vrp-config
                  key: MERCHANT_SVC_ADDR
            - name: CONSENT_SVC_ADDR
              valueFrom:
                configMapKeyRef:
                  name: vrp-config
                  key: CONSENT_SVC_ADDR
            - name: PAYMENT_SVC_ADDR
              valueFrom:
                configMapKeyRef:
                  name: vrp-config
                  key: PAYMENT_SVC_ADDR
            - name: REDIS_ADDR
              valueFrom:
                configMapKeyRef:
                  name: vrp-config
                  key: REDIS_ADDR
            - name: LOG_LEVEL
              valueFrom:
                configMapKeyRef:
                  name: vrp-config
                  key: LOG_LEVEL
            # Secret values
            - name: JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: vrp-secrets
                  key: JWT_SECRET

          resources:
            requests:
              cpu:    "100m"
              memory: "128Mi"
            limits:
              cpu:    "500m"
              memory: "256Mi"

          readinessProbe:
            httpGet:
              path: /healthz/ready
              port: 8080
            initialDelaySeconds: 5
            periodSeconds:       10
            failureThreshold:    3

          livenessProbe:
            httpGet:
              path: /healthz/live
              port: 8080
            initialDelaySeconds: 10
            periodSeconds:       15
            failureThreshold:    3

          lifecycle:
            preStop:
              exec:
                command: ["/bin/sh", "-c", "sleep 5"]  # drain connections before shutdown

      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              app: gateway
```

---

### `k8s/services/gateway/service.yaml`

```yaml
apiVersion: v1
kind: Service
metadata:
  name: gateway
  namespace: vrp-demo
spec:
  selector:
    app: gateway
  ports:
    - name: http
      port: 80
      targetPort: 8080
  type: ClusterIP   # Ingress handles external traffic
```

---

### `k8s/services/gateway/ingress.yaml`

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: gateway
  namespace: vrp-demo
  annotations:
    kubernetes.io/ingress.class:           "nginx"
    cert-manager.io/cluster-issuer:       "letsencrypt-prod"
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
    nginx.ingress.kubernetes.io/use-regex: "true"
    # Rate limit at ingress level (defence in depth — gateway also rate-limits)
    nginx.ingress.kubernetes.io/limit-rps: "200"
spec:
  tls:
    - hosts:
        - api.vrp-demo.internal
      secretName: vrp-tls-cert
  rules:
    - host: api.vrp-demo.internal
      http:
        paths:
          - path: /v1
            pathType: Prefix
            backend:
              service:
                name: gateway
                port:
                  name: http
          - path: /docs
            pathType: Prefix
            backend:
              service:
                name: gateway
                port:
                  name: http
```

---

### `k8s/services/gateway/hpa.yaml`

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: gateway
  namespace: vrp-demo
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: gateway
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 60      # scale up before CPU saturates
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 70
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 300  # don't scale down too aggressively
      policies:
        - type: Pods
          value: 1
          periodSeconds: 60
    scaleUp:
      stabilizationWindowSeconds: 30
      policies:
        - type: Pods
          value: 2
          periodSeconds: 30
```

---

### `k8s/services/consent-svc/deployment.yaml` (gRPC service pattern)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consent-svc
  namespace: vrp-demo
  labels:
    app: consent-svc
spec:
  replicas: 2
  selector:
    matchLabels:
      app: consent-svc
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
  template:
    metadata:
      labels:
        app: consent-svc
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port:   "9090"
    spec:
      terminationGracePeriodSeconds: 30
      containers:
        - name: consent-svc
          image: yourregistry/vrp-consent-svc:latest
          ports:
            - name: grpc
              containerPort: 50052
            - name: metrics
              containerPort: 9090

          env:
            - name: GRPC_PORT
              value: "50052"
            - name: REDIS_ADDR
              valueFrom:
                configMapKeyRef:
                  name: vrp-config
                  key: REDIS_ADDR
            - name: DB_URL
              valueFrom:
                secretKeyRef:
                  name: vrp-secrets
                  key: CONSENT_DB_URL

          resources:
            requests:
              cpu:    "100m"
              memory: "128Mi"
            limits:
              cpu:    "1000m"   # consent is on critical saga path — more headroom
              memory: "256Mi"

          readinessProbe:
            grpc:
              port: 50052       # uses gRPC health protocol (google.golang.org/grpc/health)
            initialDelaySeconds: 5
            periodSeconds:       10

          livenessProbe:
            grpc:
              port: 50052
            initialDelaySeconds: 10
            periodSeconds:       15

          lifecycle:
            preStop:
              exec:
                command: ["/bin/sh", "-c", "sleep 5"]
```

---

### `k8s/services/consent-svc/service.yaml`

```yaml
apiVersion: v1
kind: Service
metadata:
  name: consent-svc
  namespace: vrp-demo
spec:
  selector:
    app: consent-svc
  ports:
    - name: grpc
      port: 50052
      targetPort: 50052
      protocol: TCP
  type: ClusterIP
```

---

### PodDisruptionBudget (apply to all services)

```yaml
# k8s/services/consent-svc/pdb.yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: consent-svc-pdb
  namespace: vrp-demo
spec:
  minAvailable: 1   # always keep at least 1 pod up during node drains / upgrades
  selector:
    matchLabels:
      app: consent-svc
```

---

### gRPC Health Check (wire into every gRPC service)

```go
// Required for K8s readinessProbe grpc check to work
import (
    "google.golang.org/grpc/health"
    "google.golang.org/grpc/health/grpc_health_v1"
)

s := grpc.NewServer(opts...)
consentv1.RegisterConsentServiceServer(s, handler)

// Health server: Kubernetes calls this for readiness/liveness
healthSrv := health.NewServer()
grpc_health_v1.RegisterHealthServer(s, healthSrv)
healthSrv.SetServingStatus("consent.v1.ConsentService", grpc_health_v1.HealthCheckResponse_SERVING)

// On graceful shutdown:
healthSrv.SetServingStatus("consent.v1.ConsentService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
s.GracefulStop()
```

---

### `k8s/infra/postgres-statefulset.yaml` (demo — use managed DB in prod)

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: vrp-demo
spec:
  serviceName: postgres
  replicas: 1   # demo; use CloudNativePG or RDS in prod
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
        - name: postgres
          image: postgres:16-alpine
          env:
            - name: POSTGRES_USER
              value: vrp
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: vrp-secrets
                  key: POSTGRES_PASSWORD
            - name: POSTGRES_DB
              value: vrp
          ports:
            - containerPort: 5432
          volumeMounts:
            - name: pgdata
              mountPath: /var/lib/postgresql/data
          readinessProbe:
            exec:
              command: ["pg_isready", "-U", "vrp"]
            initialDelaySeconds: 10
            periodSeconds: 5
  volumeClaimTemplates:
    - metadata:
        name: pgdata
      spec:
        accessModes: [ReadWriteOnce]
        resources:
          requests:
            storage: 10Gi
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: vrp-demo
spec:
  selector:
    app: postgres
  ports:
    - port: 5432
  clusterIP: None   # headless — StatefulSet DNS: postgres-0.postgres.vrp-demo.svc.cluster.local
```

---

### Makefile targets for K8s

```makefile
# Makefile

NAMESPACE = vrp-demo
KUBECTL   = kubectl -n $(NAMESPACE)

.PHONY: k8s-apply k8s-rollout k8s-status k8s-logs

k8s-apply:
	kubectl apply -f k8s/namespace.yaml
	kubectl apply -f k8s/configmap.yaml
	kubectl apply -f k8s/secrets.yaml
	kubectl apply -f k8s/infra/
	kubectl apply -f k8s/services/

k8s-rollout:
	$(KUBECTL) rollout restart deployment/gateway
	$(KUBECTL) rollout restart deployment/consent-svc
	$(KUBECTL) rollout restart deployment/payment-svc
	$(KUBECTL) rollout status deployment/gateway

k8s-status:
	$(KUBECTL) get pods,svc,hpa,pdb

k8s-logs:
	$(KUBECTL) logs -l app=$(SVC) --tail=100 -f

proto:
	cd proto && buf generate

migrate:
	for svc in merchant consent payment ledger; do \
	  migrate -path migrations/$$svc -database "$$($${svc^^}_DB_URL)" up; \
	done

lint:
	golangci-lint run ./...

test:
	go test -race -count=1 ./...

test-integration:
	go test -race -count=1 -tags=integration ./...
```

---

## Quick Reference — Tools Summary

| Need | Tool | Command |
|------|------|---------|
| Browse gRPC services in browser | grpcui | `grpcui -plaintext localhost:50051` |
| Call gRPC from terminal | Evans | `evans --proto proto/consent/v1/consent.proto repl` |
| Publish & document protos | buf.build | `buf push` |
| Detect breaking proto changes | buf | `buf breaking --against .git#branch=main` |
| Browse REST API | Swagger UI | `http://localhost:8080/docs` |
| Generate Go from proto | buf generate | `make proto` |
| Apply K8s manifests | kubectl | `make k8s-apply` |
| Scale manually | kubectl | `kubectl scale deployment/gateway --replicas=4` |
| Check HPA status | kubectl | `kubectl get hpa -n vrp-demo` |

# VRP System — Kod Kalitesi & Mimari Raporu

> **Tarih:** 2026-08-14  
> **Revizyon:** 3.0 (öğrenme yol haritası eklendi)
> **Yazar:** Principal Engineer / Architect Review

---

## İçindekiler

1. [Yönetici Özeti](#1-yönetici-özeti)
2. [Sistem Mimarisi — C4 Diyagramları](#2-sistem-mimarisi--c4-diyagramları)
   - 2.1 [Level 1 — System Context](#21-level-1--system-context)
   - 2.2 [Level 2 — Container Diagram](#22-level-2--container-diagram)
   - 2.3 [Level 3 — Component: Payment Service](#23-level-3--component-payment-service)
   - 2.4 [Level 3 — Component: Gateway](#24-level-3--component-gateway)
   - 2.5 [Level 3 — Component: Consent Service](#25-level-3--component-consent-service)
   - 2.6 [Level 4 — Saga Sequence (Payment Flow)](#26-level-4--saga-sequence-payment-flow)
3. [Go Dil Özellikleri Kullanımı](#3-go-dil-özellikleri-kullanımı)
4. [Kod Kalitesi — Servis Bazında Analiz](#4-kod-kalitesi--servis-bazında-analiz)
   - 4.1 [Gateway](#41-gateway)
   - 4.2 [Payment Service](#42-payment-service)
   - 4.3 [Ledger Service](#43-ledger-service)
   - 4.4 [Consent Service](#44-consent-service)
   - 4.5 [Merchant Service](#45-merchant-service)
   - 4.6 [Risk Service](#46-risk-service)
   - 4.7 [Notification Service](#47-notification-service)
   - 4.8 [Bank Adapter](#48-bank-adapter)
5. [Paylaşılan Paketler (pkg/shared)](#5-paylaşılan-paketler-pkgshared)
6. [Altyapı ve DevOps](#6-altyapı-ve-devops)
7. [Test Stratejisi ve Coverage Analizi](#7-test-stratejisi-ve-coverage-analizi)
8. [Güvenlik Analizi](#8-güvenlik-analizi)
9. [Kritik Hatalar (Öncelik Sırası)](#9-kritik-hatalar-öncelik-sırası)
10. [Mimari İyileştirme Önerileri](#10-mimari-iyileştirme-önerileri)
11. [Puanlama Özeti](#11-puanlama-özeti)
12. [Çözüm Önerileri — Kritik Bug Fix Kataloğu](#12-çözüm-önerileri--kritik-bug-fix-kataloğu)
13. [Öğrenme Yol Haritası](#13-öğrenme-yol-haritası--bu-projeyi-yazmak-için-gerekenler)

## 1. Yönetici Özeti

`vrp-system`, Open Banking VRP (Variable Recurring Payments) protokolüne uygun, event-driven bir finansal ödeme platformudur. **8 Go microservice**, gRPC iletişim, Saga orchestration, Transactional Outbox ve SERIALIZABLE çift-giriş muhasebe defteri içermektedir.

### Güçlü Yönler

| Alan | Değerlendirme |
|---|---|
| Genel mimari doğruluk | ✅ Mükemmel — Saga, Outbox, DDD sınırları yerinde |
| Go deyimleri (`slog`, `signal.NotifyContext`, `range`-over-int) | ✅ Modern, tutarlı |
| Domain hata modeli (`domainerr`) | ✅ Temiz, gRPC entegre, test eksiksiz |
| Çift-giriş muhasebe (`ledger-svc`) | ✅ SERIALIZABLE + trigger + idempotency |
| Outbox atomikliği (`payment-svc`) | ✅ Tek transaction'da update+event+outbox |
| Idempotency katmanı | ✅ Redis + DB dedup + `FOR UPDATE` |
| Resilience (`bank-adapter`) | ✅ Circuit breaker + exponential retry |

### Sistemik Sorunlar

| Kategori | Sorun | Risk |
|---|---|---|
| **Build** | `go 1.26.2` versiyonu mevcut değil | 🔴 BLOCKER |
| **Güvenlik** | `k8s/secrets.yaml` plaintext gizli veri | 🔴 BLOCKER |
| **Güvenlik** | `webhook.Verify` replay saldırısına açık | 🔴 HIGH |
| **Dağıtık sistem** | Outbox `FOR UPDATE SKIP LOCKED` yok | 🟠 HIGH |
| **Gözlemlenebilirlik** | gRPC servislerinde `livenessProbe` yok | 🟠 HIGH |
| **Test** | Handler katmanında hiç gRPC testi yok | 🟠 HIGH |
| **API tasarımı** | `FailedPrecondition` mesajlarında iç hata kodu sızıyor | 🟡 MEDIUM |

---

## 2. Sistem Mimarisi — C4 Diyagramları

### 2.1 Level 1 — System Context

```mermaid
C4Context
    title VRP System — System Context

    Person(consumer, "Tüketici", "Open Banking VRP yetkisi veren son kullanıcı")
    Person(merchant, "Merchant", "VRP ödeme başlatan iş ortağı")
    Person(ops, "Platform Ops", "SRE / Platform mühendisi")

    System(vrp, "VRP Platform", "Variable Recurring Payment orchestration — onay yönetimi, ödeme işleme, muhasebe, bildirim")

    System_Ext(bank, "Katılımcı Banka", "PSD2/OBIE uyumlu banka API'si — ödeme rezervasyonu ve transferi")
    System_Ext(webhook_receiver, "Merchant Webhook Endpoint", "Ödeme sonuçlarını alan HTTPS endpoint")
    System_Ext(idp, "Kimlik Sağlayıcı", "OAuth2 / Open Banking consent akışı")

    Rel(merchant, vrp, "API Key ile çağrı", "HTTPS/REST")
    Rel(consumer, idp, "VRP yetkisi verir", "OAuth2")
    Rel(idp, vrp, "Consent token iletir", "HTTPS")
    Rel(vrp, bank, "Ödeme başlatır / durumu sorgular", "HTTPS")
    Rel(vrp, webhook_receiver, "Ödeme sonucunu bildirir", "HTTPS + HMAC-SHA256")
    Rel(ops, vrp, "İzler ve yönetir", "Prometheus / Grafana / kubectl")
```

### 2.2 Level 2 — Container Diagram

```mermaid
C4Container
    title VRP Platform — Container Diyagramı

    Person(merchant, "Merchant")
    Person(consumer, "Consumer")

    System_Boundary(vrp, "VRP Platform") {
        Container(gateway, "API Gateway", "Go / chi", "JWT auth, rate limiting, HTTP→gRPC proxy")

        Container(payment_svc, "Payment Service", "Go / gRPC", "Saga orchestrator — 6 adımlı ödeme akışı, Outbox relay")
        Container(consent_svc, "Consent Service", "Go / gRPC", "VRP yetkisi yaşam döngüsü, rezervasyon, kullanım takibi")
        Container(merchant_svc, "Merchant Service", "Go / gRPC", "Merchant kaydı, API key, webhook config")
        Container(risk_svc, "Risk Service", "Go / gRPC", "Kural tabanlı risk puanlama, blocklist, velocity")
        Container(ledger_svc, "Ledger Service", "Go / gRPC", "Çift-giriş muhasebe, SERIALIZABLE transactions")
        Container(notification_svc, "Notification Service", "Go / Kafka consumer", "Ödeme olayı tüketimi, webhook teslimi, DLQ")
        Container(bank_adapter, "Bank Adapter", "Go / gRPC", "Banka API uyarlayıcı, circuit breaker, mock bank")

        ContainerDb(pg_merchant, "merchant DB", "PostgreSQL", "Merchant, API key hash, webhook config")
        ContainerDb(pg_consent, "consent DB", "PostgreSQL", "Consent, rezervasyon, kullanım")
        ContainerDb(pg_payment, "payment DB", "PostgreSQL", "Payment, event audit log, outbox")
        ContainerDb(pg_ledger, "ledger DB", "PostgreSQL", "Account, journal_entry, journal_line + trigger")

        Container(redis, "Redis", "Redis 7", "Rate limiting, consent cache, risk velocity, idempotency, notif. dedup")
        Container(kafka, "Redpanda/Kafka", "Redpanda", "payment.settled topic + webhook.dlq")
    }

    System_Ext(bank_api, "Katılımcı Banka API", "HTTPS")
    System_Ext(merchant_webhook, "Merchant Webhook", "HTTPS")

    Rel(merchant, gateway, "REST API", "HTTPS + Bearer JWT")
    Rel(consumer, gateway, "Consent yönetimi", "HTTPS")

    Rel(gateway, merchant_svc, "gRPC", ":50051")
    Rel(gateway, consent_svc, "gRPC", ":50052")
    Rel(gateway, payment_svc, "gRPC", ":50053")

    Rel(payment_svc, consent_svc, "ValidateAndReserve / Confirm / Release", "gRPC :50052")
    Rel(payment_svc, risk_svc, "Score", "gRPC :50054")
    Rel(payment_svc, bank_adapter, "Initiate / Reverse / Status", "gRPC :50055")
    Rel(payment_svc, ledger_svc, "PostDoubleEntry / Reverse", "gRPC :50056")
    Rel(payment_svc, kafka, "Outbox events yayınlar", "Kafka producer")

    Rel(notification_svc, kafka, "Tüketir", "Kafka consumer group")
    Rel(notification_svc, merchant_svc, "Webhook config sorgular", "gRPC :50051")
    Rel(notification_svc, merchant_webhook, "Webhook gönderir", "HTTPS + HMAC")

    Rel(bank_adapter, bank_api, "Ödeme API çağrıları", "HTTPS")

    Rel(payment_svc, pg_payment, "", "pgx/v5")
    Rel(consent_svc, pg_consent, "", "pgx/v5")
    Rel(merchant_svc, pg_merchant, "", "pgx/v5")
    Rel(ledger_svc, pg_ledger, "", "pgx/v5")

    Rel(consent_svc, redis, "Cache & lock", "")
    Rel(risk_svc, redis, "Velocity & blocklist", "")
    Rel(notification_svc, redis, "Dedup", "")
    Rel(gateway, redis, "Rate limiting", "")
```

### 2.3 Level 3 — Component: Payment Service

```mermaid
C4Component
    title Payment Service — Component Diyagramı

    Container_Boundary(payment_svc, "Payment Service") {
        Component(handler, "gRPC Handler", "Go struct", "InitiatePayment, GetPayment, RetryPayment — proto↔domain çevirisi")
        Component(orchestrator, "Saga Orchestrator", "Go struct", "6 adımlı saga: Consent→Risk→Bank→Ledger→Settle→Confirm")
        Component(repo, "Payment Repository", "Go struct", "pgx/v5, paymentRepo interface, SettleWithOutbox atomic tx")
        Component(outbox_relay, "Outbox Relay", "Goroutine loop", "200ms poll, Kafka publish, at-least-once, sequential ack")

        Component(consent_client, "Consent gRPC Client", "gen/consent/v1", "ValidateAndReserve / Confirm / Release")
        Component(risk_client, "Risk gRPC Client", "gen/risk/v1", "Score")
        Component(bank_client, "Bank gRPC Client", "gen/bank/v1", "Initiate / Reverse / GetStatus")
        Component(ledger_client, "Ledger gRPC Client", "gen/ledger/v1", "PostDoubleEntry / ReverseEntry")
    }

    ContainerDb(pg, "payment DB", "PostgreSQL")
    Container(kafka, "Redpanda", "Kafka")
    Container(redis_idem, "Redis", "idempotency store")

    Rel(handler, orchestrator, "InitiatePayment çağrısı")
    Rel(handler, repo, "GetByID / GetByIDAndMerchant")
    Rel(orchestrator, repo, "CreatePayment / UpdateAfterXxx / SettleWithOutbox")
    Rel(orchestrator, consent_client, "Saga Step 1, 6")
    Rel(orchestrator, risk_client, "Saga Step 2")
    Rel(orchestrator, bank_client, "Saga Step 3 (+compensation)")
    Rel(orchestrator, ledger_client, "Saga Step 4 (+compensation)")
    Rel(outbox_relay, repo, "ListOutbox / DeleteOutbox")
    Rel(outbox_relay, kafka, "Publish payment.settled")
    Rel(repo, pg, "pgx/v5")
    Rel(orchestrator, redis_idem, "Begin / Complete / Release")
```

### 2.4 Level 3 — Component: Gateway

```mermaid
C4Component
    title API Gateway — Component Diyagramı

    Container_Boundary(gw, "API Gateway") {
        Component(router, "chi Router", "go-chi/chi", "Route tree, middleware zinciri, public/auth grupları")
        Component(auth_mw, "Auth Middleware", "JWT doğrulama", "Bearer token parse, MerchantID context'e ekleme")
        Component(rate_mw, "Rate Limit Middleware", "Redis INCR", "Per-merchant sliding window, nil Redis bypass")
        Component(log_mw, "Logger Middleware", "log/slog", "Request ID, latency, status, merchant_id structured log")
        Component(handlers, "HTTP Handlers", "Go struct", "decode→validate→gRPC→write pattern, 12 endpoint")
        Component(respond, "respond pkg", "Go", "writeJSON, mapGRPCError, decodeJSON+DisallowUnknown")
        Component(convert, "convert pkg", "Go", "proto↔HTTP tip dönüşümleri, nil-safe, pure functions")
        Component(swagger, "Swagger UI", "embedded", "OpenAPI spec const, CDN-pinned Swagger UI")
    }

    Container(redis, "Redis")
    Container(merchant_svc, "Merchant Service", "gRPC")
    Container(consent_svc, "Consent Service", "gRPC")
    Container(payment_svc, "Payment Service", "gRPC")

    Rel(router, auth_mw, "")
    Rel(router, rate_mw, "")
    Rel(router, log_mw, "")
    Rel(router, handlers, "route dispatch")
    Rel(handlers, respond, "writeJSON / mapGRPCError")
    Rel(handlers, convert, "proto↔HTTP")
    Rel(handlers, merchant_svc, "gRPC calls")
    Rel(handlers, consent_svc, "gRPC calls")
    Rel(handlers, payment_svc, "gRPC calls")
    Rel(rate_mw, redis, "INCR / EXPIRE")
    Rel(auth_mw, redis, "—")
```

### 2.5 Level 3 — Component: Consent Service

```mermaid
C4Component
    title Consent Service — Component Diyagramı

    Container_Boundary(consent, "Consent Service") {
        Component(handler_g, "gRPC Handler", "Go struct", "8 RPC, proto↔domain, domainerr.ToGRPC")
        Component(service, "Consent Service", "Go struct", "ValidateAndReserve (tx+lock), UpdateUsage, Cache, Revoke")
        Component(repo, "Consent Repo", "Go struct", "Dynamic WHERE builder, pgx.BeginFunc, FOR UPDATE, ON CONFLICT")
        Component(cache, "Cache Layer", "Redis optional", "Consent read cache, marshal/unmarshal, TTL")
    }

    ContainerDb(pg, "consent DB", "PostgreSQL")
    Container(redis, "Redis (optional)")

    Rel(handler_g, service, "delegates all business logic")
    Rel(service, repo, "CRUD + transactions")
    Rel(service, cache, "get / set / delete")
    Rel(repo, pg, "pgx/v5 pool + tx")
    Rel(cache, redis, "GET / SET / DEL")
```

### 2.6 Level 4 — Saga Sequence (Payment Flow)

```mermaid
sequenceDiagram
    participant GW as Gateway
    participant PS as Payment Service
    participant CS as Consent Service
    participant RS as Risk Service
    participant BA as Bank Adapter
    participant LS as Ledger Service
    participant KF as Kafka (Outbox)
    participant NS as Notification Service
    participant MR as Merchant Webhook

    GW->>PS: InitiatePayment (idempotency_key)
    PS->>PS: idempotency.Begin() [Redis SET NX]
    PS->>PS: repo.CreatePayment() [DB INSERT]

    Note over PS,CS: Step 1 — Consent Reservation
    PS->>CS: ValidateAndReserve(payment_id)
    CS-->>PS: reservation_id ✓

    Note over PS,RS: Step 2 — Risk Scoring
    PS->>RS: Score(payment details)
    RS-->>PS: decision=ALLOW, score=42

    Note over PS,BA: Step 3 — Bank Initiation
    PS->>BA: InitiatePayment()
    BA-->>PS: bank_payment_ref ✓

    Note over PS,LS: Step 4 — Ledger Entry
    PS->>LS: PostDoubleEntry(payment_id, DR/CR)
    LS-->>PS: entry_id ✓

    Note over PS: Step 5 — Settle (Atomic TX)
    PS->>PS: SettleWithOutbox()<br/>UPDATE payment SET status=SETTLED<br/>INSERT payment_event<br/>INSERT outbox

    Note over PS,CS: Step 6 — Confirm Reservation
    PS->>CS: ConfirmReservation()
    CS-->>PS: ✓

    PS->>GW: Payment{status=SETTLED}
    PS->>PS: idempotency.Complete()

    Note over PS,KF: Async Outbox Relay
    PS->>KF: Publish payment.settled event

    KF->>NS: FetchMessage()
    NS->>NS: Redis dedup check
    NS->>PS: GetWebhookConfig (via merchant-svc)
    NS->>MR: POST webhook + HMAC-SHA256

    Note over PS: Compensation (failure paths)
    PS--xBA: Bank reversal
    PS--xCS: Release reservation
    PS--xLS: Reverse ledger entry
```

---

## 3. Go Dil Özellikleri Kullanımı

### Kullanılan Modern Go Özellikleri

| Özellik | Sürüm | Kullanım Yeri | Değerlendirme |
|---|---|---|---|
| `log/slog` structured logging | Go 1.21 | Tüm servisler | ✅ Tutarlı, JSON handler, seviye duyarlı |
| `signal.NotifyContext` | Go 1.16 | gateway, payment-svc, notification-svc | ✅ İdiyomatik graceful shutdown |
| `range` over integer | Go 1.22 | `ledger-svc/store.go:withSerializable` | ✅ Doğru kullanım |
| `net/http` method+path routing | Go 1.22 | `bank-adapter/mockbank/server.go` | ✅ Framework bağımlılığı azaltıyor |
| `math/rand/v2` | Go 1.22 | `bank-adapter/mockbank/server.go` | ✅ Doğru paket kullanımı |
| `errors.As` / `errors.Is` | Go 1.13+ | Tüm servisler | ✅ Tutarlı hata zinciri traversal |
| `errors.Join` | Go 1.20 | Yok | ❌ Compensation hata toplama `errs []error` ile yapılmış |
| Generics | Go 1.18+ | Yok | ❌ Fırsat: `pkg/shared` yardımcıları genericize edilebilir |
| `slices` / `maps` paketi | Go 1.21+ | Yok | ❌ `sort.Slice` yerine kullanılabilirdi |
| `slog.LogAttrs` (düşük alloc) | Go 1.21 | Yok | ⚠️ Hot path'lerde `slog.Attr` kullanılabilir |
| `http.NewRequestWithContext` | Go 1.13+ | `deliver.go` | ✅ Context propagation doğru |

### Kaçırılan Go 1.21+ Fırsatları

```go
// MEVCUT — compensation'da errors biriktirme
var errs []error
if err := s.releaseConsent(...); err != nil {
    errs = append(errs, err)
}
// hata birleştirme için custom fmt.Sprintf

// İYİLEŞTİRME — errors.Join (Go 1.20)
return errors.Join(releaseErr, reverseBankErr)
```

```go
// MEVCUT — saga.go anti-pattern
p.RiskScore = new(scoreResp.GetScore())  // new() bir type alır, expression değil

// DOĞRU
score := scoreResp.GetScore()
p.RiskScore = &score
```

---

## 4. Kod Kalitesi — Servis Bazında Analiz

### 4.1 Gateway

**Genel Puan: 7.5/10**

#### Güçlü Yönler
- Textbook HTTP→gRPC proxy: business logic'in tamamı downstream servislerde
- chi router'da public/auth route split ile `Group + sub-middleware` idiom'u doğru kullanılmış
- `RouterDeps` / `Handlers` struct DI — framework-free, test edilebilir
- Tüm HTTP handler'larda tutarlı `decode→validate→rpc→write` pattern

#### Sorunlar

| Seviye | Dosya | Sorun |
|---|---|---|
| 🔴 | `cmd/main.go` | `jwtSecret` default'u `"super-secret-jwt-key"` — production'da silentle insecure |
| 🟠 | `middleware.go` | Rate limiter `Incr`+`Expire` atomic değil — iki goroutine race'de ikisi de `n==1` görebilir |
| 🟠 | `httpapi/router.go` | chi timeout 30s = server `WriteTimeout` 30s — chi 503 yazamadan transport kopar |
| 🟡 | `respond.go` | `grpcBusinessCode` status message string parsing — downstream format değişirse sessizce bozulur |
| 🟡 | `handlers.go` | `RetryPayment` 202 yanıtında `Location` / `Retry-After` header yok (InitiatePayment'ta var) |
| 🟡 | `swagger.go` | CDN-pinned Swagger UI (`unpkg.com`) — air-gapped ortamda bozulur, SRI hash yok |
| 🟡 | `handlers.go` | `GetMerchant` yetki hatası 403 döner; 404 bilgi sızmasını önler |
| 🟡 | `convert.go` | `default: return s.String()` proto enum ismini HTTP API'ye sızdırır |
| 🔵 | `router_test.go` | Yalnızca 2 test (doc routes) — auth middleware, rate limit, iş handler'ları için hiç test yok |

#### Rate Limiter Race Fix

```go
// MEVCUT (race condition)
n, err := rdb.Incr(ctx, key).Result()
if n == 1 {
    rdb.Expire(ctx, key, 2*time.Second)
}

// DOĞRU — Lua script ile atomik
const luaRateLimit = `
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
return n`
```

---

### 4.2 Payment Service

**Genel Puan: 8.5/10** — En olgun servis

#### Güçlü Yönler
- 6 adımlı Saga orchestration sırası (Consent→Risk→Bank→Ledger→Settle→Confirm) doğru
- Compensation matrisi eksiksiz: her başarısız adım için ilgili rollback denenor
- `compensate()` hataları short-circuit yapmadan toplar — her compensation denenir
- `SettleWithOutbox`: payment UPDATE + event INSERT + outbox INSERT tek transaction'da
- Idempotency doğru: `UniqueViolation → GetByIdempotencyKey → Complete` race'i kapatır

#### Sorunlar

| Seviye | Dosya | Sorun |
|---|---|---|
| 🔴 | `repo.go:ListOutbox` | `FOR UPDATE SKIP LOCKED` eksik — multi-replica'da outbox duplike event üretir |
| 🟠 | `saga.go` | `mapDownstreamErr`: `strings.Contains` ile gRPC status mesajı parse — fragile |
| 🟠 | `handler.go` | `Handler.repo` concrete `*Repo` alıyor — test edilemez; interface çıkartılmalı |
| 🟠 | `outbox.go` | Kafka `RequiredAcks: RequireOne` — finansal topic için `RequireAll` gerekir |
| 🟡 | `saga.go` | `new(scoreResp.GetScore())` — intent belirsiz, anti-pattern |
| 🟡 | `saga_test.go` | `fakeRepo.CreatePayment` domainerr.CodeDuplicateIdempotency dönmüyor — idempotent replay test edilemiyor |
| 🔵 | `repo.go` | `isUniqueViolation` string fallback var; `store.go`'daki temiz `errors.As`-only versiyonla unify edilmeli |

---

### 4.3 Ledger Service

**Genel Puan: 9/10** — Sistem genelinde en yüksek kalite

#### Güçlü Yönler
- `withSerializable`: SERIALIZABLE isolation + 5-attempt retry — doğru concurrent double-entry koruması
- DB-level balance enforcement: `CONSTRAINT TRIGGER trg_journal_balance DEFERRABLE INITIALLY DEFERRED`
- Hem `PostDoubleEntry` hem `ReverseEntry` idempotent ve FOR UPDATE ile korunmuş
- `querier` interface: `*pgxpool.Pool` ve `pgx.Tx` için tek adapter — test edilebilir
- `isUniqueViolation`: yalnızca `errors.As` — referans implementasyon

#### Sorunlar

| Seviye | Dosya | Sorun |
|---|---|---|
| 🔴 | `cmd/main.go` | `signal.NotifyContext` yok — K8s SIGTERM sonrası graceful shutdown imkansız |
| 🟠 | `store.go` | `loadEntryByPaymentID` `pgx.ErrNoRows` sızdırıyor — `domainerr.CodeNotFound` olmalı |
| 🟠 | `store_test.go` | `PostDoubleEntry`, `ReverseEntry`, `GetBalance` için hiç integration test yok — en yüksek risk bileşen |
| 🟡 | `cmd/main.go` | `slog.LevelInfo` hardcoded — `LOG_LEVEL` env var desteklenmiyor |
| 🔵 | `store.go:GetBalance` | `reversed=FALSE` filtresi yok — doğru ama invariant belgelenmemiş |

---

### 4.4 Consent Service

**Genel Puan: 7.5/10**

#### Güçlü Yönler
- `ValidateAndReserve`: transaction + `SELECT FOR UPDATE` + `payment_id` dedup — doğru pessimistic locking
- `ON CONFLICT DO NOTHING` on usage insert — idempotent kullanım kaydı
- Redis cache opsiyonel graceful degradation (Warn, not Fatal)
- 8 RPC hepsinde nil-guard ve exhaustive enum mapping

#### Sorunlar

| Seviye | Dosya | Sorun |
|---|---|---|
| 🟠 | `handler.go` | Soft-expiry: DB=ACTIVE ama ValidUntil geçmiş → proto EXPIRED döner, DB'ye yazılmaz — split-brain |
| 🟠 | `service.go` | `RevokeConsent` reason parametresi ignore ediliyor (`_ string`) — audit trail eksik |
| 🟡 | `handler.go` | `page_token` integer — offset-based pagination impl detail sızması; opaque token olmalı |
| 🟡 | `service.go` | `cacheGet` unmarshal hatasını `err = nil` ile yiyor — silent data corruption |
| 🟡 | `repo.go` | `ListConsents` iki round-trip (COUNT + SELECT); `COUNT(*) OVER()` ile tek query yapılabilir |
| 🔵 | `service_test.go` | Yalnızca validation testleri; nil repo ile çalışıyor — happy path, rezervasyon lifecycle test yok |

---

### 4.5 Merchant Service

**Genel Puan: 7/10**

#### Güçlü Yönler
- Tek `domain.go` olan servis — diğerlerine template
- API key: `vrp_` prefix + 64-hex + bcrypt(cost=10) — doğru güvenlik posture
- HMAC secret 32 random byte — entropi yeterli

#### Sorunlar

| Seviye | Dosya | Sorun |
|---|---|---|
| 🟠 | `repo.go` | bcrypt karşılaştırma repo katmanında — crypto service katmanında olmalı |
| 🟠 | `handler.go` | `toProtoMerchant` `contact_email` alanını atıyor — veri kaybı |
| 🟡 | `service.go` | `SuspendMerchant` reason parametresi ignore ediliyor |
| 🟡 | `domain.go` | `KYBActive="ACTIVE"` ve `StatusActive="ACTIVE"` string collision — farklı domain kavramları aynı değer |
| 🟡 | `repo.go` | `Begin/defer Rollback/Commit` pattern; consent-svc `pgx.BeginFunc` kullanıyor — inconsistency |
| 🔵 | `service_test.go` | bcrypt, webhook, suspend, GetByAPIKey için test yok |

---

### 4.6 Risk Service

**Genel Puan: 8/10**

#### Güçlü Yönler
- `Rule` interface — clean extension point, 5 parametreli dönüş tuple net
- Severity merging DECLINE > REVIEW > ALLOW — doğru öncelik
- VelocityRule `Expire only on n==1` — standart atomic velocity pattern
- `t.Parallel()` + miniredis ile yüksek kaliteli integration testler (370 satır)

#### Sorunlar

| Seviye | Dosya | Sorun |
|---|---|---|
| 🟠 | `handler.go` | `*redis.Client` handler'da — storage concern handler katmanına sızmış |
| 🟡 | `rules.go` | `SISMEMBER risk:blocklist:{type}:{value}` — key=member olan tek-elemanlı set; `SET/GET/EXISTS` daha idiyomatik |
| 🟡 | `cmd/main.go` | `signal.NotifyContext` yok (grpcutil.Serve yakalar ama double-registration riski var) |
| 🔵 | Genelde | Handler katmanı için gRPC RPC testi yok |

---

### 4.7 Notification Service

**Genel Puan: 8/10**

#### Güçlü Yönler
- `FetchMessage → handle → CommitMessages` at-least-once semantics doğru
- Poison message (invalid JSON) skip — infinite retry loop yok
- Redis dedup 24h TTL — idempotent teslim
- DLQ: raw Kafka bytes `json.RawMessage` olarak saklı — replay için doğru
- `postWebhook` body `LimitReader(4096)` ile drainleniyor

#### Sorunlar

| Seviye | Dosya | Sorun |
|---|---|---|
| 🟡 | `deliver.go` | `buildWebhookBody` `map[string]any` — tip güvenliği yok, serialization kontrolsüz |
| 🟡 | `deliver.go` | `PaymentKey()` ID/PaymentID duality — schema evolution artifact, belgelenmiyor |
| 🔵 | `deliver_test.go` | Retry logic, DLQ publish, consumer loop, Redis dedup — hiç test yok |

---

### 4.8 Bank Adapter

**Genel Puan: 8.5/10**

#### Güçlü Yönler
- Circuit breaker (`gobreaker`) + exponential retry (`retry-go`) — resilience stack eksiksiz
- `transientError` custom type — retriable vs non-retriable ayrımı net
- `mapStatus` AUTHORISED/AUTHORIZED iki yazımı da handle ediyor
- Mock bank: configurable fail rate, idempotent reverse, RFC3339Nano fallback parse

#### Sorunlar

| Seviye | Dosya | Sorun |
|---|---|---|
| 🟡 | `adapter.go` | Circuit breaker `ReadyToTrip threshold=10` hardcoded — env var ile ayarlanabilmeli |
| 🟡 | `mockbank/server.go` | Duplicate `payment_id` initiate reject edilmiyor — mock idempotency eksik |
| 🔵 | `mockbank/server_test.go` | 2 test case, 250 satır kod — GET status, reverse, fail rate için test yok |

---

## 5. Paylaşılan Paketler (pkg/shared)

### 5.1 money — Value Object

**Puan: 6.5/10**

```
✅ int64 minor-units (float arithmetic riski yok)
✅ currency normalise: trim+upper before length check
❌ Fields exported → constructor invariant bypass edilebilir
❌ Add/Sub/Equal/SameCurrency operasyonları yok — cross-currency bug riski
❌ ISO 4217 validation yok ("1GB" geçer)
❌ String() negative path var ama New() negative reddeder — dead code
```

**Kritik Fix:**
```go
// Mevcut: exported, bypass edilebilir
type Money struct {
    AmountPence int64
    Currency    string
}

// Öneri: unexport + getters + arithmetic
type Money struct {
    amountPence int64
    currency    string
}

func (m Money) Add(other Money) (Money, error) {
    if m.currency != other.currency {
        return Money{}, fmt.Errorf("currency mismatch: %s != %s", m.currency, other.currency)
    }
    return Money{amountPence: m.amountPence + other.amountPence, currency: m.currency}, nil
}
```

### 5.2 auth — JWT

**Puan: 8/10**

```
✅ Algorithm pinning (alg:none saldırısı engelleniyor)
✅ jti (uuid) replay protection hook
✅ Kapsamlı test: wrong alg, tampered payload, expired
❌ Empty secret silentle kabul edilir
❌ Issuer claim set edilmiyor — multi-service provenance kontrolü yok
❌ NotBefore claim yok
```

### 5.3 domainerr — Hata Modeli

**Puan: 9/10** — En iyi tasarlanmış paylaşılan paket

```
✅ Typed Code (string-backed) — serialize/log aramasına uygun
✅ Unwrap() implemented — errors.Is/As chain çalışır
✅ ToGRPC 14 kodu deterministic map — missing case yok
✅ CodeOf / Is ergonomik helper'lar
❌ FailedPrecondition gRPC mesajına Code string sızdırıyor — client coupling
❌ New/Wrap *Error döner, error değil — Go anti-pattern
❌ CodeOf(nil) → CodeInternal — misleading (belgelenmeli)
```

### 5.4 idempotency — Redis Store

**Puan: 6/10**

```
✅ SET NX atomic claim
✅ context.Done() ile WaitForCompletion
✅ miniredis tests (concurrency + cancel)
🔴 Begin: network error silentle yutulur, Execute falls through
🟠 Complete: iki goroutine aynı key'i Complete edebilir — last-write-wins
🟠 Release: Complete sonrası çağrılırsa result silinir — correctness violation
🟡 WaitForCompletion: time.After per iteration — timer leak (Go < 1.23)
🟡 100ms polling hardcoded — thundering herd riski
```

### 5.5 webhook — HMAC

**Puan: 5.5/10**

```
✅ hmac.Equal constant-time karşılaştırma
✅ Timestamp payload'a bind edilmiş — signing level replay protection
🔴 Verify timestamp freshness check YOK — 30 günlük signature geçer
❌ Empty secret guard yok
❌ mac.Write error discarded (_, _) — Go docs garantisi var ama gürültülü
```

**Kritik Fix:**
```go
// Mevcut
func Verify(secret, signature string, timestamp time.Time, body []byte) bool {
    expected := Sign(secret, timestamp, body)
    return hmac.Equal([]byte(expected), []byte(signature))
}

// Öneri
func VerifyWithTolerance(secret, sig string, ts time.Time, body []byte, maxAge time.Duration) bool {
    if time.Since(ts).Abs() > maxAge {
        return false // replay saldırısı engellendi
    }
    return hmac.Equal([]byte(Sign(secret, ts, body)), []byte(sig))
}
```

---

## 6. Altyapı ve DevOps

### 6.1 Go Versiyon Sorunları

**BLOCKER:** `go 1.26.2` mevcut değil. Tüm go.mod, go.work, Dockerfile ve CI'da geçiyor.

```
go.work          → go 1.26.2
services/*/go.mod → go 1.25.0 veya go 1.26.2 (servis bazında)
Dockerfile       → ARG GO_VERSION=1.26.2
.github/workflows/ci.yml → go-version: '1.26.2'
```

**Fix:** Tümünü `go 1.23.2` (veya mevcut latest) olarak güncelle.

### 6.2 Kubernetes

| Kategori | Bulgu | Risk |
|---|---|---|
| secrets.yaml | Plaintext JWT/HMAC secret commit'li | 🔴 BLOCKER |
| migrate Job | `PGPASSWORD=vrp` literal arg'da | 🔴 HIGH |
| sslmode | `sslmode=disable` tüm DB URL'lerinde | 🔴 HIGH |
| securityContext | Hiçbir container'da yok | 🟠 HIGH |
| livenessProbe | gRPC servislerde yok | 🟠 HIGH |
| PDB | `minAvailable:1` + `replicas:1` → node drain bloklar | 🟡 MEDIUM |
| Postgres | StatefulSet değil Deployment, PVC yok | 🟡 MEDIUM |
| configmap | `BANK_HTTP_BASE_URL: http://127.0.0.1:18080` — localhost, pod arası routing çalışmaz | 🟠 HIGH |

**Eksik securityContext örneği:**
```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]
```

### 6.3 CI/CD

```
✅ dorny/paths-filter@v3 değişiklik tespiti — yalnızca etkilenen servisler build edilir
✅ fail-fast: false — bir servis başarısız olursa diğerleri devam eder
✅ KinD E2E cluster ile gerçek k8s testi
✅ MkDocs deploy pages:write izni doğru kapsamlanmış
❌ permissions: contents: write top-level — sadece write ihtiyacı olan job'lara indirilmeli
❌ Shared paket testleri yalnızca gateway matrix'de çalışıyor — başka servis değişirse atlanıyor
❌ E2E build sequential for-loop — docker buildx bake paralelize edebilir
```

### 6.4 Migrations

```
✅ TIMESTAMPTZ her timestamp alanında
✅ CONSTRAINT TRIGGER trg_journal_balance (muhasebe doğruluğu DB seviyesinde)
✅ Partial index WHERE status='HELD' (aktif rezervasyon sorguları)
✅ key_hash saklama (plaintext API key değil)
❌ İki farklı migration mekanizması: golang-migrate (local) vs raw psql (K8s)
❌ status TEXT kolonu CHECK constraint yok — herhangi string kabul edilir
```

---

## 7. Test Stratejisi ve Coverage Analizi

> Bu bölüm, tüm kaynak ve test dosyalarının fonksiyon-seviyesinde okunmasıyla üretilmiş **ölçülen/hesaplanan coverage tahminidir**. Gerçek `go test -coverprofile` çıktısı değildir; ancak her fonksiyonun test durumuna tek tek bakılarak hesaplanmıştır.

### 7.1 Özet: Ne Kadar Test Eksik?

| Katman | Fonksiyon* | Test Edilen | Coverage (satır) | Eksik Test Senaryosu |
|---|---|---|---|---|
| `pkg/shared` (5 paket) | ~30 | ~28 | **~80%** | ~5 edge case |
| risk-svc | 17 | 11 | **~52%** | ~10 |
| payment-svc | 39 | ~9 (kısmi) | **~14%** | ~40 |
| bank-adapter | 12 | ~2 (kısmi) | **~15%** | ~10 |
| notification-svc | 15 | 1 (kısmi) | **~10%** | ~15 |
| merchant-svc | 20 | 2 (kısmi) | **~9%** | ~20 |
| ledger-svc | 19 | 4 | **~5%** | ~20 |
| gateway | 40 | 2 | **~4%** | ~60 |
| consent-svc | 30 | 1 (kısmi) | **~3%** | ~35 |
| **TOPLAM** | **~222** | **~60** | **~15-20%** | **~215** |

> \* Fonksiyon sayısı exported + unexported method/function toplamıdır; `cmd/main.go` entrypoint'leri hariç.

**Kritik tespit:** Sistemin **finansal doğruluk açısından en riskli** iki bileşeni (`ledger-svc` çift-giriş muhasebe ve `payment-svc` saga orchestration) en düşük coverage'a sahip katmanlar arasında. Bunun tersi olmalıydı.

### 7.2 Servis Bazında Fonksiyon Coverage Tablosu

#### 7.2.1 Gateway — `~4%` (en kötü)

| Dosya | Fonksiyon | Test | Eksik Kritik Senaryo |
|---|---|---|---|
| `swagger.go` | 3 | ✅ %100 | — |
| `router.go` | 1 | ⚠️ %15 | route registration smoke testi yok |
| `respond.go` | 7 | ⚠️ %8 | `mapGRPCError` 9 kod dalı, `isBusinessCode`, `grpcBusinessCode` |
| `handlers.go` | 12 | ❌ %0 | 12 handler'ın tümü; `InitiatePayment` 5 validation + 3 status çıkışı |
| `middleware.go` | 9 | ❌ %0 | `AuthMiddleware` 6 güvenlik dalı, rate limiter 5 dal |
| `convert.go` | 10 | ❌ %0 | pure function — 10 test case ile %100 olur |

#### 7.2.2 Payment Service — `~14%`

| Dosya | Fonksiyon | Test | Durum |
|---|---|---|---|
| `saga.go` | 10 | ⚠️ %55 (dal) | Saga adım branch: **9/22 (%41)**; compensation: **2/7 (%29)**; idempotency: **0/14 branch** |
| `repo.go` | 18 | ❌ %0 | Tüm 18 SQL metodu — fakeRepo kullanılıyor |
| `handler.go` | 7 | ❌ %0 | `InitiatePayment`, `RetryPayment` (SkipIdempotency path) |
| `outbox.go` | 4 | ❌ %0 | `flush` at-least-once delivery, error backoff |

**En kritik eksikler (risk sırası):**
1. Bank rejection / bank gRPC failure → compensation untested
2. `compensateFull` (ledger reversal + bank + consent birlikte) → hiç test yok
3. Idempotent replay → 5 idempotency branch'i untested
4. `RISK_REVIEW` kararı (risk'i geçer ama işaretlenir) → untested
5. `SettleWithOutbox` failure → compensateFull → untested

#### 7.2.3 Ledger Service — `~5%`

| Dosya | Fonksiyon | Test | Durum |
|---|---|---|---|
| `store.go` | 14 | ⚠️ %12 | Sadece 4 converter test ediliyor; `PostDoubleEntry`, `ReverseEntry`, `GetBalance`, `GetJournalEntry`, `withSerializable`, `ensureAccount`, `isUniqueViolation` → hepsi %0 |
| `server.go` | 5 | ❌ %0 | 4 RPC handler + constructor |

**En yüksek riskli bileşen, en ince test katmanı.** `PostDoubleEntry` şu senaryolar için test edilmeli: balanced entry, unbalanced (hata), mixed currency, idempotent double-post, multi-currency rejection.

#### 7.2.4 Consent Service — `~3%`

| Dosya | Fonksiyon | Test | Durum |
|---|---|---|---|
| `service.go` | 13 | ⚠️ %8 | Sadece `CreateConsent` 4/6 validation; `ValidateAndReserve`, `ConfirmReservation`, `ReleaseReservation`, `GetRollingUsage`, cache yardımcıları → %0 |
| `repo.go` | 17 | ❌ %0 | Tümü — `LockConsent` FOR UPDATE, `RollingUsage`, `InsertReservation` |
| `handler.go` | 11 | ❌ %0 | 8 RPC + `moneyFromProto` + `toProtoConsent` soft-expiry + `mapStatus` |

#### 7.2.5 Merchant Service — `~9%`

| Dosya | Fonksiyon | Test | Durum |
|---|---|---|---|
| `service.go` | 7 | ⚠️ %25 | Sadece `RegisterMerchant` validation + `generateAPIKey`; `GetMerchant`, `SuspendMerchant`, `GetMerchantByAPIKey`, `GetWebhookConfig` → %0 |
| `repo.go` | 5 | ❌ %0 | Tümü — `CreateMerchantAndAPIKey` tx rollback en kritik dal |
| `handler.go` | 6 | ❌ %0 | Tümü + `toProtoMerchant` (contact_email kaybı) |
| `domain.go` | N/A | — | Sadece struct/const |

#### 7.2.6 Risk Service — `~52%` (en iyi servis)

| Dosya | Fonksiyon | Test | Durum |
|---|---|---|---|
| `rules.go` | 14 | ✅ %79 | `Engine.Score`, `HighValueRule`, `mergeResults`, `NormalizeBlocklistType`, `DecisionToProto` tam kapsanmış |
| `handler.go` | 3 | ❌ %0 | 3 RPC handler — bufconn + miniredis ile kolayca kapatılır |

Eksik: `VelocityRule` empty-consumerID fast-return, Redis error injection (`mr.SetError`), `scoreInputFromProto` nil-req.

#### 7.2.7 Notification Service — `~10%`

| Dosya | Fonksiyon | Test | Durum |
|---|---|---|---|
| `deliver.go` | ~8 | ⚠️ %15 | Sadece signing round-trip; retry [1s,2s,4s], DLQ publish, 4xx/5xx handle, `buildWebhookBody` → %0 |
| `consumer.go` | ~7 | ❌ %0 | `Run` loop, `handle` poison-message skip, `alreadyDelivered` Redis dedup, `CommitMessages` failure |

#### 7.2.8 Bank Adapter — `~15%`

| Dosya | Fonksiyon | Test | Durum |
|---|---|---|---|
| `mockbank/server.go` | ~8 | ⚠️ %20 | 2 test (happy initiate + invalid JSON); GET status, reverse, idempotent reverse, fail-rate, 503 sim → %0 |
| `adapter.go` | ~4 | ❌ %0 | `mapStatus`, `transientError`, `GetPaymentStatus`, circuit breaker wiring |

### 7.3 Eksik Test Sayısı — Özet

```
Toplam fonksiyon:          ~222
Test edilen:               ~60  (bazıları kısmi)
Hiç test edilmeyen:        ~162
Tahmini eksik test case'i: ~215 senaryo

Katman dağılımı:
  Handler (gRPC) testleri:  0 servis — 8 servisin tamamında eksik
  Repo (SQL) testleri:      0 servis — 8 servisin tamamında eksik
  Saga compensation:        2/7 path — 5 path eksik
  Idempotency replay:       0 test — tüm branch'ler eksik
  Outbox delivery:          0 test — tamamen karanlık
```

### 7.4 Coverage Öncelik Sırası (Yeni Test Yazımı)

| # | Hedef | Neden | Yaklaşım | Tahmini Test |
|---|---|---|---|---|
| 1 | `ledger-svc/store.go` | En yüksek finansal risk | testcontainers-go + gerçek Postgres | ~12 |
| 2 | `payment-svc/saga.go` compensation | Para hareketi geri alma | mock gRPC server + fakeRepo | ~8 |
| 3 | `payment-svc` idempotent replay | En kritik production senaryosu | fakeRepo'ya `CodeDuplicateIdempotency` döndür | ~6 |
| 4 | `gateway/middleware.go` AuthMiddleware | Güvenlik açığı = test açığı | httptest + mock token service | ~6 |
| 5 | Tüm handler gRPC katmanları | 8 serviste %0 | `bufconn` + mock service interface | ~30 |
| 6 | `gateway/respond.go` + `convert.go` | Hata çevirisi pure logic | Table-driven, mock yok | ~25 |
| 7 | `bank-adapter/mockbank` | Resilience stack doğrulama | httptest handler + fail-rate injection | ~8 |
| 8 | `notification-svc` retry/DLQ | Webhook teslim güvenilirliği | httptest + kafka mock | ~10 |
| 9 | Repo SQL katmanları (tüm servis) | 8 serviste %0 | testcontainers-go | ~40 |
| 10 | `consent-svc` reservation lifecycle | Pessimistic lock doğrulama | testcontainers + concurrency | ~10 |

### 7.5 İyi Test Pratikleri (Mevcut)

```
✅ t.Parallel() suite ve subtest seviyesinde tutarlı
✅ testify/require (fatal) ve testify/assert (non-fatal) doğru ayrım
✅ miniredis — gerçek Redis bağımlılığı yok, hızlı ve deterministik
✅ httptest.NewServer — gerçek HTTP sunucu testleri
✅ Table-driven tests yaygın (risk-svc, domainerr, money)
✅ Real in-process gRPC servers (net.Listen :0) — mock framework yok
✅ Fonksiyon-field injection ile DI (fakeRepo pattern)
```

### 7.6 Test Altyapısı İyileştirmeleri

```go
// 1. Handler testi için bufconn — 8 serviste tekrar kullanılabilir helper
// pkg/shared/grpcutil/bufconn_test.go (test helper)
func NewBufconnServer(t *testing.T, register func(*grpc.Server)) (clientConn *grpc.ClientConn) {
    lis := bufconn.Listen(1024 * 1024)
    srv := grpc.NewServer()
    register(srv)
    go srv.Serve(lis)
    t.Cleanup(srv.Stop)
    conn, err := grpc.DialContext(context.Background(), "bufnet",
        grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
            return lis.DialContext(ctx)
        }), grpc.WithTransportCredentials(insecure.NewCredentials()))
    require.NoError(t, err)
    return conn
}

// 2. Repo testi için testcontainers helper
// pkg/shared/db/testdb_test.go
func TestDB(t *testing.T) *pgxpool.Pool {
    // testcontainers-go postgres:16-alpine
    // apply migrations/ledger/000001_init.up.sql
}
```

### 7.7 Coverage Hedefi Önerisi

| Katman | Mevcut | Hedef | Gerekçe |
|---|---|---|---|
| Repo (SQL) | %0 | ≥%80 | Finansal sorgu doğruluğu |
| Handler (gRPC) | %0 | ≥%90 | Validation + error mapping |
| Saga orchestration | %41 (dal) | ≥%90 | Para hareketi branch'leri |
| Domain/service | %8-52 | ≥%85 | Business kural doğruluğu |
| Shared utility | %80 | ≥%95 | Edge case'ler |
| **Sistem geneli** | **~15-20%** | **≥%70** | Production readiness |


---

## 8. Güvenlik Analizi

| ID | Kategori | Bulgu | Etki | Öncelik |
|---|---|---|---|---|
| S-01 | Credential Exposure | `k8s/secrets.yaml` plaintext gizli veri repo'da | Tam credential compromise | 🔴 P0 |
| S-02 | Replay Attack | `webhook.Verify` timestamp freshness yok | Webhook replay saldırısı | 🔴 P0 |
| S-03 | Transport Security | `sslmode=disable` tüm DB bağlantılarında | Man-in-the-middle | 🔴 P0 |
| S-04 | Secret Leak | `WebhookConfig` gRPC response'da `hmac_secret` dönüyor | Internal caller secret erişimi | 🟠 P1 |
| S-05 | Default Credentials | `jwtSecret` default `"super-secret-jwt-key"` | Token forgery | 🟠 P1 |
| S-06 | Missing Validation | `auth.TokenService` empty secret kabul ediyor | Token forgery | 🟠 P1 |
| S-07 | Container Security | K8s pod'larında `securityContext` yok | Container escape kolaylaşır | 🟠 P1 |
| S-08 | Information Leak | `FailedPrecondition` gRPC mesajında iç error code | API client coupling | 🟡 P2 |
| S-09 | Supply Chain | CDN-pinned Swagger UI, SRI hash yok | JS injection | 🟡 P2 |
| S-10 | Authorization | `GetMerchant` 403 dönüyor (404 olmalı) | Resource enumeration | 🟡 P2 |
| S-11 | Idempotency Race | `Release` after `Complete` result siliyor | Double execution | 🟡 P2 |

---

## 9. Kritik Hatalar (Öncelik Sırası)

### P0 — Üretim Engelleyiciler

```
1. go 1.26.2 versiyon direktifi — build tamamen bozulur
2. k8s/secrets.yaml plaintext — credential rotation zorunlu
3. webhook.Verify replay koruması yok — finansal fraud riski
4. sslmode=disable PostgreSQL — plaintext financial data
```

### P1 — Yüksek Risk

```
5. ListOutbox FOR UPDATE SKIP LOCKED eksik — multi-replica outbox duplikasyon
6. ledger-svc signal.NotifyContext yok — K8s SIGTERM graceful shutdown çalışmaz
7. Kafka RequireOne → RequireAll — finansal event kaybı riski (leader failover)
8. idempotency.Begin error swallow — Redis failure silentle ignored
9. BANK_HTTP_BASE_URL localhost in configmap — bank-adapter K8s'de çalışmaz
10. Container securityContext yok — PSA restricted profil reddeder
```

### P2 — Orta Risk

```
11. Handler *Repo concrete (payment-svc) — unit test edilemiyor
12. mapDownstreamErr strings.Contains — format değişirse sessizce bozulur
13. Consent handler soft-expiry split-brain
14. money.Money exported fields — invariant bypass
15. grpcutil.Serve double signal registration
```

---

## 10. Mimari İyileştirme Önerileri

### 10.1 gRPC Error Details — FailedPrecondition Sızması

**Problem:** Gateway `respond.go` ve saga `mapDownstreamErr` gRPC status message'ı string parse ediyor.

**Öneri:** `google.rpc.ErrorInfo` veya `errdetails.PreconditionFailure` kullan:

```proto
// proto/common/v1/common.proto'ya ekle
import "google/rpc/error_details.proto";
```

```go
// domainerr/errors.go
import "google.golang.org/genproto/googleapis/rpc/errdetails"

func ToGRPC(err error) error {
    // ... mevcut kod ...
    st, _ := status.New(codes.FailedPrecondition, de.Message).
        WithDetails(&errdetails.ErrorInfo{
            Reason: string(de.Code),
            Domain: "vrp.platform",
        })
    return st.Err()
}
```

### 10.2 Outbox FOR UPDATE SKIP LOCKED

```sql
-- repo.go:ListOutbox
SELECT id, topic, key, payload, created_at
FROM outbox
ORDER BY created_at ASC
LIMIT $1
FOR UPDATE SKIP LOCKED;  -- multi-replica safety
```

### 10.3 Secrets Management

```
Geçiş Yolu:
1. k8s/secrets.yaml'ı .gitignore'a ekle
2. External Secrets Operator + Vault veya AWS Secrets Manager
3. SealedSecrets (kubeseal) — en hızlı geçiş
```

```yaml
# Öneri: SealedSecret
apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: vrp-secrets
spec:
  encryptedData:
    JWT_SECRET: AgB...
    HMAC_SECRET: AgC...
```

### 10.4 Webhook Replay Koruması

```go
// pkg/shared/webhook/hmac.go
const DefaultMaxAge = 5 * time.Minute

func VerifyWithTolerance(secret, sig string, ts time.Time, body []byte, maxAge time.Duration) bool {
    if time.Since(ts).Abs() > maxAge {
        return false
    }
    return hmac.Equal([]byte(Sign(secret, ts, body)), []byte(sig))
}
```

### 10.5 Money Value Object Güçlendirme

```go
// pkg/shared/money/money.go
type Money struct {
    amountPence int64  // unexported
    currency    string // unexported — ISO 4217 validated
}

func (m Money) Add(other Money) (Money, error) {
    if !m.SameCurrency(other) {
        return Money{}, fmt.Errorf("money: cannot add %s to %s", m.currency, other.currency)
    }
    return Money{amountPence: m.amountPence + other.amountPence, currency: m.currency}, nil
}

func (m Money) Equal(other Money) bool {
    return m.amountPence == other.amountPence && m.currency == other.currency
}
```

### 10.6 Idempotency Store Güçlendirme

```go
// Complete: yalnızca PROCESSING durumundaysa yaz
const luaComplete = `
local current = redis.call('GET', KEYS[1])
if current == 'PROCESSING' then
    return redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
end
return current`

// Release: yalnızca PROCESSING durumundaysa sil
const luaRelease = `
local current = redis.call('GET', KEYS[1])
if current == 'PROCESSING' then
    return redis.call('DEL', KEYS[1])
end
return 0`
```

### 10.7 Ledger Service Integration Testleri

```go
// store_test.go — testcontainers-go ile
func TestPostDoubleEntry_Idempotent(t *testing.T) {
    ctx := context.Background()
    pool := testDB(t) // testcontainers postgres

    store := NewStore(pool)

    // İlk post
    e1, err := store.PostDoubleEntry(ctx, PostRequest{...})
    require.NoError(t, err)

    // Aynı paymentID ile ikinci post — idempotent olmalı
    e2, err := store.PostDoubleEntry(ctx, PostRequest{...})
    require.NoError(t, err)
    assert.Equal(t, e1.ID, e2.ID) // aynı entry dönmeli
}
```

### 10.8 Mimari: Async Confirmation Pattern

**Problem:** Saga Step 6 (`ConfirmReservation`) başarısız olursa hata loglanıp geçiliyor — consent-svc'de orphaned rezervasyon kalıyor.

**Öneri:** Outbox'a `confirmation_retry` event ekle:

```
payment.settled event → notification-svc'ye ek olarak
                      → consent-svc retry queue'ya ConfirmReservation retry
```

### 10.9 grpcutil.Serve Signal Handling

```go
// MEVCUT — double registration riski
func (s *Server) Serve(ctx context.Context) error {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM) // library signal ownership: YANLIŞ
    // ...
}

// ÖNERI — yalnızca ctx.Done() dinle
func (s *Server) Serve(ctx context.Context) error {
    go func() {
        <-ctx.Done()
        s.gs.GracefulStop()
    }()
    return s.gs.Serve(s.lis)
}
// Her main.go signal.NotifyContext(ctx, ...) ile ctx üretir — single owner
```

### 10.10 Gözlemlenebilirlik Yol Haritası

```
Mevcut:
✅ slog JSON structured logging
✅ Prometheus metrics (docker-compose + k8s configmap'te endpoint var)
✅ Jaeger distributed tracing (docker-compose'da)
✅ OpenAPI spec endpoint

Eksik:
❌ pgx QueryTracer → OTEL trace'e DB sorguları
❌ gRPC metrics interceptor (grpc-prometheus)
❌ Business metrics (payment.saga.step.duration, saga.compensation.total)
❌ SLO/SLI tanımları
❌ liveness probe gRPC servisleri için
```

---

## 11. Puanlama Özeti

### Servis Bazında Kalite Puanı

| Servis | Mimari | Go Idioms | Hata İşleme | Test | Güvenlik | **Genel** |
|---|---|---|---|---|---|---|
| Gateway | 9 | 8 | 7 | 3 | 6 | **7.5** |
| Payment Service | 9 | 8.5 | 8 | 7 | 8 | **8.5** |
| Ledger Service | 9.5 | 9 | 8 | 5 | 8 | **9.0** |
| Consent Service | 8 | 8 | 7 | 4 | 7 | **7.5** |
| Merchant Service | 7.5 | 7.5 | 8 | 4 | 7 | **7.0** |
| Risk Service | 8.5 | 8.5 | 8.5 | 8 | 7 | **8.0** |
| Notification Service | 8 | 8 | 8 | 5 | 7 | **8.0** |
| Bank Adapter | 9 | 8.5 | 8.5 | 5 | 8 | **8.5** |
| **pkg/shared** | 8.5 | 8 | 7.5 | 7.5 | 6 | **7.5** |
| **Infra/DevOps** | 7.5 | — | — | 7 | 4 | **6.5** |

### **Sistem Genel Puanı: 7.9 / 10**

### Neden Yüksek Puan

Bu sistem, birçok senior mühendislik ekibinin kaçındığı doğru tasarım kararlarını hayata geçirmiş:

1. **Saga pattern ile Outbox atomikliği** — finansal tutarlılık için doğru çözüm
2. **SERIALIZABLE + DB trigger** ile muhasebe bütünlüğü — uygulama katmanı guarantee yetmez
3. **domainerr typed errors** — gRPC boundary'de temiz error translation
4. **Idempotency everywhere** — consent reservation, ledger, outbox, notification — katmanlı koruma
5. **Circuit breaker + retry** — bank adapter'da production-grade resilience

### Neden Tam Puan Değil

1. Go versiyon direktifleri hayali — `1.26.2` mevcut değil
2. K8s secrets plaintext — production'da kullanılamaz
3. Webhook replay koruması eksik — finansal güvenlik açığı
4. Outbox `SKIP LOCKED` eksik — multi-replica'da veri duplikasyonu
5. Test kapsamı ledger ve handler katmanlarında kritik boşluklar içeriyor

---

## 12. Çözüm Önerileri — Kritik Bug Fix Kataloğu

Bu bölüm, Bölüm 9'daki her kritik bulgu için **doğrudan uygulanabilir kod çözümü** içerir. Her fix, değiştirilecek dosya ve exact diff yaklaşımıyla verilmiştir.

### 12.1 Go Versiyon Direktifi (P0 — Blocker)

```bash
# go.work, services/*/go.mod, pkg/shared/go.mod, gen/go.mod
# Dockerfile (ARG GO_VERSION), .github/workflows/ci.yml (go-version)
sed -i '' 's/go 1\.26\.2/go 1.23.2/g; s/go 1\.25\.0/go 1.23.2/g' \
    go.work services/*/go.mod pkg/shared/go.mod gen/go.mod
```

> Doğru versiyon için `go version` çıktısına bak; `1.23.x` en güvenli sabit nokta. Dockerfile ve CI'daki `GO_VERSION` / `go-version` değerleri ayrıca elle güncellenmeli.

### 12.2 PostgreSQL TLS (P0 — `sslmode=disable`)

```yaml
# k8s/configmap.yaml — tüm DSN'lerde
# MEVCUT
DATABASE_URL: "postgres://vrp:vrp@postgres:5432/payment?sslmode=disable"

# ÖNERİ — production
DATABASE_URL: "postgres://vrp:vrp@postgres:5432/payment?sslmode=verify-full&sslrootcert=/etc/ssl/certs/ca.crt"
```

```go
// pkg/shared/db/pgx.go — dev/prod ayrımı
sslmode := config.Get("DB_SSLMODE", "require") // default güvenli
// local docker-compose'ta "disable", production'da "verify-full"
```

### 12.3 Rate Limiter Atomicity (P1 — Race Condition)

```go
// services/gateway/internal/httpapi/middleware.go
// MEVCUT (race): Incr + conditional Expire iki ayrı komut
n, _ := rdb.Incr(ctx, key).Result()
if n == 1 { rdb.Expire(ctx, key, 2*time.Second) }

// FIX: Lua script — key oluşturma + TTL atomic
var rateLimitScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
return n
`)

func (m *rateLimiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, error) {
    n, err := rateLimitScript.Run(ctx, m.rdb, []string{key}, int(window.Seconds())).Int64()
    if err != nil { return true, err } // Redis hata → fail-open (mevcut davranış)
    return n <= limit, nil
}
```

### 12.4 `new(expr)` Antipattern (P2)

```go
// services/payment-svc/internal/saga.go
// MEVCUT — new() bir type alır, expression değil
p.RiskScore = new(scoreResp.GetScore())

// FIX
score := scoreResp.GetScore()
p.RiskScore = &score
```

### 12.5 `mapDownstreamErr` String Parsing (P2)

```go
// services/payment-svc/internal/saga.go
// MEVCUT — strings.Contains ile gRPC status message parse
func mapDownstreamErr(err error) error {
    upper := strings.ToUpper(err.Error())
    switch {
    case strings.Contains(upper, string(domainerr.CodeConsentLimitExceeded)):
        return domainerr.New(domainerr.CodeConsentLimitExceeded, ...)
    }
}

// FIX — domainerr.CodeOf zaten errors.As chain'ini çözer
func mapDownstreamErr(err error) error {
    if code := domainerr.CodeOf(err); code != domainerr.CodeInternal {
        return domainerr.New(code, domainerr.MessageOf(err))
    }
    return domainerr.New(domainerr.CodeInternal, "downstream failure")
}
// Ön koşul: downstream servislerin domainerr kodlarını gRPC ErrorInfo detail'de
// taşıması (Bölüm 10.1). Bu fix, string parsing'i tamamen ortadan kaldırır.
```

### 12.6 Handler Concrete `*Repo` → Interface (P2)

```go
// services/payment-svc/internal/handler.go
// MEVCUT — Handler concrete *Repo alır, test edilemez
type Handler struct {
    orch *Orchestrator
    repo *Repo
}

// FIX — minimal interface extraction
type paymentReader interface {
    GetByID(ctx context.Context, id uuid.UUID) (*Payment, error)
    GetByIDAndMerchant(ctx context.Context, id, merchantID uuid.UUID) (*Payment, error)
}

type Handler struct {
    orch *Orchestrator
    repo paymentReader // interface — fake ile test edilebilir
}
```

### 12.7 JWT Default Secret (P1)

```go
// services/gateway/cmd/main.go
// MEVCUT — hardcoded insecure default
jwtSecret := config.Get("JWT_SECRET", "super-secret-jwt-key")

// FIX — production'da zorunlu, dev'de uyarı
jwtSecret := os.Getenv("JWT_SECRET")
if jwtSecret == "" {
    if os.Getenv("APP_ENV") == "production" {
        slog.Error("JWT_SECRET is required in production")
        os.Exit(1)
    }
    slog.Warn("JWT_SECRET unset; using insecure dev default")
    jwtSecret = "dev-only-secret"
}
```

### 12.8 Consent Soft-Expiry Split-Brain (P2)

```go
// services/consent-svc/internal/handler.go
// MEVCUT — DB=ACTIVE ama ValidUntil geçti → proto EXPIRED (DB/API ayrışır)
func toProtoConsent(c domain.Consent) *consentv1.Consent {
    if c.Status == domain.StatusActive && time.Now().After(c.ValidUntil) {
        c.Status = domain.StatusExpired // sadece bellekte, DB'ye yazılmaz
    }
    ...
}

// FIX — iki seçenek:
// (a) Lazy-expire'ı DB'ye de yansıt (service katmanında UPDATE)
func (s *Service) GetConsent(ctx, id) (*domain.Consent, error) {
    c, err := s.repo.GetConsent(ctx, id)
    if c.Status == domain.StatusActive && time.Now().After(c.ValidUntil) {
        if err := s.repo.MarkExpired(ctx, id); err == nil { // fire-and-forget değil
            c.Status = domain.StatusExpired
        }
    }
    return c, err
}

// (b) Status'u derived etme — DB'de saklı tutma
//     Proto'ya yazarken "effective status" hesapla ama bunu açıkça isimlendir
func effectiveStatus(c domain.Consent) domain.Status {
    if c.Status == domain.StatusActive && time.Now().After(c.ValidUntil) {
        return domain.StatusExpired
    }
    return c.Status
}
```

### 12.9 Kafka `RequireOne` → `RequireAll` (P1)

```go
// services/payment-svc/internal/outbox.go
// MEVCUT — tek ack, leader failover'da event kaybı
w := &kafka.Writer{
    RequiredAcks: kafka.RequireOne,
}

// FIX — finansal event için tam çoğaltma ack'i
w := &kafka.Writer{
    RequiredAcks: kafka.RequireAll, // replication factor ≥ 2 olmalı
}
// Ön koşul: Kafka topic replication-factor=3 (production)
```

### 12.10 `signal.NotifyContext` Eksik (ledger-svc, risk-svc)

```go
// services/ledger-svc/cmd/main.go + services/risk-svc/cmd/main.go
// MEVCUT
ctx := context.Background()
if err := srv.Serve(ctx); err != nil { ... }

// FIX — payment-svc pattern'iyle aynı
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
if err := srv.Serve(ctx); err != nil && err != grpc.ErrServerStopped { ... }
```

### 12.11 `BANK_HTTP_BASE_URL` Localhost (P1)

```yaml
# k8s/configmap.yaml
# MEVCUT — pod içinde 127.0.0.1 kendi pod'una işaret eder, bank-adapter'a değil
BANK_HTTP_BASE_URL: "http://127.0.0.1:18080"

# FIX — service DNS adı
BANK_HTTP_BASE_URL: "http://bank-adapter:8080"
```

### 12.12 `securityContext` (P1)

```yaml
# Tüm k8s/services/*/all.yaml Deployment spec'lerine ekle
spec:
  template:
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532          # distroless nonroot UID
        readOnlyRootFilesystem: true
        allowPrivilegeEscalation: false
        capabilities:
          drop: ["ALL"]
        seccompProfile:
          type: RuntimeDefault
```

### 12.13 Uygulama Sırası (Roadmap)

```
Hafta 1 (P0 — build ve güvenlik):
  12.1 go version → 12.2 sslmode → 12.7 JWT secret → 12.11 configmap → 12.12 securityContext

Hafta 2 (P1 — dağıtık sistem doğruluğu):
  12.3 rate limiter → 12.9 RequireAll → 12.10 signal → 10.2 outbox SKIP LOCKED

Hafta 3 (P2 — kod kalitesi):
  12.4 new(expr) → 12.5 mapDownstreamErr → 12.6 interface → 12.8 split-brain
  → 10.4 webhook replay → 10.6 idempotency Lua

Hafta 4+ (Test kapatma — Bölüm 7.4 sırası):
  ledger store → saga compensation → handler gRPC → repo SQL
```

---

## 13. Öğrenme Yol Haritası — Bu Projeyi Yazmak İçin Gerekenler

Bu bölüm, `vrp-system` benzeri bir finansal mikroservis platformunu sıfırdan yazabilmek için gereken Go konularını, teknoloji ve tasarım kalıplarını öğrenme sırasına göre listeler. Her başlık, projede gerçekten kullanıldığı dosya ile eşleştirilmiştir.

### 13.1 Go Dil Konuları

#### Zorunlu Temel

| Konu | Projede Nerede |
|---|---|
| Struct, interface, method set | `pkg/shared/money`, `domainerr` |
| Pointer vs value receiver | `Money` (value), `*Repo` (pointer) |
| Interface design (küçük interface, "accept interfaces return structs") | `paymentRepo` (saga.go), `querier` (ledger store) |
| `error` handling: `errors.Is`, `errors.As`, `%w` | `domainerr.Is`, `isUniqueViolation` |
| `errors.Join` (Go 1.20) | compensation hata birleştirme |
| `context.Context` propagation | Tüm gRPC/handler/repo zinciri |
| `defer` kaynak yönetimi | `defer pool.Close()`, `defer cancel()` |
| Table-driven tests | `rules_test.go`, `errors_test.go` |

#### Concurrency (Go'nun Asıl Zorluğu)

| Konu | Projede Nerede |
|---|---|
| Goroutine yaşam döngüsü | Outbox relay (`outbox.go`) |
| `sync.RWMutex` | `mockbank/server.go` (payment map) |
| `sync.WaitGroup` / `errgroup` | Graceful shutdown |
| Channel ownership | Notification consumer (single goroutine) |
| `select` + `ctx.Done()` | `WaitForCompletion` polling |
| Race condition farkındalığı | Rate limiter `INCR+EXPIRE` race (Bölüm 12.3) |
| `time.Ticker` vs `time.After` (timer leak) | Idempotency polling |
| Data race detection (`-race`) | Test çalıştırma |

#### Modern Go (1.21+)

```
log/slog            → structured logging (tüm servisler)
signal.NotifyContext → graceful shutdown (payment-svc, gateway)
range over int      → withSerializable retry loop (Go 1.22)
net/http routing    → mockbank (Go 1.22, framework'siz)
math/rand/v2        → mockbank fail-rate
generics            → fırsat var (pkg/shared helper'ları genericize)
```

### 13.2 Tasarım Kalıpları

#### Dağıtık Veri Kalıpları

| Kalıp | Amaç | Projede Nerede |
|---|---|---|
| **Saga Orchestration** | Dağıtık transaction, lokal rollback'lerle | `payment-svc/internal/saga.go` (6 adım + compensation) |
| **Transactional Outbox** | DB commit + event publish atomikliği (dual-write çözümü) | `SettleWithOutbox` + `outbox.go` relay |
| **Idempotency Key** | Duplicate istek koruması | Redis `SET NX` + DB `payment_id` dedup |
| **Idempotent Consumer** | Kafka en az-bir-kez teslim | `notification-svc` Redis dedup (24h TTL) |
| **Dead Letter Queue (DLQ)** | Poison mesaj izolasyonu | `webhook.dlq` topic, `json.RawMessage` replay |
| **Circuit Breaker** | Kademeli hata yayılmasını kesme | `bank-adapter` gobreaker (10 failure → open) |
| **Retry + Backoff** | Geçici hataları yeniden dene | retry-go (3 deneme, exponential) |
| **Pessimistic Locking** | Rezervasyon serializasyonu | `SELECT FOR UPDATE` (consent, ledger) |
| **Optimistic Concurrency** | CAS | `ResetForRetry` WHERE id+status, `ON CONFLICT DO NOTHING` |
| **At-least-once + dedup** | Teslim güvenilirliği | Outbox + Redis dedup |

#### Mimari / Kod Kalıpları

| Kalıp | Amaç | Projede Nerede |
|---|---|---|
| **Handler → Service → Repo** | Katmanlı soyutlama | Tüm servislerde tutarlı |
| **Repository pattern** | DB erişimini izole et | `repo.go` (her servis) |
| **Value Object** | Tip güvenliği + invariant | `money.Money` (int64 pence) |
| **Domain Error model** | Business error'ları tipte taşı | `domainerr` (Code enum + ToGRPC) |
| **API Gateway / BFF** | Tek giriş noktası, protokol çevirisi | `gateway` (HTTP→gRPC proxy) |
| **Dependency Injection** | Test edilebilirlik | Struct-based DI (framework yok) |
| **Interceptor Chain** | Cross-cutting concern | `grpc.ChainUnaryInterceptor` |
| **Middleware Chain** | HTTP katmanında | chi (RequestID → Logger → Recoverer → Auth → RateLimit) |
| **Double-entry accounting** | Muhasebe invariant'ı | `ledger-svc` (DR/CR + balance trigger) |

#### DDD (Domain-Driven Design)

```
Bounded Context   → her microservice bir domain sınırı (merchant, consent, payment, ledger)
Ubiquitous Language → "ValidateAndReserve", "ConfirmReservation", "ReleaseReservation"
Aggregate         → Payment (saga root), Consent (reservation lifecycle)
Domain Event      → payment.settled (event audit log + Kafka)
Value Object      → Money, WebhookConfig
```

### 13.3 Dağıtık Sistem Konuları

#### Tutarlılık (En Kritik)

| Konu | Neden |
|---|---|
| **ACID vs eventual consistency** | Saga = eventual, ledger = strong (SERIALIZABLE) — ayırt etmek |
| **Saga pattern** (orchestration vs choreography) | Proje orchestration kullanıyor |
| **Compensation** | Geri alınabilir vs geri alınamaz işlemler |
| **Dual-write problem** | Outbox pattern'in varoluş nedeni |
| **Exactly-once vs at-least-once** | Kafka'da tam exactly-once yok — idempotency + dedup ile |
| **Isolation seviyeleri** | SERIALIZABLE + retry (ledger), READ COMMITTED (çoğu) |
| **Serialization failure retry** | `withSerializable` 5-deneme (40001/40P01) |

#### Güvenilirlik

| Konu | Projede |
|---|---|
| **Graceful shutdown** | `signal.NotifyContext` + drain (in-flight RPC bitir) |
| **Health checks** | Readiness vs liveness ayrımı |
| **Backpressure** | Kafka consumer `CommitMessages` |
| **Rate limiting** | Per-merchant fixed window (Redis) |
| **Bulkhead** | Servis başına izole havuz (pgxpool) |
| **Timeout her katmanda** | HTTP server timeout, gRPC dial timeout, DB ping deadline |

#### Mesajlaşma / Event

| Konu | Neden |
|---|---|
| **Event-driven architecture** | Payment → event → notification |
| **Producer/Consumer ack modeli** | `RequiredAcks`, `CommitMessages` |
| **Partition key seçimi** | DLQ'da MerchantID partition key |
| **Consumer group** | notification-service group |
| **Poison message handling** | Invalid JSON skip + log |

#### Gözlemlenebilirlik

```
Structured logging → slog (JSON handler, seviye, correlation ID)
Metrics            → Prometheus (RED metodu: Rate/Error/Duration)
Tracing            → Jaeger / OpenTelemetry (distributed trace)
SLI/SLO            → error budget, availability hedefi
```

### 13.4 Teknoloji Stack

| Katman | Teknoloji | Öğrenme Kaynağı |
|---|---|---|
| **RPC** | gRPC + protobuf + **buf** (lint + breaking change) | buf.build docs |
| **DB** | PostgreSQL + **pgx/v5** (pool, tx, SERIALIZABLE) | pgx README |
| **Migration** | golang-migrate | github docs |
| **Cache/Lock** | Redis (go-redis/v9) | |
| **Message** | Kafka / Redpanda (kafka-go) | |
| **HTTP** | chi router | |
| **Auth** | golang-jwt/v5 | |
| **Resilience** | gobreaker, retry-go | |
| **Test** | testify, miniredis, testcontainers-go | |
| **Container** | Docker (multi-stage, distroless) | |
| **Orchestration** | Kubernetes (Kustomize, kind, HPA, PDB) | |
| **CI/CD** | GitHub Actions | |
| **Observability** | Prometheus, Grafana, Jaeger | |
| **Docs** | MkDocs Material | |

### 13.5 Öğrenme Yol Haritası (Sıralı)

```
AŞAMA 1 — Go Temelleri (2-3 ay)
├─ Effective Go + "100 Go Mistakes" kitabı
├─ Interface design (küçük interface'ler)
├─ Concurrency: goroutine, channel, mutex, select
├─ error handling: %w, errors.Is/As/Join
└─ Testing: table-driven, testify, mocks vs fakes

AŞAMA 2 — Kalıplar + Web/RPC (2 ay)
├─ Repository + Service + Handler katmanları
├─ Value object, domain error model
├─ gRPC + protobuf + buf (bu projede bol örnek var)
├─ Middleware/interceptor chain
└─ Dependency injection (framework'süz struct-based)

AŞAMA 3 — Dağıtık Sistemler (3-4 ay) ← EN ZOR
├─ Saga pattern (bu projenin saga.go'su mükemmel çalışma)
├─ Transactional Outbox (dual-write problemi)
├─ Idempotency (key + consumer + dedup)
├─ Circuit breaker + retry + timeout
├─ Eventual consistency vs ACID
├─ "Designing Data-Intensive Applications" (Kleppmann) — zorunlu okuma
└─ "Patterns of Enterprise Application Architecture" (Fowler)

AŞAMA 4 — Operasyon (2 ay)
├─ Kubernetes (Deployment, Service, HPA, PDB, probes)
├─ Observability (Prometheus + tracing)
├─ Graceful shutdown (signal + drain)
├─ CI/CD pipeline
└─ Docker multi-stage + distroless

AŞAMA 5 — Derinleşme (sürekli)
├─ DDD bounded contexts (Evans)
├─ SERIALIZABLE isolation + retry (PostgreSQL concurrency)
├─ Kafka exactly-once sınırları
├─ Performance: pprof, allocation, escape analysis
└─ Bu projeyi refactor ederek öğren (Bölüm 12'deki fix'leri uygula)
```

### 13.6 En Kritik 5 Şey (Kısaltılmış)

1. **Concurrency'yi gerçekten anlamak** — goroutine değil, *hangi veri hangi goroutine'de*, race detection
2. **Saga + Outbox + Idempotency üçlüsü** — dağıtık tutarlılığın kutsal üçgeni
3. **Interface'leri küçük tutmak** — `paymentRepo` gibi 5 metotluk interface, test edilebilirlik
4. **Hata işlemede `errors.Is/As` zinciri** — `domainerr` gibi tip-güvenli model
5. **"Accept interfaces, return structs"** — Go'nun DI felsefesi

---

> **Pratik tavsiye:** Bu proje, bu konuların neredeyse tamamını içeren nadir bir çalışma örneği. Bölüm 12'deki fix'leri uygulayarak başlamak — özellikle saga compensation testleri ve ledger integration testleri — öğrenmenin en hızlı yolu olur.



---

*Rapor otomatik olarak kaynak kod analizi ile üretilmiştir. Her bulgu kaynak dosya referansına dayanmaktadır.*

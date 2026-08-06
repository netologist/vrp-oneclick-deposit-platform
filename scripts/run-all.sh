#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$ROOT/logs" "$ROOT/bin"

export JWT_SECRET="${JWT_SECRET:-super-secret-jwt-key}"
export REDIS_ADDR="${REDIS_ADDR:-localhost:6379}"
export KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:19092}"
export LOG_LEVEL="${LOG_LEVEL:-info}"

export MERCHANT_DB_URL="${MERCHANT_DB_URL:-postgres://vrp:vrp@localhost:5432/merchant?sslmode=disable}"
export CONSENT_DB_URL="${CONSENT_DB_URL:-postgres://vrp:vrp@localhost:5432/consent?sslmode=disable}"
export PAYMENT_DB_URL="${PAYMENT_DB_URL:-postgres://vrp:vrp@localhost:5432/payment?sslmode=disable}"
export LEDGER_DB_URL="${LEDGER_DB_URL:-postgres://vrp:vrp@localhost:5432/ledger?sslmode=disable}"

export MERCHANT_SVC_ADDR="${MERCHANT_SVC_ADDR:-localhost:50051}"
export CONSENT_SVC_ADDR="${CONSENT_SVC_ADDR:-localhost:50052}"
export PAYMENT_SVC_ADDR="${PAYMENT_SVC_ADDR:-localhost:50053}"
export RISK_SVC_ADDR="${RISK_SVC_ADDR:-localhost:50054}"
export LEDGER_SVC_ADDR="${LEDGER_SVC_ADDR:-localhost:50055}"
export BANK_ADAPTER_ADDR="${BANK_ADAPTER_ADDR:-localhost:50056}"
export MOCK_BANK_HTTP_ADDR="${MOCK_BANK_HTTP_ADDR:-localhost:18080}"
export GATEWAY_HTTP_ADDR="${GATEWAY_HTTP_ADDR:-:8080}"

start() {
  local name="$1"
  shift
  echo "starting $name..."
  "$@" >"$ROOT/logs/$name.log" 2>&1 &
  echo $! >"$ROOT/logs/$name.pid"
}

start merchant-svc   "$ROOT/bin/merchant-svc"
start consent-svc    "$ROOT/bin/consent-svc"
start risk-svc       "$ROOT/bin/risk-svc"
start ledger-svc     "$ROOT/bin/ledger-svc"
start bank-adapter   "$ROOT/bin/bank-adapter"
sleep 1
start payment-svc    "$ROOT/bin/payment-svc"
start notification-svc "$ROOT/bin/notification-svc"
sleep 1
start gateway        "$ROOT/bin/gateway"

echo "all services started. logs in $ROOT/logs"
echo "gateway: http://localhost:8080"
echo "tail -f logs/*.log"
wait

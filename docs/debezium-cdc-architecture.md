# Debezium CDC Outbox Architecture

## Overview

In high-throughput financial environments, polling the transactional outbox table via SQL queries (`SELECT ... FOR UPDATE SKIP LOCKED`) introduces periodic database CPU overhead and table bloat.

The **Debezium Change Data Capture (CDC)** architecture replaces polling by tailing the PostgreSQL Write-Ahead Log (WAL) directly using logical decoding (`pgoutput`).

```
┌─────────────────┐       ┌─────────────────┐       ┌─────────────────┐       ┌─────────────────┐
│   Payment DB    │       │    PostgreSQL   │       │  Debezium CDC   │       │  Redpanda Kafka │
│  (Settle Tx)    │ ────> │       WAL       │ ────> │  Event Router   │ ────> │ payment.events  │
│  INSERT outbox  │       │ (Logical Slot)  │       │      (SMT)      │       │      Topic      │
└─────────────────┘       └─────────────────┘       └─────────────────┘       └─────────────────┘
```

## How It Works

1. **Atomic Write**: The `payment-svc` writes `INSERT INTO outbox (topic, key, payload)` in the exact same transaction as the payment status update.
2. **WAL Capture**: PostgreSQL writes the insert into its WAL. The Debezium PostgreSQL connector reads the logical replication stream via replication slot `debezium_vrp_outbox_slot`.
3. **Outbox Event Router (SMT)**: Debezium applies the `io.debezium.transforms.outbox.EventRouter` Single Message Transform (SMT):
   - Extracts the destination `topic` column and routes the Kafka message dynamically.
   - Sets the Kafka message key from the `key` column (`merchant_id` or `payment_id`).
   - Unwraps the `payload` JSON as the message value.
4. **Zero Polling Latency**: Events arrive in Kafka in sub-milliseconds without executing any SQL SELECT or DELETE queries against the database.

## Registration

```bash
# Register the connector via Kafka Connect REST API
curl -i -X POST -H "Accept:application/json" -H "Content-Type:application/json" \
  http://localhost:8083/connectors/ \
  -d @deploy/debezium/register-postgres-outbox.json
```

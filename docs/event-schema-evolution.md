# Event Schema Evolution & Compatibility Guide

## Overview

The VRP Platform enforces **Protobuf Schema Registry** governance on all asynchronous Kafka topics (`payment.events`, `webhook.dlq`) to guarantee backward compatibility and prevent breaking consumers during independent deployments.

```
┌────────────────────┐
│   payment-svc      │ ─── 1. Check & Validate Schema ───> ┌─────────────────────┐
│ (Outbox Producer)  │                                     │  Buf / Schema       │
└─────────┬──────────┘                                     │  Registry           │
          │                                                └──────────┬──────────┘
          │ 2. Publish with Schema ID                                 │
          ▼                                                           │ 3. Fetch Schema
┌────────────────────┐                                                ▼
│ Redpanda / Kafka   │ ────────────────────────────────────> ┌─────────────────────┐
│  payment.events    │                                       │  notification-svc   │
└────────────────────┘                                       │ (Webhook Consumer)  │
                                                             └─────────────────────┘
```

## Schema Evolution Rules

1. **BACKWARD Compatibility**: Consumers using the latest schema can always read events written by older producers.
2. **Never Reorder/Delete Field Tags**: In Protobuf, field numbers (e.g. `string payment_id = 3;`) are immutable. Deleted fields must be marked `reserved`.
3. **Automated CI Breaking Change Detection**:
   ```bash
   buf breaking --against 'https://github.com/netologist/vrp-oneclick-deposit-platform.git#branch=main'
   ```

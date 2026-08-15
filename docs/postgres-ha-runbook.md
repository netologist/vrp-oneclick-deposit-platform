# PostgreSQL High-Availability & Disaster Recovery Runbook

## Overview

The VRP Platform utilizes CloudNativePG operator to manage a 3-instance PostgreSQL cluster with automated failover, read-write splitting via PgBouncer connection pooler, and continuous WAL archiving.

```
                    ┌─────────────────────────┐
                    │ Client / Microservices  │
                    └───────────┬─────────────┘
                                │
                    ┌───────────▼─────────────┐
                    │  PgBouncer Pooler (x2)  │
                    └───────────┬─────────────┘
                                │
            ┌───────────────────┼───────────────────┐
            │ (Primary)         │ (Streaming Rep)   │ (Streaming Rep)
   ┌────────▼────────┐ ┌────────▼────────┐ ┌────────▼────────┐
   │ vrp-postgres-1  │ │ vrp-postgres-2  │ │ vrp-postgres-3  │
   │   (Read/Write)  │ │   (Read-Only)   │ │   (Read-Only)   │
   └─────────────────┘ └─────────────────┘ └─────────────────┘
```

## Key Operations

### 1. Check Cluster Health
```bash
kubectl cnpg status vrp-postgres-ha -n vrp-demo
```

### 2. Manual Failover / Switchover
```bash
# Graceful switchover of the primary node
kubectl cnpg switchover vrp-postgres-ha --target=vrp-postgres-2 -n vrp-demo
```

### 3. Maintenance & Vacuuming
Transaction ID wraparound and table bloat monitoring are automated via autovacuum with:
- `autovacuum_vacuum_scale_factor = 0.05`
- `autovacuum_analyze_scale_factor = 0.02`

# Service Mesh & Zero-Trust mTLS Architecture

## Overview

The VRP Platform utilizes **Istio Service Mesh** in **STRICT mTLS mode** to guarantee cryptographic identity verification, automatic mutual TLS encryption, and fine-grained authorization policies between microservices.

```
┌─────────────┐        mTLS (SPIFFE ID)         ┌─────────────┐
│   Gateway   │ ─────────────────────────────>  │ Payment-Svc │
└─────────────┘                                 └──────┬──────┘
                                                       │
                      ┌────────────────────────────────┼────────────────────────────────┐
                      │ mTLS (STRICT)                  │ mTLS (STRICT)                  │ mTLS (STRICT)
                      ▼                                ▼                                ▼
               ┌─────────────┐                  ┌─────────────┐                  ┌─────────────┐
               │ Consent-Svc │                  │  Risk-Svc   │                  │ Ledger-Svc  │
               └─────────────┘                  └─────────────┘                  └─────────────┘
```

## Security Guarantees

1. **STRICT mTLS**: Unencrypted plaintext communication is rejected across the entire `vrp-demo` namespace.
2. **SPIFFE/SPIRE Identity**: Each service authenticates using its cryptographic X.509 certificate bound to `vrp-svc-account`.
3. **Least Privilege Authorization**: Downstream financial services (such as `ledger-svc` and `bank-adapter`) strictly permit calls originating from the payment orchestrator and reject direct calls from any other workload.

# REST API Reference & Swagger UI

The VRP Platform API Gateway exposes an **OpenAPI 3.0** compliant REST API for merchants to register, authenticate, manage consents, and initiate one-click deposits.

---

## Interactive Live Swagger UI

When running the system locally or in Kubernetes, access the live interactive Swagger UI directly from the API Gateway:

- **Live Gateway Swagger UI**: [http://localhost:8080/docs](http://localhost:8080/docs)
- **OpenAPI 3.0 Specification (YAML)**: [http://localhost:8080/docs/openapi.yaml](http://localhost:8080/docs/openapi.yaml)

---

## Embedded Swagger UI Explorer

Below is the embedded interactive Swagger UI rendering the platform API specification:

<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
<style>
  #swagger-ui { background: #ffffff; padding: 15px; border-radius: 8px; border: 1px solid #e0e0e0; }
  .swagger-ui .topbar { display: none; }
</style>

<div id="swagger-ui"></div>

<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
<script>
  window.addEventListener('load', function() {
    SwaggerUIBundle({
      url: "http://localhost:8080/docs/openapi.yaml",
      dom_id: '#swagger-ui',
      deepLinking: true,
      presets: [
        SwaggerUIBundle.presets.apis,
        SwaggerUIStandalonePreset
      ],
      plugins: [
        SwaggerUIBundle.plugins.DownloadUrl
      ]
    });
  });
</script>

---

## Core REST Endpoints Overview

| Endpoint | Method | Security | Description |
|----------|--------|----------|-------------|
| `/v1/auth/token` | `POST` | None | Exchange merchant API Key for JWT Bearer token |
| `/v1/merchants` | `POST` | None | Register new merchant & issue API Key |
| `/v1/merchants/{id}` | `GET` | Bearer JWT | Retrieve merchant details |
| `/v1/consents` | `POST` | Bearer JWT | Create a new VRP consent |
| `/v1/consents` | `GET` | Bearer JWT | List consents for consumer |
| `/v1/consents/{id}` | `GET` | Bearer JWT | Get consent details |
| `/v1/consents/{id}` | `DELETE` | Bearer JWT | Revoke a consent |
| `/v1/payments` | `POST` | Bearer JWT + Idempotency-Key | Initiate a one-click payment (Saga) |
| `/v1/payments/{id}` | `GET` | Bearer JWT | Get payment status |
| `/v1/payments/{id}/retry` | `POST` | Bearer JWT | Manually retry a failed payment |
| `/healthz/live` | `GET` | None | Liveness check |
| `/healthz/ready` | `GET` | None | Readiness check |

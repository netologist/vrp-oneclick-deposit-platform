package httpapi

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// RegisterSwaggerRoutes mounts OpenAPI specification and Swagger UI handlers.
func (h *Handlers) RegisterSwaggerRoutes(r chi.Router) {
	r.Get("/docs/openapi.yaml", h.ServeOpenAPISpec)
	r.Get("/docs", h.ServeSwaggerUI)
	r.Get("/docs/*", h.ServeSwaggerUI)
}

func (h *Handlers) ServeOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(OpenAPISpecYAML))
}

func (h *Handlers) ServeSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>VRP One-Click Deposit API Docs</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
  <style>
    html { box-sizing: border-box; overflow: -moz-scrollbars-vertical; overflow-y: scroll; }
    *, *:before, *:after { box-sizing: inherit; }
    body { margin:0; background: #fafafa; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
  <script>
    window.onload = function() {
      window.ui = SwaggerUIBundle({
        url: "/docs/openapi.yaml",
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIStandalonePreset
        ],
        plugins: [
          SwaggerUIBundle.plugins.DownloadUrl
        ],
        layout: "StandaloneLayout"
      });
    };
  </script>
</body>
</html>`)
	_, _ = w.Write([]byte(html))
}

const OpenAPISpecYAML = `openapi: "3.0.3"
info:
  title: VRP One-Click Deposit API
  description: |
    Pay-by-Bank / VRP payment platform.
    All monetary values are in pence (integer). £50.00 = 5000 pence.
    All requests to /v1/* require Bearer JWT token.
    Idempotency-Key header is required for POST /v1/payments.
  version: "1.0.0"
  contact:
    name: Platform Team
    email: platform@vrp-demo.internal

servers:
  - url: /v1
    description: API Gateway (/v1)
  - url: http://localhost:8080/v1
    description: Local Development / Kind Cluster (http://localhost:8080)
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
        currency:
          type: string
          example: GBP

    Error:
      type: object
      required: [code, message]
      properties:
        code:
          type: string
          example: CONSENT_LIMIT_EXCEEDED
        message:
          type: string
          example: "Monthly spending limit exceeded"

    Merchant:
      type: object
      properties:
        id:          { type: string, format: uuid }
        name:        { type: string }
        kyb_status:  { type: string }
        status:      { type: string }
        webhook_url: { type: string, format: uri }
        created_at:  { type: string, format: date-time }

    Consent:
      type: object
      properties:
        id:               { type: string, format: uuid }
        merchant_id:      { type: string, format: uuid }
        consumer_id:      { type: string }
        bank_consent_ref: { type: string }
        status:           { type: string }
        max_per_transaction: { $ref: "#/components/schemas/Money" }
        max_per_month:       { $ref: "#/components/schemas/Money" }
        valid_until:      { type: string, format: date-time }

    Payment:
      type: object
      properties:
        id:               { type: string, format: uuid }
        idempotency_key:  { type: string }
        merchant_id:      { type: string, format: uuid }
        consent_id:       { type: string, format: uuid }
        consumer_id:      { type: string }
        amount:           { $ref: "#/components/schemas/Money" }
        status:           { type: string }
        bank_payment_ref: { type: string }
        risk_decision:    { type: string }

paths:
  /auth/token:
    post:
      summary: Exchange API key for JWT
      security: []
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
  /merchants:
    post:
      summary: Register merchant
      security: []
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name, webhook_url]
              properties:
                name: { type: string }
                webhook_url: { type: string }
      responses:
        "201":
          description: Merchant created with plaintext API key
  /consents:
    post:
      summary: Create VRP consent
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [consumer_id, bank_consent_ref, max_per_transaction, max_per_month, valid_until]
      responses:
        "201":
          description: Consent created
  /payments:
    post:
      summary: Initiate one-click payment
      parameters:
        - name: Idempotency-Key
          in: header
          required: true
          schema: { type: string }
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [consent_id, amount]
      responses:
        "201":
          description: Payment settled
        "422":
          description: Business rule violation
`

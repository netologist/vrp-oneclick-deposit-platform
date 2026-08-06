package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	commonv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/common/v1"
	consentv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/consent/v1"
	merchantv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/merchant/v1"
	paymentv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/payment/v1"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/auth"
)

type Handlers struct {
	Tokens   *auth.TokenService
	Merchant merchantv1.MerchantServiceClient
	Consent  consentv1.ConsentServiceClient
	Payment  paymentv1.PaymentServiceClient
}

// POST /v1/auth/token
func (h *Handlers) IssueToken(w http.ResponseWriter, r *http.Request) {
	var req TokenRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.APIKey) == "" {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "api_key is required")
		return
	}

	m, err := h.Merchant.GetMerchantByApiKey(r.Context(), &merchantv1.GetMerchantByApiKeyRequest{
		ApiKey: req.APIKey,
	})
	if err != nil {
		mapGRPCError(w, r, err)
		return
	}
	if m.GetStatus() == "SUSPENDED" {
		writeError(w, r, http.StatusForbidden, "MERCHANT_SUSPENDED", "merchant is suspended")
		return
	}

	token, exp, err := h.Tokens.Issue(m.GetId())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to issue token")
		return
	}
	writeJSON(w, http.StatusOK, TokenResponse{
		Token:     token,
		ExpiresAt: exp.UTC().Format(time.RFC3339),
	})
}

// POST /v1/merchants
func (h *Handlers) RegisterMerchant(w http.ResponseWriter, r *http.Request) {
	var req RegisterMerchantRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.WebhookURL) == "" || strings.TrimSpace(req.ContactEmail) == "" {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "name, webhook_url, and contact_email are required")
		return
	}

	resp, err := h.Merchant.RegisterMerchant(r.Context(), &merchantv1.RegisterMerchantRequest{
		Name:         req.Name,
		WebhookUrl:   req.WebhookURL,
		ContactEmail: req.ContactEmail,
	})
	if err != nil {
		mapGRPCError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, RegisterMerchantResponse{
		Merchant: merchantFromProto(resp.GetMerchant()),
		APIKey:   resp.GetApiKey(),
	})
}

// GET /v1/merchants/{id}
func (h *Handlers) GetMerchant(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	merchantID := MerchantIDFrom(r.Context())
	if id != merchantID {
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "cannot access another merchant")
		return
	}

	m, err := h.Merchant.GetMerchant(r.Context(), &merchantv1.GetMerchantRequest{MerchantId: id})
	if err != nil {
		mapGRPCError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, merchantFromProto(m))
}

// POST /v1/consents
func (h *Handlers) CreateConsent(w http.ResponseWriter, r *http.Request) {
	var req CreateConsentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON body")
		return
	}
	if req.ConsumerID == "" || req.BankConsentRef == "" {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "consumer_id and bank_consent_ref are required")
		return
	}
	if req.MaxPerTransaction.AmountPence <= 0 || req.MaxPerMonth.AmountPence <= 0 {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "max_per_transaction and max_per_month must be positive")
		return
	}
	validUntil, err := parseTime(req.ValidUntil)
	if err != nil || validUntil == nil {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "valid_until must be RFC3339 timestamp")
		return
	}

	c, err := h.Consent.CreateConsent(r.Context(), &consentv1.CreateConsentRequest{
		MerchantId:        MerchantIDFrom(r.Context()),
		ConsumerId:        req.ConsumerID,
		BankConsentRef:    req.BankConsentRef,
		MaxPerTransaction: moneyToProto(req.MaxPerTransaction),
		MaxPerMonth:       moneyToProto(req.MaxPerMonth),
		ValidUntil:        validUntil,
	})
	if err != nil {
		mapGRPCError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, consentFromProto(c))
}

// GET /v1/consents/{id}
func (h *Handlers) GetConsent(w http.ResponseWriter, r *http.Request) {
	c, err := h.Consent.GetConsent(r.Context(), &consentv1.GetConsentRequest{
		ConsentId:  chi.URLParam(r, "id"),
		MerchantId: MerchantIDFrom(r.Context()),
	})
	if err != nil {
		mapGRPCError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, consentFromProto(c))
}

// DELETE /v1/consents/{id}
func (h *Handlers) RevokeConsent(w http.ResponseWriter, r *http.Request) {
	var req RevokeConsentRequest
	// body optional
	_ = decodeJSON(r, &req)
	if req.Reason == "" {
		req.Reason = "MERCHANT_REQUEST"
	}

	c, err := h.Consent.RevokeConsent(r.Context(), &consentv1.RevokeConsentRequest{
		ConsentId:  chi.URLParam(r, "id"),
		MerchantId: MerchantIDFrom(r.Context()),
		Reason:     req.Reason,
	})
	if err != nil {
		mapGRPCError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, consentFromProto(c))
}

// GET /v1/consents?consumer_id=
func (h *Handlers) ListConsents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	consumerID := q.Get("consumer_id")
	if consumerID == "" {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "consumer_id query param is required")
		return
	}

	pageSize := int32(20)
	if v := q.Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 100 {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "page_size must be 1-100")
			return
		}
		pageSize = int32(n)
	}

	resp, err := h.Consent.ListConsents(r.Context(), &consentv1.ListConsentsRequest{
		ConsumerId: consumerID,
		MerchantId: MerchantIDFrom(r.Context()),
		Status:     parseConsentStatus(q.Get("status")),
		Page: &commonv1.PageRequest{
			PageSize:  pageSize,
			PageToken: q.Get("page_token"),
		},
	})
	if err != nil {
		mapGRPCError(w, r, err)
		return
	}

	out := ListConsentsResponse{
		Consents: make([]ConsentResponse, 0, len(resp.GetConsents())),
	}
	for _, c := range resp.GetConsents() {
		out.Consents = append(out.Consents, consentFromProto(c))
	}
	if p := resp.GetPage(); p != nil {
		out.NextPageToken = p.GetNextPageToken()
		out.TotalCount = p.GetTotalCount()
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /v1/payments
func (h *Handlers) InitiatePayment(w http.ResponseWriter, r *http.Request) {
	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idemKey == "" {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Idempotency-Key header is required")
		return
	}
	if len(idemKey) > 128 {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Idempotency-Key max length is 128")
		return
	}

	var req InitiatePaymentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON body")
		return
	}
	if req.ConsentID == "" {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "consent_id is required")
		return
	}
	if req.Amount.AmountPence <= 0 || req.Amount.Currency == "" {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "amount.amount_pence and amount.currency are required")
		return
	}

	p, err := h.Payment.InitiatePayment(r.Context(), &paymentv1.InitiatePaymentRequest{
		IdempotencyKey: idemKey,
		MerchantId:     MerchantIDFrom(r.Context()),
		ConsentId:      req.ConsentID,
		Amount:         moneyToProto(req.Amount),
		Description:    req.Description,
	})
	if err != nil {
		mapGRPCError(w, r, err)
		return
	}

	body := paymentFromProto(p)
	switch p.GetStatus() {
	case paymentv1.PaymentStatus_SETTLED:
		writeJSON(w, http.StatusCreated, body)
	case paymentv1.PaymentStatus_FAILED:
		// business failure still returns the payment resource with failure details
		writeJSON(w, http.StatusUnprocessableEntity, body)
	default:
		w.Header().Set("Location", "/v1/payments/"+p.GetId())
		w.Header().Set("Retry-After", "2")
		writeJSON(w, http.StatusAccepted, body)
	}
}

// GET /v1/payments/{id}
func (h *Handlers) GetPayment(w http.ResponseWriter, r *http.Request) {
	p, err := h.Payment.GetPayment(r.Context(), &paymentv1.GetPaymentRequest{
		PaymentId:  chi.URLParam(r, "id"),
		MerchantId: MerchantIDFrom(r.Context()),
	})
	if err != nil {
		mapGRPCError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, paymentFromProto(p))
}

// POST /v1/payments/{id}/retry
func (h *Handlers) RetryPayment(w http.ResponseWriter, r *http.Request) {
	p, err := h.Payment.RetryPayment(r.Context(), &paymentv1.RetryPaymentRequest{
		PaymentId:  chi.URLParam(r, "id"),
		MerchantId: MerchantIDFrom(r.Context()),
	})
	if err != nil {
		mapGRPCError(w, r, err)
		return
	}

	body := paymentFromProto(p)
	switch p.GetStatus() {
	case paymentv1.PaymentStatus_SETTLED:
		writeJSON(w, http.StatusOK, body)
	case paymentv1.PaymentStatus_FAILED:
		writeJSON(w, http.StatusUnprocessableEntity, body)
	default:
		writeJSON(w, http.StatusAccepted, body)
	}
}

// GET /healthz/live
func (h *Handlers) Live(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /healthz/ready
func (h *Handlers) Ready(w http.ResponseWriter, r *http.Request) {
	// readiness is process-up for demo; upstream dial is blocking at startup
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

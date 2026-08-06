package httpapi

// Money is integer minor units + ISO 4217 currency. Never float64.
type Money struct {
	AmountPence int64  `json:"amount_pence"`
	Currency    string `json:"currency"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// --- Auth ---

type TokenRequest struct {
	APIKey string `json:"api_key"`
}

type TokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// --- Merchants ---

type RegisterMerchantRequest struct {
	Name         string `json:"name"`
	WebhookURL   string `json:"webhook_url"`
	ContactEmail string `json:"contact_email"`
}

type MerchantResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	KYBStatus  string `json:"kyb_status"`
	Status     string `json:"status"`
	WebhookURL string `json:"webhook_url"`
	CreatedAt  string `json:"created_at,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

type RegisterMerchantResponse struct {
	Merchant MerchantResponse `json:"merchant"`
	APIKey   string           `json:"api_key"`
}

// --- Consents ---

type CreateConsentRequest struct {
	ConsumerID         string `json:"consumer_id"`
	BankConsentRef     string `json:"bank_consent_ref"`
	MaxPerTransaction  Money  `json:"max_per_transaction"`
	MaxPerMonth        Money  `json:"max_per_month"`
	ValidUntil         string `json:"valid_until"`
}

type ConsentResponse struct {
	ID                string `json:"id"`
	MerchantID        string `json:"merchant_id"`
	ConsumerID        string `json:"consumer_id"`
	BankConsentRef    string `json:"bank_consent_ref"`
	Status            string `json:"status"`
	MaxPerTransaction Money  `json:"max_per_transaction"`
	MaxPerMonth       Money  `json:"max_per_month"`
	ValidFrom         string `json:"valid_from,omitempty"`
	ValidUntil        string `json:"valid_until,omitempty"`
	CreatedAt         string `json:"created_at,omitempty"`
	UpdatedAt         string `json:"updated_at,omitempty"`
}

type ListConsentsResponse struct {
	Consents      []ConsentResponse `json:"consents"`
	NextPageToken string            `json:"next_page_token,omitempty"`
	TotalCount    int32             `json:"total_count"`
}

type RevokeConsentRequest struct {
	Reason string `json:"reason"`
}

// --- Payments ---

type InitiatePaymentRequest struct {
	ConsentID   string `json:"consent_id"`
	Amount      Money  `json:"amount"`
	Description string `json:"description,omitempty"`
}

type PaymentResponse struct {
	ID             string `json:"id"`
	IdempotencyKey string `json:"idempotency_key"`
	MerchantID     string `json:"merchant_id"`
	ConsentID      string `json:"consent_id"`
	ConsumerID     string `json:"consumer_id,omitempty"`
	Amount         Money  `json:"amount"`
	Status         string `json:"status"`
	BankPaymentRef string `json:"bank_payment_ref,omitempty"`
	RiskScore      int32  `json:"risk_score,omitempty"`
	RiskDecision   string `json:"risk_decision,omitempty"`
	FailureReason  string `json:"failure_reason,omitempty"`
	Description    string `json:"description,omitempty"`
	InitiatedAt    string `json:"initiated_at,omitempty"`
	SettledAt      string `json:"settled_at,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
}

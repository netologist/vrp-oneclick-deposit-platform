package internal

import "time"

const (
	StatusActive    = "ACTIVE"
	StatusSuspended = "SUSPENDED"

	KYBPending   = "PENDING_KYB"
	KYBApproved  = "KYB_APPROVED"
	KYBActive    = "ACTIVE"
	KYBSuspended = "SUSPENDED"

	APIKeyStatusActive  = "ACTIVE"
	APIKeyStatusRevoked = "REVOKED"
)

type Merchant struct {
	ID           string
	Name         string
	WebhookURL   string
	ContactEmail string
	KYBStatus    string
	Status       string
	HMACSecret   string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type APIKey struct {
	ID         string
	MerchantID string
	KeyHash    string
	KeyPrefix  string
	Status     string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
}

type WebhookConfig struct {
	MerchantID string
	WebhookURL string
	HMACSecret string
}

type RegisterInput struct {
	Name         string
	WebhookURL   string
	ContactEmail string
}

type RegisterResult struct {
	Merchant *Merchant
	APIKey   string // plaintext, shown once
}

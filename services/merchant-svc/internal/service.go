package internal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/domainerr"
	"golang.org/x/crypto/bcrypt"
)

const apiKeyPrefix = "vrp_"

type Service struct {
	repo *Repo
}

func NewService(repo *Repo) *Service {
	return &Service{repo: repo}
}

func (s *Service) RegisterMerchant(ctx context.Context, in RegisterInput) (*RegisterResult, error) {
	name := strings.TrimSpace(in.Name)
	webhookURL := strings.TrimSpace(in.WebhookURL)
	contactEmail := strings.TrimSpace(in.ContactEmail)

	if name == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "name is required")
	}
	if webhookURL == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "webhook_url is required")
	}

	plaintextKey, err := generateAPIKey()
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "generate api key", err)
	}
	keyHash, err := bcrypt.GenerateFromPassword([]byte(plaintextKey), 10)
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "hash api key", err)
	}
	hmacSecret, err := randomHex(32)
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "generate hmac secret", err)
	}

	m := &Merchant{
		Name:         name,
		WebhookURL:   webhookURL,
		ContactEmail: contactEmail,
		// Demo: skip real KYB delay — merchants register as ACTIVE.
		KYBStatus:  KYBActive,
		Status:     StatusActive,
		HMACSecret: hmacSecret,
	}
	prefix := plaintextKey[:8]
	if err := s.repo.CreateMerchantAndAPIKey(ctx, m, string(keyHash), prefix); err != nil {
		return nil, err
	}
	return &RegisterResult{Merchant: m, APIKey: plaintextKey}, nil
}

func (s *Service) GetMerchant(ctx context.Context, id string) (*Merchant, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "merchant_id is required")
	}
	return s.repo.GetByID(ctx, id)
}

func (s *Service) SuspendMerchant(ctx context.Context, id, _ string) (*Merchant, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "merchant_id is required")
	}
	return s.repo.Suspend(ctx, id)
}

func (s *Service) GetMerchantByAPIKey(ctx context.Context, apiKey string) (*Merchant, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "api_key is required")
	}
	return s.repo.GetByAPIKey(ctx, apiKey)
}

func (s *Service) GetWebhookConfig(ctx context.Context, merchantID string) (*WebhookConfig, error) {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "merchant_id is required")
	}
	return s.repo.GetWebhookConfig(ctx, merchantID)
}

func generateAPIKey() (string, error) {
	hexPart, err := randomHex(32)
	if err != nil {
		return "", err
	}
	return apiKeyPrefix + hexPart, nil
}

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

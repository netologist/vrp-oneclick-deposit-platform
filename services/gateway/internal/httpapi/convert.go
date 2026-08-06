package httpapi

import (
	"time"

	commonv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/common/v1"
	consentv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/consent/v1"
	merchantv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/merchant/v1"
	paymentv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/payment/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func moneyToProto(m Money) *commonv1.Money {
	return &commonv1.Money{
		AmountPence: m.AmountPence,
		Currency:    m.Currency,
	}
}

func moneyFromProto(m *commonv1.Money) Money {
	if m == nil {
		return Money{}
	}
	return Money{AmountPence: m.GetAmountPence(), Currency: m.GetCurrency()}
}

func tsString(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	t := ts.AsTime()
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) (*timestamppb.Timestamp, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, err
		}
	}
	return timestamppb.New(t.UTC()), nil
}

func merchantFromProto(m *merchantv1.Merchant) MerchantResponse {
	if m == nil {
		return MerchantResponse{}
	}
	return MerchantResponse{
		ID:         m.GetId(),
		Name:       m.GetName(),
		KYBStatus:  m.GetKybStatus(),
		Status:     m.GetStatus(),
		WebhookURL: m.GetWebhookUrl(),
		CreatedAt:  tsString(m.GetCreatedAt()),
		UpdatedAt:  tsString(m.GetUpdatedAt()),
	}
}

func consentStatusString(s consentv1.ConsentStatus) string {
	switch s {
	case consentv1.ConsentStatus_PENDING:
		return "PENDING"
	case consentv1.ConsentStatus_ACTIVE:
		return "ACTIVE"
	case consentv1.ConsentStatus_REVOKED:
		return "REVOKED"
	case consentv1.ConsentStatus_EXPIRED:
		return "EXPIRED"
	default:
		return s.String()
	}
}

func parseConsentStatus(s string) consentv1.ConsentStatus {
	switch s {
	case "PENDING":
		return consentv1.ConsentStatus_PENDING
	case "ACTIVE":
		return consentv1.ConsentStatus_ACTIVE
	case "REVOKED":
		return consentv1.ConsentStatus_REVOKED
	case "EXPIRED":
		return consentv1.ConsentStatus_EXPIRED
	default:
		return consentv1.ConsentStatus_CONSENT_STATUS_UNSPECIFIED
	}
}

func consentFromProto(c *consentv1.Consent) ConsentResponse {
	if c == nil {
		return ConsentResponse{}
	}
	return ConsentResponse{
		ID:                c.GetId(),
		MerchantID:        c.GetMerchantId(),
		ConsumerID:        c.GetConsumerId(),
		BankConsentRef:    c.GetBankConsentRef(),
		Status:            consentStatusString(c.GetStatus()),
		MaxPerTransaction: moneyFromProto(c.GetMaxPerTransaction()),
		MaxPerMonth:       moneyFromProto(c.GetMaxPerMonth()),
		ValidFrom:         tsString(c.GetValidFrom()),
		ValidUntil:        tsString(c.GetValidUntil()),
		CreatedAt:         tsString(c.GetCreatedAt()),
		UpdatedAt:         tsString(c.GetUpdatedAt()),
	}
}

func paymentStatusString(s paymentv1.PaymentStatus) string {
	switch s {
	case paymentv1.PaymentStatus_INITIATED:
		return "INITIATED"
	case paymentv1.PaymentStatus_CONSENT_RESERVED:
		return "CONSENT_RESERVED"
	case paymentv1.PaymentStatus_RISK_PASSED:
		return "RISK_PASSED"
	case paymentv1.PaymentStatus_AUTHORISING:
		return "AUTHORISING"
	case paymentv1.PaymentStatus_SETTLED:
		return "SETTLED"
	case paymentv1.PaymentStatus_FAILED:
		return "FAILED"
	case paymentv1.PaymentStatus_MANUAL_REVIEW:
		return "MANUAL_REVIEW"
	default:
		return s.String()
	}
}

func paymentFromProto(p *paymentv1.Payment) PaymentResponse {
	if p == nil {
		return PaymentResponse{}
	}
	return PaymentResponse{
		ID:             p.GetId(),
		IdempotencyKey: p.GetIdempotencyKey(),
		MerchantID:     p.GetMerchantId(),
		ConsentID:      p.GetConsentId(),
		ConsumerID:     p.GetConsumerId(),
		Amount:         moneyFromProto(p.GetAmount()),
		Status:         paymentStatusString(p.GetStatus()),
		BankPaymentRef: p.GetBankPaymentRef(),
		RiskScore:      p.GetRiskScore(),
		RiskDecision:   p.GetRiskDecision(),
		FailureReason:  p.GetFailureReason(),
		Description:    p.GetDescription(),
		InitiatedAt:    tsString(p.GetInitiatedAt()),
		SettledAt:      tsString(p.GetSettledAt()),
		UpdatedAt:      tsString(p.GetUpdatedAt()),
	}
}

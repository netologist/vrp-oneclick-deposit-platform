package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"
	bankv1 "github.com/hozgan/vrp-demo/gen/bank/v1"
	"github.com/hozgan/vrp-demo/pkg/shared/domainerr"
	"github.com/sony/gobreaker"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Adapter is the gRPC BankAdapter that proxies to the mock bank HTTP API
// behind a circuit breaker and retry policy.
type Adapter struct {
	bankv1.UnimplementedBankAdapterServer
	baseURL    string
	httpClient *http.Client
	cb         *gobreaker.CircuitBreaker
	log        *slog.Logger
}

// NewAdapter constructs a BankAdapter client wrapper.
func NewAdapter(baseURL string) *Adapter {
	baseURL = strings.TrimRight(baseURL, "/")
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "bank-api",
		MaxRequests: 5,
		Interval:    60 * time.Second,
		Timeout:     10 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures > 10
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			slog.Warn("bank circuit breaker state change",
				"name", name, "from", from.String(), "to", to.String())
		},
	})
	return &Adapter{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		cb:  cb,
		log: slog.Default().With("component", "bank-adapter"),
	}
}

type httpInitiateReq struct {
	PaymentID      string `json:"payment_id"`
	BankConsentRef string `json:"bank_consent_ref"`
	ConsumerID     string `json:"consumer_id"`
	AmountPence    int64  `json:"amount_pence"`
	Currency       string `json:"currency"`
	Description    string `json:"description"`
}

type httpInitiateResp struct {
	BankPaymentRef string `json:"bank_payment_ref"`
	Status         string `json:"status"`
	FailureReason  string `json:"failure_reason"`
}

type httpStatusResp struct {
	BankPaymentRef string `json:"bank_payment_ref"`
	Status         string `json:"status"`
	UpdatedAt      string `json:"updated_at"`
	FailureReason  string `json:"failure_reason"`
	ReversalRef    string `json:"reversal_ref"`
}

type httpReverseReq struct {
	Reason string `json:"reason"`
}

type httpReverseResp struct {
	BankPaymentRef string `json:"bank_payment_ref"`
	ReversalRef    string `json:"reversal_ref"`
	Status         string `json:"status"`
}

// InitiatePayment calls POST /bank/payments.
func (a *Adapter) InitiatePayment(ctx context.Context, req *bankv1.InitiateRequest) (*bankv1.InitiateResponse, error) {
	if req.GetPaymentId() == "" {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "payment_id is required"))
	}
	if req.GetBankConsentRef() == "" {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "bank_consent_ref is required"))
	}
	if req.GetAmount() == nil || req.GetAmount().GetAmountPence() <= 0 {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "amount must be positive"))
	}

	currency := req.GetAmount().GetCurrency()
	if currency == "" {
		currency = "GBP"
	}
	body := httpInitiateReq{
		PaymentID:      req.GetPaymentId(),
		BankConsentRef: req.GetBankConsentRef(),
		ConsumerID:     req.GetConsumerId(),
		AmountPence:    req.GetAmount().GetAmountPence(),
		Currency:       currency,
		Description:    req.GetDescription(),
	}

	var out httpInitiateResp
	if err := a.doWithResilience(ctx, http.MethodPost, "/bank/payments", body, &out); err != nil {
		return nil, domainerr.ToGRPC(err)
	}

	status, err := mapStatus(out.Status)
	if err != nil {
		return nil, domainerr.ToGRPC(domainerr.Wrap(domainerr.CodeInternal, "unknown bank status", err))
	}

	resp := &bankv1.InitiateResponse{
		BankPaymentRef: out.BankPaymentRef,
		Status:         status,
		FailureReason:  out.FailureReason,
		InitiatedAt:    timestamppb.Now(),
	}
	if status == bankv1.BankPaymentStatus_REJECTED && resp.FailureReason == "" {
		resp.FailureReason = "bank rejected payment"
	}
	return resp, nil
}

// GetPaymentStatus calls GET /bank/payments/{ref}.
func (a *Adapter) GetPaymentStatus(ctx context.Context, req *bankv1.StatusRequest) (*bankv1.StatusResponse, error) {
	ref := req.GetBankPaymentRef()
	if ref == "" {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "bank_payment_ref is required"))
	}

	var out httpStatusResp
	path := "/bank/payments/" + ref
	if err := a.doWithResilience(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, domainerr.ToGRPC(err)
	}

	status, err := mapStatus(out.Status)
	if err != nil {
		return nil, domainerr.ToGRPC(domainerr.Wrap(domainerr.CodeInternal, "unknown bank status", err))
	}

	updated := timestamppb.Now()
	if out.UpdatedAt != "" {
		if t, perr := time.Parse(time.RFC3339Nano, out.UpdatedAt); perr == nil {
			updated = timestamppb.New(t)
		} else if t, perr := time.Parse(time.RFC3339, out.UpdatedAt); perr == nil {
			updated = timestamppb.New(t)
		}
	}

	return &bankv1.StatusResponse{
		BankPaymentRef: out.BankPaymentRef,
		Status:         status,
		UpdatedAt:      updated,
	}, nil
}

// ReversePayment calls POST /bank/payments/{ref}/reverse.
func (a *Adapter) ReversePayment(ctx context.Context, req *bankv1.ReverseRequest) (*bankv1.ReverseResponse, error) {
	ref := req.GetBankPaymentRef()
	if ref == "" {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "bank_payment_ref is required"))
	}

	var out httpReverseResp
	path := "/bank/payments/" + ref + "/reverse"
	body := httpReverseReq{Reason: req.GetReason()}
	if err := a.doWithResilience(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, domainerr.ToGRPC(err)
	}

	status, err := mapStatus(out.Status)
	if err != nil {
		return nil, domainerr.ToGRPC(domainerr.Wrap(domainerr.CodeInternal, "unknown bank status", err))
	}

	return &bankv1.ReverseResponse{
		BankPaymentRef: out.BankPaymentRef,
		ReversalRef:    out.ReversalRef,
		Status:         status,
	}, nil
}

func (a *Adapter) doWithResilience(ctx context.Context, method, path string, body any, out any) error {
	_, err := a.cb.Execute(func() (any, error) {
		var last error
		rerr := retry.Do(
			func() error {
				attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				last = a.doHTTP(attemptCtx, method, path, body, out)
				return last
			},
			retry.Attempts(3),
			retry.Delay(100*time.Millisecond),
			retry.DelayType(retry.BackOffDelay),
			retry.Context(ctx),
			retry.LastErrorOnly(true),
			retry.RetryIf(func(err error) bool {
				return isTransient(err)
			}),
		)
		if rerr != nil {
			if last != nil {
				return nil, normalizeErr(last)
			}
			return nil, normalizeErr(rerr)
		}
		return nil, nil
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return domainerr.New(domainerr.CodeBankUnavailable, "bank circuit breaker open")
		}
		return err
	}
	return nil
}

func (a *Adapter) doHTTP(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "marshal request", err)
		}
		rdr = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, rdr)
	if err != nil {
		return domainerr.Wrap(domainerr.CodeInternal, "build request", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return domainerr.Wrap(domainerr.CodeBankUnavailable, "bank http call failed", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return domainerr.Wrap(domainerr.CodeBankUnavailable, "read bank response", err)
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return domainerr.New(domainerr.CodeNotFound, "bank payment not found")
	case resp.StatusCode == http.StatusBadRequest:
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = "invalid bank request"
		}
		return domainerr.New(domainerr.CodeValidation, msg)
	case resp.StatusCode == http.StatusConflict:
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = "bank payment conflict"
		}
		return domainerr.New(domainerr.CodeConflict, msg)
	case resp.StatusCode >= 500:
		return &transientError{msg: fmt.Sprintf("bank returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))}
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return domainerr.New(domainerr.CodeBankUnavailable, fmt.Sprintf("bank returned %d", resp.StatusCode))
	}

	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "decode bank response", err)
		}
	}
	return nil
}

func normalizeErr(err error) error {
	if err == nil {
		return nil
	}
	var te *transientError
	if errors.As(err, &te) {
		return domainerr.New(domainerr.CodeBankUnavailable, te.msg)
	}
	return err
}

type transientError struct{ msg string }

func (e *transientError) Error() string { return e.msg }

func isTransient(err error) bool {
	if err == nil {
		return false
	}
	var te *transientError
	if errors.As(err, &te) {
		return true
	}
	return domainerr.Is(err, domainerr.CodeBankUnavailable)
}

func mapStatus(s string) (bankv1.BankPaymentStatus, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "PENDING":
		return bankv1.BankPaymentStatus_PENDING, nil
	case "AUTHORISED", "AUTHORIZED":
		return bankv1.BankPaymentStatus_AUTHORISED, nil
	case "SETTLED":
		return bankv1.BankPaymentStatus_SETTLED, nil
	case "REJECTED":
		return bankv1.BankPaymentStatus_REJECTED, nil
	case "REVERSED":
		return bankv1.BankPaymentStatus_REVERSED, nil
	case "", "UNSPECIFIED":
		return bankv1.BankPaymentStatus_BANK_PAYMENT_STATUS_UNSPECIFIED, nil
	default:
		return 0, fmt.Errorf("unmapped status %q", s)
	}
}

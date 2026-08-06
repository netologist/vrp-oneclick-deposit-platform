package mockbank

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hozgan/vrp-demo/pkg/shared/config"
)

type paymentRecord struct {
	PaymentID      string    `json:"payment_id"`
	BankConsentRef string    `json:"bank_consent_ref"`
	ConsumerID     string    `json:"consumer_id"`
	AmountPence    int64     `json:"amount_pence"`
	Currency       string    `json:"currency"`
	Description    string    `json:"description"`
	BankPaymentRef string    `json:"bank_payment_ref"`
	Status         string    `json:"status"`
	FailureReason  string    `json:"failure_reason,omitempty"`
	ReversalRef    string    `json:"reversal_ref,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type initiateRequest struct {
	PaymentID      string `json:"payment_id"`
	BankConsentRef string `json:"bank_consent_ref"`
	ConsumerID     string `json:"consumer_id"`
	AmountPence    int64  `json:"amount_pence"`
	Currency       string `json:"currency"`
	Description    string `json:"description"`
}

type reverseRequest struct {
	Reason string `json:"reason"`
}

// Server is an in-memory mock Open Banking HTTP API.
type Server struct {
	addr     string
	failRate float64
	mu       sync.RWMutex
	payments map[string]*paymentRecord
	httpSrv  *http.Server
	log      *slog.Logger
}

// New creates a mock bank server.
// Addr defaults via MOCK_BANK_HTTP_ADDR (:18080).
// Fail rate via MOCK_BANK_FAIL_RATE (0.0–1.0).
func New(addr string) *Server {
	if addr == "" {
		addr = config.Get("MOCK_BANK_HTTP_ADDR", ":18080")
	}
	return &Server{
		addr:     addr,
		failRate: config.GetFloat("MOCK_BANK_FAIL_RATE", 0),
		payments: make(map[string]*paymentRecord),
		log:      slog.Default().With("component", "mockbank"),
	}
}

// Addr returns the listen address (updated after Start binds an ephemeral port).
func (s *Server) Addr() string { return s.addr }

// Handler exposes the HTTP mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /bank/payments", s.handleInitiate)
	mux.HandleFunc("GET /bank/payments/{ref}", s.handleStatus)
	mux.HandleFunc("POST /bank/payments/{ref}/reverse", s.handleReverse)
	return mux
}

// Start binds and serves in a background goroutine.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.addr = ln.Addr().String()
	s.httpSrv = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.log.Info("mock bank HTTP listening", "addr", s.addr, "fail_rate", s.failRate)
	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Error("mock bank HTTP stopped", "err", err)
		}
	}()
	return nil
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) handleInitiate(w http.ResponseWriter, r *http.Request) {
	time.Sleep(50 * time.Millisecond)

	if s.shouldFail() {
		s.writeFailure(w)
		return
	}

	var req initiateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.PaymentID == "" || req.BankConsentRef == "" || req.AmountPence <= 0 {
		http.Error(w, "payment_id, bank_consent_ref and positive amount_pence required", http.StatusBadRequest)
		return
	}
	if req.Currency == "" {
		req.Currency = "GBP"
	}

	now := time.Now().UTC()
	rec := &paymentRecord{
		PaymentID:      req.PaymentID,
		BankConsentRef: req.BankConsentRef,
		ConsumerID:     req.ConsumerID,
		AmountPence:    req.AmountPence,
		Currency:       req.Currency,
		Description:    req.Description,
		BankPaymentRef: "FPS-" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:16]),
		Status:         "SETTLED",
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	s.mu.Lock()
	s.payments[rec.BankPaymentRef] = rec
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"bank_payment_ref": rec.BankPaymentRef,
		"status":           rec.Status,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	time.Sleep(50 * time.Millisecond)

	if s.shouldFail() {
		s.writeFailure(w)
		return
	}

	ref := r.PathValue("ref")
	s.mu.RLock()
	rec, ok := s.payments[ref]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "payment not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"bank_payment_ref": rec.BankPaymentRef,
		"status":           rec.Status,
		"updated_at":       rec.UpdatedAt.Format(time.RFC3339Nano),
		"failure_reason":   rec.FailureReason,
		"reversal_ref":     rec.ReversalRef,
	})
}

func (s *Server) handleReverse(w http.ResponseWriter, r *http.Request) {
	time.Sleep(50 * time.Millisecond)

	if s.shouldFail() {
		s.writeFailure(w)
		return
	}

	ref := r.PathValue("ref")
	var req reverseRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.payments[ref]
	if !ok {
		http.Error(w, "payment not found", http.StatusNotFound)
		return
	}
	if rec.Status == "REVERSED" {
		writeJSON(w, http.StatusOK, map[string]any{
			"bank_payment_ref": rec.BankPaymentRef,
			"reversal_ref":     rec.ReversalRef,
			"status":           rec.Status,
		})
		return
	}
	if rec.Status != "SETTLED" && rec.Status != "AUTHORISED" {
		http.Error(w, "payment not reversible in status "+rec.Status, http.StatusConflict)
		return
	}

	rec.Status = "REVERSED"
	rec.ReversalRef = "REV-" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:12])
	rec.UpdatedAt = time.Now().UTC()
	if req.Reason != "" {
		rec.FailureReason = req.Reason
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"bank_payment_ref": rec.BankPaymentRef,
		"reversal_ref":     rec.ReversalRef,
		"status":           rec.Status,
	})
}

func (s *Server) shouldFail() bool {
	if s.failRate <= 0 {
		return false
	}
	if s.failRate >= 1 {
		return true
	}
	return rand.Float64() < s.failRate
}

// writeFailure returns either a business REJECTED (200) or a transient 503.
func (s *Server) writeFailure(w http.ResponseWriter) {
	if rand.Float64() < 0.5 {
		ref := "FPS-" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:16])
		now := time.Now().UTC()
		rec := &paymentRecord{
			BankPaymentRef: ref,
			Status:         "REJECTED",
			FailureReason:  "mock bank simulated rejection",
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		s.mu.Lock()
		s.payments[ref] = rec
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"bank_payment_ref": ref,
			"status":           "REJECTED",
			"failure_reason":   rec.FailureReason,
		})
		return
	}
	http.Error(w, "mock bank unavailable", http.StatusServiceUnavailable)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

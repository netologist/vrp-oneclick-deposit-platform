package internal

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	bankv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/bank/v1"
	commonv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/common/v1"
	consentv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/consent/v1"
	ledgerv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/ledger/v1"
	riskv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/risk/v1"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/idempotency"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// --- Fake Repo ---

type fakeRepo struct {
	payments map[uuid.UUID]*Payment
	byKey    map[string]*Payment
	events   map[uuid.UUID][]string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		payments: make(map[uuid.UUID]*Payment),
		byKey:    make(map[string]*Payment),
		events:   make(map[uuid.UUID][]string),
	}
}

func (r *fakeRepo) CreatePayment(ctx context.Context, p *Payment) error {
	if _, exists := r.byKey[p.IdempotencyKey]; exists {
		return errors.New("duplicate key")
	}
	cpy := *p
	r.payments[p.ID] = &cpy
	r.byKey[p.IdempotencyKey] = &cpy
	return nil
}

func (r *fakeRepo) GetByID(ctx context.Context, id uuid.UUID) (*Payment, error) {
	p, ok := r.payments[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cpy := *p
	return &cpy, nil
}

func (r *fakeRepo) GetByIdempotencyKey(ctx context.Context, key string) (*Payment, error) {
	p, ok := r.byKey[key]
	if !ok {
		return nil, errors.New("not found")
	}
	cpy := *p
	return &cpy, nil
}

func (r *fakeRepo) UpdateAfterConsent(ctx context.Context, p *Payment, reservationID uuid.UUID, consumerID string) error {
	p.ReservationID = &reservationID
	p.ConsumerID = consumerID
	p.Status = StatusConsentReserved
	r.payments[p.ID] = p
	return nil
}

func (r *fakeRepo) UpdateAfterRisk(ctx context.Context, p *Payment, score int32, decision string) error {
	p.RiskScore = &score
	p.RiskDecision = decision
	p.Status = StatusRiskPassed
	r.payments[p.ID] = p
	return nil
}

func (r *fakeRepo) UpdateAfterBank(ctx context.Context, p *Payment, bankRef string) error {
	p.BankPaymentRef = bankRef
	p.Status = StatusAuthorising
	r.payments[p.ID] = p
	return nil
}

func (r *fakeRepo) SettleWithOutbox(ctx context.Context, p *Payment) error {
	now := time.Now()
	p.Status = StatusSettled
	p.SettledAt = &now
	r.payments[p.ID] = p
	return nil
}

func (r *fakeRepo) MarkFailed(ctx context.Context, p *Payment, reason string) error {
	p.Status = StatusFailed
	p.FailureReason = reason
	r.payments[p.ID] = p
	return nil
}

func (r *fakeRepo) MarkManualReview(ctx context.Context, p *Payment, reason string) error {
	p.Status = StatusManualReview
	p.FailureReason = reason
	r.payments[p.ID] = p
	return nil
}

func (r *fakeRepo) ResetForRetry(ctx context.Context, p *Payment) error {
	p.Status = StatusInitiated
	p.FailureReason = ""
	r.payments[p.ID] = p
	return nil
}

// --- Mock gRPC Servers ---

type mockConsentServer struct {
	consentv1.UnimplementedConsentServiceServer
	validateAndReserve func(ctx context.Context, req *consentv1.ReserveRequest) (*consentv1.ReserveResponse, error)
	releaseReservation func(ctx context.Context, req *consentv1.ReleaseRequest) (*emptypb.Empty, error)
	confirmReservation func(ctx context.Context, req *consentv1.ConfirmRequest) (*emptypb.Empty, error)
}

func (m *mockConsentServer) ValidateAndReserve(ctx context.Context, req *consentv1.ReserveRequest) (*consentv1.ReserveResponse, error) {
	if m.validateAndReserve != nil {
		return m.validateAndReserve(ctx, req)
	}
	return &consentv1.ReserveResponse{
		ReservationId:   uuid.NewString(),
		ConsumerId:      "consumer-123",
		BankConsentRef:  "bank-consent-123",
		RemainingMonthly: &commonv1.Money{AmountPence: 100000, Currency: "GBP"},
	}, nil
}

func (m *mockConsentServer) ConfirmReservation(ctx context.Context, req *consentv1.ConfirmRequest) (*emptypb.Empty, error) {
	if m.confirmReservation != nil {
		return m.confirmReservation(ctx, req)
	}
	return &emptypb.Empty{}, nil
}

func (m *mockConsentServer) ReleaseReservation(ctx context.Context, req *consentv1.ReleaseRequest) (*emptypb.Empty, error) {
	if m.releaseReservation != nil {
		return m.releaseReservation(ctx, req)
	}
	return &emptypb.Empty{}, nil
}

type mockRiskServer struct {
	riskv1.UnimplementedRiskServiceServer
	score func(ctx context.Context, req *riskv1.ScoreRequest) (*riskv1.ScoreResponse, error)
}

func (m *mockRiskServer) Score(ctx context.Context, req *riskv1.ScoreRequest) (*riskv1.ScoreResponse, error) {
	if m.score != nil {
		return m.score(ctx, req)
	}
	return &riskv1.ScoreResponse{
		Score:    10,
		Decision: riskv1.RiskDecision_ALLOW,
		Reason:   "all clear",
	}, nil
}

type mockBankServer struct {
	bankv1.UnimplementedBankAdapterServer
	initiate func(ctx context.Context, req *bankv1.InitiateRequest) (*bankv1.InitiateResponse, error)
}

func (m *mockBankServer) InitiatePayment(ctx context.Context, req *bankv1.InitiateRequest) (*bankv1.InitiateResponse, error) {
	if m.initiate != nil {
		return m.initiate(ctx, req)
	}
	return &bankv1.InitiateResponse{
		BankPaymentRef: "FPS-MOCK-123",
		Status:         bankv1.BankPaymentStatus_SETTLED,
	}, nil
}

type mockLedgerServer struct {
	ledgerv1.UnimplementedLedgerServiceServer
	post func(ctx context.Context, req *ledgerv1.PostEntryRequest) (*ledgerv1.JournalEntry, error)
}

func (m *mockLedgerServer) PostDoubleEntry(ctx context.Context, req *ledgerv1.PostEntryRequest) (*ledgerv1.JournalEntry, error) {
	if m.post != nil {
		return m.post(ctx, req)
	}
	return &ledgerv1.JournalEntry{Id: uuid.NewString(), PaymentId: req.PaymentId}, nil
}

func startMockGRPC(t *testing.T, register func(s *grpc.Server)) (grpcConn *grpc.ClientConn, cleanup func()) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := grpc.NewServer()
	register(s)
	go func() { _ = s.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	return conn, func() {
		_ = conn.Close()
		s.Stop()
		_ = lis.Close()
	}
}

// --- Unit Tests ---

func TestPlatformFee(t *testing.T) {
	tests := []struct {
		amount   int64
		expected int64
	}{
		{amount: 50, expected: 1},    // 1% is 0.5 -> min 1
		{amount: 100, expected: 1},   // 1% of 100 = 1
		{amount: 5000, expected: 50}, // 1% of 5000 = 50 (£0.50 fee on £50)
	}
	for _, tt := range tests {
		got := platformFee(tt.amount)
		assert.Equal(t, tt.expected, got)
	}
}

func TestSagaOrchestrator_HappyPath(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	idemStore := idempotency.NewStore(rdb)

	consentSrv := &mockConsentServer{}
	riskSrv := &mockRiskServer{}
	bankSrv := &mockBankServer{}
	ledgerSrv := &mockLedgerServer{}

	cConn, cClean := startMockGRPC(t, func(s *grpc.Server) { consentv1.RegisterConsentServiceServer(s, consentSrv) })
	defer cClean()
	rConn, rClean := startMockGRPC(t, func(s *grpc.Server) { riskv1.RegisterRiskServiceServer(s, riskSrv) })
	defer rClean()
	bConn, bClean := startMockGRPC(t, func(s *grpc.Server) { bankv1.RegisterBankAdapterServer(s, bankSrv) })
	defer bClean()
	lConn, lClean := startMockGRPC(t, func(s *grpc.Server) { ledgerv1.RegisterLedgerServiceServer(s, ledgerSrv) })
	defer lClean()

	repo := newFakeRepo()
	orch := NewOrchestrator(
		repo,
		idemStore,
		consentv1.NewConsentServiceClient(cConn),
		riskv1.NewRiskServiceClient(rConn),
		bankv1.NewBankAdapterClient(bConn),
		ledgerv1.NewLedgerServiceClient(lConn),
	)

	mID := uuid.NewString()
	cID := uuid.NewString()

	p, err := orch.Initiate(context.Background(), InitiateInput{
		IdempotencyKey: "test-happy-1",
		MerchantID:     mID,
		ConsentID:      cID,
		AmountPence:    5000,
		Currency:       "GBP",
		Description:    "Test Payment",
	})

	require.NoError(t, err)
	assert.Equal(t, StatusSettled, p.Status)
	assert.Equal(t, "FPS-MOCK-123", p.BankPaymentRef)
	assert.Equal(t, "ALLOW", p.RiskDecision)
}

func TestSagaOrchestrator_RiskDecline(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	idemStore := idempotency.NewStore(rdb)

	consentReleased := false
	consentSrv := &mockConsentServer{
		releaseReservation: func(ctx context.Context, req *consentv1.ReleaseRequest) (*emptypb.Empty, error) {
			consentReleased = true
			return &emptypb.Empty{}, nil
		},
	}
	riskSrv := &mockRiskServer{
		score: func(ctx context.Context, req *riskv1.ScoreRequest) (*riskv1.ScoreResponse, error) {
			return &riskv1.ScoreResponse{
				Score:    90,
				Decision: riskv1.RiskDecision_DECLINE,
				Reason:   "known fraud pattern",
			}, nil
		},
	}
	bankSrv := &mockBankServer{}
	ledgerSrv := &mockLedgerServer{}

	cConn, cClean := startMockGRPC(t, func(s *grpc.Server) { consentv1.RegisterConsentServiceServer(s, consentSrv) })
	defer cClean()
	rConn, rClean := startMockGRPC(t, func(s *grpc.Server) { riskv1.RegisterRiskServiceServer(s, riskSrv) })
	defer rClean()
	bConn, bClean := startMockGRPC(t, func(s *grpc.Server) { bankv1.RegisterBankAdapterServer(s, bankSrv) })
	defer bClean()
	lConn, lClean := startMockGRPC(t, func(s *grpc.Server) { ledgerv1.RegisterLedgerServiceServer(s, ledgerSrv) })
	defer lClean()

	repo := newFakeRepo()
	orch := NewOrchestrator(
		repo,
		idemStore,
		consentv1.NewConsentServiceClient(cConn),
		riskv1.NewRiskServiceClient(rConn),
		bankv1.NewBankAdapterClient(bConn),
		ledgerv1.NewLedgerServiceClient(lConn),
	)

	p, err := orch.Initiate(context.Background(), InitiateInput{
		IdempotencyKey: "test-risk-decline",
		MerchantID:     uuid.NewString(),
		ConsentID:      uuid.NewString(),
		AmountPence:    5000,
		Currency:       "GBP",
	})

	assert.Error(t, err)
	assert.Equal(t, StatusFailed, p.Status)
	assert.True(t, consentReleased, "reservation should be released on risk decline")
}

func TestSagaOrchestrator_ConsentLimitExceeded(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	idemStore := idempotency.NewStore(rdb)

	consentSrv := &mockConsentServer{
		validateAndReserve: func(ctx context.Context, req *consentv1.ReserveRequest) (*consentv1.ReserveResponse, error) {
			return nil, status.Error(codes.FailedPrecondition, "CONSENT_LIMIT_EXCEEDED: monthly limit exceeded")
		},
	}
	riskSrv := &mockRiskServer{}
	bankSrv := &mockBankServer{}
	ledgerSrv := &mockLedgerServer{}

	cConn, cClean := startMockGRPC(t, func(s *grpc.Server) { consentv1.RegisterConsentServiceServer(s, consentSrv) })
	defer cClean()
	rConn, rClean := startMockGRPC(t, func(s *grpc.Server) { riskv1.RegisterRiskServiceServer(s, riskSrv) })
	defer rClean()
	bConn, bClean := startMockGRPC(t, func(s *grpc.Server) { bankv1.RegisterBankAdapterServer(s, bankSrv) })
	defer bClean()
	lConn, lClean := startMockGRPC(t, func(s *grpc.Server) { ledgerv1.RegisterLedgerServiceServer(s, ledgerSrv) })
	defer lClean()

	repo := newFakeRepo()
	orch := NewOrchestrator(
		repo,
		idemStore,
		consentv1.NewConsentServiceClient(cConn),
		riskv1.NewRiskServiceClient(rConn),
		bankv1.NewBankAdapterClient(bConn),
		ledgerv1.NewLedgerServiceClient(lConn),
	)

	p, err := orch.Initiate(context.Background(), InitiateInput{
		IdempotencyKey: "test-limit-exceeded",
		MerchantID:     uuid.NewString(),
		ConsentID:      uuid.NewString(),
		AmountPence:    150000,
		Currency:       "GBP",
	})

	assert.Error(t, err)
	assert.Equal(t, StatusFailed, p.Status)
	assert.Contains(t, p.FailureReason, "CONSENT_LIMIT_EXCEEDED")
}

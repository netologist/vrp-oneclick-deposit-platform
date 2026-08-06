package internal

import (
	"context"

	ledgerv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/ledger/v1"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/domainerr"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Server struct {
	ledgerv1.UnimplementedLedgerServiceServer
	store *Store
}

func NewServer(store *Store) *Server {
	return &Server{store: store}
}

func (s *Server) PostDoubleEntry(ctx context.Context, req *ledgerv1.PostEntryRequest) (*ledgerv1.JournalEntry, error) {
	if req == nil {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "request is required"))
	}

	lines := make([]lineInput, 0, len(req.GetLines()))
	for _, l := range req.GetLines() {
		accType, err := accountTypeToDB(l.GetAccountType())
		if err != nil {
			return nil, domainerr.ToGRPC(err)
		}
		dir, err := directionToDB(l.GetDirection())
		if err != nil {
			return nil, domainerr.ToGRPC(err)
		}
		amt := l.GetAmount()
		if amt == nil {
			return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "line amount is required"))
		}
		lines = append(lines, lineInput{
			accountType: accType,
			ownerRef:    l.GetOwnerRef(),
			direction:   dir,
			amountPence: amt.GetAmountPence(),
			currency:    amt.GetCurrency(),
		})
	}

	entry, err := s.store.PostDoubleEntry(ctx, req.GetPaymentId(), req.GetDescription(), lines)
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return entry, nil
}

func (s *Server) ReverseEntry(ctx context.Context, req *ledgerv1.ReverseRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "request is required"))
	}
	if err := s.store.ReverseEntry(ctx, req.GetPaymentId(), req.GetReason()); err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) GetBalance(ctx context.Context, req *ledgerv1.BalanceRequest) (*ledgerv1.BalanceResponse, error) {
	if req == nil {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "request is required"))
	}
	accType, err := accountTypeToDB(req.GetAccountType())
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	resp, err := s.store.GetBalance(ctx, accType, req.GetOwnerRef(), req.GetCurrency())
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return resp, nil
}

func (s *Server) GetJournalEntry(ctx context.Context, req *ledgerv1.JournalEntryRequest) (*ledgerv1.JournalEntry, error) {
	if req == nil {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "request is required"))
	}
	entry, err := s.store.GetJournalEntry(ctx, req.GetPaymentId())
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return entry, nil
}

package internal

import (
	"context"
	"testing"

	commonv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/common/v1"
	ledgerv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/ledger/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServer_Validation(t *testing.T) {
	t.Parallel()

	srv := NewServer(nil) // store is nil; validation occurs before store calls

	t.Run("PostDoubleEntry nil request", func(t *testing.T) {
		_, err := srv.PostDoubleEntry(context.Background(), nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("PostDoubleEntry unspecified account type", func(t *testing.T) {
		req := &ledgerv1.PostEntryRequest{
			PaymentId: "pay-1",
			Lines: []*ledgerv1.JournalLine{
				{
					AccountType: ledgerv1.AccountType_ACCOUNT_TYPE_UNSPECIFIED,
					Direction:   ledgerv1.Direction_DR,
					Amount:      &commonv1.Money{AmountPence: 100, Currency: "GBP"},
				},
			},
		}
		_, err := srv.PostDoubleEntry(context.Background(), req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("PostDoubleEntry unspecified direction", func(t *testing.T) {
		req := &ledgerv1.PostEntryRequest{
			PaymentId: "pay-1",
			Lines: []*ledgerv1.JournalLine{
				{
					AccountType: ledgerv1.AccountType_CONSUMER_ESCROW,
					Direction:   ledgerv1.Direction_DIRECTION_UNSPECIFIED,
					Amount:      &commonv1.Money{AmountPence: 100, Currency: "GBP"},
				},
			},
		}
		_, err := srv.PostDoubleEntry(context.Background(), req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("PostDoubleEntry nil amount", func(t *testing.T) {
		req := &ledgerv1.PostEntryRequest{
			PaymentId: "pay-1",
			Lines: []*ledgerv1.JournalLine{
				{
					AccountType: ledgerv1.AccountType_CONSUMER_ESCROW,
					Direction:   ledgerv1.Direction_DR,
					Amount:      nil,
				},
			},
		}
		_, err := srv.PostDoubleEntry(context.Background(), req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("ReverseEntry nil request", func(t *testing.T) {
		_, err := srv.ReverseEntry(context.Background(), nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("GetBalance nil request", func(t *testing.T) {
		_, err := srv.GetBalance(context.Background(), nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("GetJournalEntry nil request", func(t *testing.T) {
		_, err := srv.GetJournalEntry(context.Background(), nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

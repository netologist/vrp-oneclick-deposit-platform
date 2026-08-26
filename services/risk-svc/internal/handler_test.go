package internal

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	commonv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/common/v1"
	riskv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/risk/v1"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestHandler(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	engine := NewEngine(rdb)
	handler := NewHandler(engine, rdb)

	t.Run("Score validation errors", func(t *testing.T) {
		// nil request
		_, err := handler.Score(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))

		// missing consumer_id
		_, err = handler.Score(context.Background(), &riskv1.ScoreRequest{
			MerchantId: "m-1",
			ConsumerId: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))

		// missing merchant_id
		_, err = handler.Score(context.Background(), &riskv1.ScoreRequest{
			MerchantId: "",
			ConsumerId: "c-1",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("Score happy path", func(t *testing.T) {
		resp, err := handler.Score(context.Background(), &riskv1.ScoreRequest{
			MerchantId: "m-1",
			ConsumerId: "consumer-happy",
			Amount:     &commonv1.Money{AmountPence: 2000, Currency: "GBP"},
		})
		require.NoError(t, err)
		assert.Equal(t, riskv1.RiskDecision_ALLOW, resp.GetDecision())
		assert.Equal(t, int32(0), resp.GetScore())
	})

	t.Run("AddToBlocklist and RemoveFromBlocklist", func(t *testing.T) {
		// nil request
		_, err := handler.AddToBlocklist(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))

		// invalid type
		_, err = handler.AddToBlocklist(context.Background(), &riskv1.BlocklistRequest{
			Type:  "INVALID_TYPE",
			Value: "val",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))

		// empty value
		_, err = handler.AddToBlocklist(context.Background(), &riskv1.BlocklistRequest{
			Type:  "CONSUMER",
			Value: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))

		// valid add
		addResp, err := handler.AddToBlocklist(context.Background(), &riskv1.BlocklistRequest{
			Type:   "CONSUMER",
			Value:  "fraud-user-1",
			Reason: "chargeback fraud",
		})
		require.NoError(t, err)
		assert.Equal(t, "CONSUMER", addResp.GetType())
		assert.Equal(t, "fraud-user-1", addResp.GetValue())
		assert.NotEmpty(t, addResp.GetId())

		// Score now DECLINE
		scoreResp, err := handler.Score(context.Background(), &riskv1.ScoreRequest{
			MerchantId: "m-1",
			ConsumerId: "fraud-user-1",
			Amount:     &commonv1.Money{AmountPence: 2000, Currency: "GBP"},
		})
		require.NoError(t, err)
		assert.Equal(t, riskv1.RiskDecision_DECLINE, scoreResp.GetDecision())

		// valid remove
		remResp, err := handler.RemoveFromBlocklist(context.Background(), &riskv1.BlocklistRequest{
			Type:  "CONSUMER",
			Value: "fraud-user-1",
		})
		require.NoError(t, err)
		assert.Equal(t, "CONSUMER", remResp.GetType())
		assert.Equal(t, "fraud-user-1", remResp.GetValue())

		// Score now ALLOW again
		scoreResp2, err := handler.Score(context.Background(), &riskv1.ScoreRequest{
			MerchantId: "m-1",
			ConsumerId: "fraud-user-1",
			Amount:     &commonv1.Money{AmountPence: 2000, Currency: "GBP"},
		})
		require.NoError(t, err)
		assert.Equal(t, riskv1.RiskDecision_ALLOW, scoreResp2.GetDecision())
	})
}

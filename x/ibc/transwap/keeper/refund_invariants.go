package keeper

import (
	"bytes"
	"sort"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"

	"github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
)

type expectedRefundAccounting struct {
	pending        map[string]sdkmath.Int
	locked         map[string]sdkmath.Int
	reserveBacking map[string]sdkmath.Int
}

type expectedRefundTransportBacking struct {
	portID    string
	channelID string
	denom     string
	amount    sdkmath.Int
}

// AssertRefundInvariants performs an offline/genesis-grade cross-module audit.
// It is O(total refund records + BEX accounting rows) and must not run in a
// per-block path.
func (k Keeper) AssertRefundInvariants(ctx sdk.Context) error {
	expectedOutputIndexes := make(map[string]string)
	expectedActiveIndexes := make(map[string]string)
	expectedRetryIndexes := make(map[string]string)
	expectedLivePackets := make(map[string]string)
	expectedByExchange := make(map[uint64]*expectedRefundAccounting)
	exchangeIDs := make(map[uint64]struct{})
	expectedTransportBacking := make(map[string]*expectedRefundTransportBacking)
	expectedTrackedTransportBacking := make(map[string]sdkmath.Int)

	for _, record := range k.GetAllRefundRecords(ctx) {
		if err := ValidateRefundRecord(record); err != nil {
			return types.ErrRefundEscrowInvariant.Wrapf("refund %s is invalid: %v", record.GetId(), err)
		}
		exchangeID, err := parseExchangeID(record.GetExchangeId())
		if err != nil {
			return types.ErrRefundEscrowInvariant.Wrapf("refund %s has invalid exchange: %v", record.GetId(), err)
		}
		exchangeIDs[exchangeID] = struct{}{}
		expected := expectedByExchange[exchangeID]
		if expected == nil {
			expected = &expectedRefundAccounting{
				pending:        make(map[string]sdkmath.Int),
				locked:         make(map[string]sdkmath.Int),
				reserveBacking: make(map[string]sdkmath.Int),
			}
			expectedByExchange[exchangeID] = expected
		}

		gross, err := types.TokenToCoin(&record.Token)
		if err != nil {
			return types.ErrRefundEscrowInvariant.Wrapf("refund %s gross token: %v", record.GetId(), err)
		}
		fee, err := types.ProtoCoinToSDK(record.GetOriginalFee())
		if err != nil {
			return types.ErrRefundEscrowInvariant.Wrapf("refund %s fee: %v", record.GetId(), err)
		}

		switch record.GetStatus() {
		case types.RefundStatus_REFUND_STATUS_PENDING:
			if err := addRefundAccountingCoin(expected.pending, gross); err != nil {
				return refundAccountingOverflow(record.GetId(), gross.Denom)
			}
			if fee.IsPositive() {
				if err := addRefundAccountingCoin(expected.locked, fee); err != nil {
					return refundAccountingOverflow(record.GetId(), fee.Denom)
				}
			}
			principal := sdk.NewCoin(gross.Denom, gross.Amount.Sub(fee.Amount))
			if err := addRefundAccountingCoin(expected.reserveBacking, principal); err != nil {
				return refundAccountingOverflow(record.GetId(), principal.Denom)
			}
			packetKey := refundPacketIndexKey(
				record.GetOriginalOutputPort(),
				record.GetOriginalOutputChannel(),
				record.GetOriginalOutputSequence(),
			)
			if existing, found := expectedLivePackets[packetKey]; found {
				return types.ErrRefundEscrowInvariant.Wrapf(
					"refunds %s and %s share live packet %s",
					existing,
					record.GetId(),
					packetKey,
				)
			}
			expectedLivePackets[packetKey] = record.GetId()
			expectedOutputIndexes[packetKey] = record.GetId()
			indexed, found, err := k.refundForOutputPacket(
				ctx,
				record.GetOriginalOutputPort(),
				record.GetOriginalOutputChannel(),
				record.GetOriginalOutputSequence(),
			)
			if err != nil || !found || indexed.GetId() != record.GetId() {
				return types.ErrRefundEscrowInvariant.Wrapf("refund %s output index is missing", record.GetId())
			}
			actualCommitment := k.channelKeeper.GetPacketCommitment(
				ctx,
				record.GetOriginalOutputPort(),
				record.GetOriginalOutputChannel(),
				record.GetOriginalOutputSequence(),
			)
			if !bytes.Equal(actualCommitment, record.GetOriginalOutputPacketCommitment()) {
				return types.ErrRefundEscrowInvariant.Wrapf(
					"refund %s original output packet commitment is missing or changed",
					record.GetId(),
				)
			}

		case types.RefundStatus_REFUND_STATUS_RETRYABLE:
			if err := addRefundAccountingCoin(expected.pending, gross); err != nil {
				return refundAccountingOverflow(record.GetId(), gross.Denom)
			}
			if err := addRefundAccountingCoin(expected.reserveBacking, gross); err != nil {
				return refundAccountingOverflow(record.GetId(), gross.Denom)
			}
			if record.GetNextRetryHeight() == 0 {
				return types.ErrRefundEscrowInvariant.Wrapf("retryable refund %s is not scheduled", record.GetId())
			}
			queueKey := refundRetryQueueKey(record.GetNextRetryHeight(), record.GetId())
			expectedRetryIndexes[string(queueKey[len(types.RefundRetryPrefix):])] = record.GetId()

		case types.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE:
			if err := addRefundAccountingCoin(expected.pending, gross); err != nil {
				return refundAccountingOverflow(record.GetId(), gross.Denom)
			}
			if err := addRefundAccountingCoin(expected.reserveBacking, gross); err != nil {
				return refundAccountingOverflow(record.GetId(), gross.Denom)
			}

		case types.RefundStatus_REFUND_STATUS_IN_FLIGHT:
			if err := addRefundAccountingCoin(expected.pending, gross); err != nil {
				return refundAccountingOverflow(record.GetId(), gross.Denom)
			}
			packetKey := refundPacketIndexKey(
				record.GetRefundSourcePort(),
				record.GetRefundSourceChannel(),
				record.GetActivePacketSequence(),
			)
			if existing, found := expectedLivePackets[packetKey]; found {
				return types.ErrRefundEscrowInvariant.Wrapf(
					"refunds %s and %s share live packet %s",
					existing,
					record.GetId(),
					packetKey,
				)
			}
			expectedLivePackets[packetKey] = record.GetId()
			expectedActiveIndexes[packetKey] = record.GetId()
			indexed, found, err := k.refundForActivePacket(
				ctx,
				record.GetRefundSourcePort(),
				record.GetRefundSourceChannel(),
				record.GetActivePacketSequence(),
			)
			if err != nil || !found || indexed.GetId() != record.GetId() {
				return types.ErrRefundEscrowInvariant.Wrapf("refund %s active index is missing", record.GetId())
			}
			reserveSender := k.BexKeeper.GetReserveAddress(ctx, exchangeID).String()
			packetData := types.NewFungibleTokenPacketData(
				types.DenomPath(record.GetToken().Denom),
				record.GetToken().Amount,
				reserveSender,
				record.GetReceiver(),
				record.GetMemo(),
			)
			expectedCommitment := channeltypes.CommitPacket(channeltypes.NewPacket(
				types.FungibleTokenPacketDataBytes(packetData),
				record.GetActivePacketSequence(),
				record.GetRefundSourcePort(),
				record.GetRefundSourceChannel(),
				"",
				"",
				clienttypes.ZeroHeight(),
				record.GetActiveTimeoutTimestamp(),
			))
			actualCommitment := k.channelKeeper.GetPacketCommitment(
				ctx,
				record.GetRefundSourcePort(),
				record.GetRefundSourceChannel(),
				record.GetActivePacketSequence(),
			)
			if !bytes.Equal(actualCommitment, expectedCommitment) {
				return types.ErrRefundEscrowInvariant.Wrapf(
					"refund %s active packet commitment is missing or changed",
					record.GetId(),
				)
			}
			if !types.DenomHasPrefix(
				record.GetToken().Denom,
				record.GetRefundSourcePort(),
				record.GetRefundSourceChannel(),
			) {
				key := record.GetRefundSourcePort() + "\x00" + record.GetRefundSourceChannel() + "\x00" + gross.Denom
				backing := expectedTransportBacking[key]
				if backing == nil {
					backing = &expectedRefundTransportBacking{
						portID:    record.GetRefundSourcePort(),
						channelID: record.GetRefundSourceChannel(),
						denom:     gross.Denom,
						amount:    sdkmath.ZeroInt(),
					}
					expectedTransportBacking[key] = backing
				}
				next, err := backing.amount.SafeAdd(gross.Amount)
				if err != nil {
					return refundAccountingOverflow(record.GetId(), gross.Denom)
				}
				backing.amount = next
			}

		case types.RefundStatus_REFUND_STATUS_COMPLETED,
			types.RefundStatus_REFUND_STATUS_CLAIMED:
			// Terminal records retain audit metadata but no live accounting.
		}
	}

	bexExchangeIDs, err := k.BexKeeper.GetRefundAccountingExchangeIDs(ctx)
	if err != nil {
		return types.ErrRefundEscrowInvariant.Wrapf("list BEX refund accounting: %v", err)
	}
	for _, exchangeID := range bexExchangeIDs {
		exchangeIDs[exchangeID] = struct{}{}
	}
	orderedExchangeIDs := make([]uint64, 0, len(exchangeIDs))
	for exchangeID := range exchangeIDs {
		orderedExchangeIDs = append(orderedExchangeIDs, exchangeID)
	}
	sort.Slice(orderedExchangeIDs, func(i, j int) bool { return orderedExchangeIDs[i] < orderedExchangeIDs[j] })

	for _, exchangeID := range orderedExchangeIDs {
		expected := expectedByExchange[exchangeID]
		if expected == nil {
			expected = &expectedRefundAccounting{
				pending:        map[string]sdkmath.Int{},
				locked:         map[string]sdkmath.Int{},
				reserveBacking: map[string]sdkmath.Int{},
			}
		}
		expectedPending := refundAccountingCoins(expected.pending)
		expectedLocked := refundAccountingCoins(expected.locked)
		expectedBacking := refundAccountingCoins(expected.reserveBacking)

		actualPending, err := k.BexKeeper.GetPendingLiabilities(ctx, exchangeID)
		if err != nil {
			return types.ErrRefundEscrowInvariant.Wrapf("exchange %d pending liability: %v", exchangeID, err)
		}
		if !actualPending.Equal(expectedPending) {
			return types.ErrRefundEscrowInvariant.Wrapf(
				"exchange %d pending liability is %s, expected %s",
				exchangeID,
				actualPending.String(),
				expectedPending.String(),
			)
		}
		actualLocked, err := k.BexKeeper.GetLockedFees(ctx, exchangeID)
		if err != nil {
			return types.ErrRefundEscrowInvariant.Wrapf("exchange %d locked fees: %v", exchangeID, err)
		}
		if !actualLocked.Equal(expectedLocked) {
			return types.ErrRefundEscrowInvariant.Wrapf(
				"exchange %d locked fees are %s, expected %s",
				exchangeID,
				actualLocked.String(),
				expectedLocked.String(),
			)
		}

		reserveAddress := k.BexKeeper.GetReserveAddress(ctx, exchangeID)
		reserveBalance := k.BankKeeper.GetAllBalances(ctx, reserveAddress)
		for _, required := range expectedBacking {
			if reserveBalance.AmountOf(required.Denom).LT(required.Amount) {
				return types.ErrRefundEscrowInvariant.Wrapf(
					"exchange %d reserve backing for %s is %s, requires at least %s",
					exchangeID,
					required.Denom,
					reserveBalance.AmountOf(required.Denom),
					required.Amount,
				)
			}
		}
	}

	transportKeys := make([]string, 0, len(expectedTransportBacking))
	for key := range expectedTransportBacking {
		transportKeys = append(transportKeys, key)
	}
	sort.Strings(transportKeys)
	for _, key := range transportKeys {
		required := expectedTransportBacking[key]
		escrowAddress := types.GetEscrowAddress(required.portID, required.channelID)
		actual := k.BankKeeper.GetAllBalances(ctx, escrowAddress).AmountOf(required.denom)
		if actual.LT(required.amount) {
			return types.ErrRefundEscrowInvariant.Wrapf(
				"refund transport escrow %s/%s for %s is %s, requires at least %s",
				required.portID,
				required.channelID,
				required.denom,
				actual,
				required.amount,
			)
		}
		trackedRequired := expectedTrackedTransportBacking[required.denom]
		if trackedRequired.IsNil() {
			trackedRequired = sdkmath.ZeroInt()
		}
		nextTrackedRequired, err := trackedRequired.SafeAdd(required.amount)
		if err != nil {
			return refundAccountingOverflow("transport", required.denom)
		}
		expectedTrackedTransportBacking[required.denom] = nextTrackedRequired
	}

	trackedDenoms := make([]string, 0, len(expectedTrackedTransportBacking))
	for denom := range expectedTrackedTransportBacking {
		trackedDenoms = append(trackedDenoms, denom)
	}
	sort.Strings(trackedDenoms)
	for _, denom := range trackedDenoms {
		required := expectedTrackedTransportBacking[denom]
		tracked := k.GetTotalEscrowForDenom(ctx, denom).Amount
		if tracked.LT(required) {
			return types.ErrRefundEscrowInvariant.Wrapf(
				"tracked refund transport escrow for %s is %s, requires at least %s",
				denom,
				tracked,
				required,
			)
		}
	}

	return k.assertRefundIndexes(ctx, expectedOutputIndexes, expectedActiveIndexes, expectedRetryIndexes)
}

func addRefundAccountingCoin(total map[string]sdkmath.Int, coin sdk.Coin) error {
	current := total[coin.Denom]
	if current.IsNil() {
		current = sdkmath.ZeroInt()
	}
	next, err := current.SafeAdd(coin.Amount)
	if err != nil {
		return err
	}
	total[coin.Denom] = next
	return nil
}

func refundAccountingCoins(total map[string]sdkmath.Int) sdk.Coins {
	denoms := make([]string, 0, len(total))
	for denom := range total {
		denoms = append(denoms, denom)
	}
	sort.Strings(denoms)
	coins := make(sdk.Coins, 0, len(denoms))
	for _, denom := range denoms {
		if total[denom].IsPositive() {
			coins = append(coins, sdk.NewCoin(denom, total[denom]))
		}
	}
	return coins
}

func refundAccountingOverflow(refundID, denom string) error {
	return types.ErrRefundEscrowInvariant.Wrapf(
		"refund accounting overflow for refund %s denom %s",
		refundID,
		denom,
	)
}

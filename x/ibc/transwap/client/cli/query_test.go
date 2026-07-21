package cli

import (
	"testing"

	transwapv1 "github.com/gurufinglobal/guru/v3/x/ibc/transwap/types"
	"github.com/stretchr/testify/require"
)

func TestGetQueryCmdHasSubcommands(t *testing.T) {
	cmd := GetQueryCmd()
	require.Equal(t, "ibc-transwap", cmd.Use)
	for _, name := range []string{"params", "refund", "refunds", "denom", "denoms", "escrow-address", "denom-hash", "total-escrow"} {
		_, _, err := cmd.Find([]string{name})
		require.NoError(t, err)
	}
}

func TestReadPageRequest(t *testing.T) {
	cmd := GetCmdQueryDenoms()
	pageReq, err := readPageRequest(cmd)
	require.NoError(t, err)
	require.NotNil(t, pageReq)
	require.Equal(t, uint64(100), pageReq.Limit)
	require.Equal(t, []byte{}, pageReq.Key)

	require.NoError(t, cmd.Flags().Set("limit", "25"))
	pageReq, err = readPageRequest(cmd)
	require.NoError(t, err)
	require.Equal(t, uint64(25), pageReq.Limit)
}

func TestParseRefundStatus(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want transwapv1.RefundStatus
	}{
		{raw: "", want: transwapv1.RefundStatus_REFUND_STATUS_UNSPECIFIED},
		{raw: "all", want: transwapv1.RefundStatus_REFUND_STATUS_UNSPECIFIED},
		{raw: " ALL ", want: transwapv1.RefundStatus_REFUND_STATUS_UNSPECIFIED},
		{raw: "0", want: transwapv1.RefundStatus_REFUND_STATUS_UNSPECIFIED},
		{raw: "pending", want: transwapv1.RefundStatus_REFUND_STATUS_PENDING},
		{raw: "in-flight", want: transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT},
		{raw: "IN_FLIGHT", want: transwapv1.RefundStatus_REFUND_STATUS_IN_FLIGHT},
		{raw: "manual claimable", want: transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE},
		{raw: "REFUND_STATUS_MANUAL_CLAIMABLE", want: transwapv1.RefundStatus_REFUND_STATUS_MANUAL_CLAIMABLE},
		{raw: "6", want: transwapv1.RefundStatus_REFUND_STATUS_CLAIMED},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := parseRefundStatus(tc.raw)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	_, err := parseRefundStatus("unknown")
	require.Error(t, err)
	_, err = parseRefundStatus("99")
	require.Error(t, err)
	_, err = parseRefundStatus("-1")
	require.Error(t, err)
	_, err = parseRefundStatus("2147483648")
	require.Error(t, err)
}

func TestRefundQueryCommandArguments(t *testing.T) {
	refund := GetCmdQueryRefund()
	require.Error(t, refund.Args(refund, nil))
	require.NoError(t, refund.Args(refund, []string{"transwap/channel-7/42"}))
	require.Error(t, refund.Args(refund, []string{"one", "two"}))

	refunds := GetCmdQueryRefunds()
	require.NoError(t, refunds.Args(refunds, nil))
	require.Error(t, refunds.Args(refunds, []string{"unexpected"}))
}

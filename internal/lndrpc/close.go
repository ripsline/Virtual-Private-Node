package lndrpc

import (
	"fmt"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
)

// CloseChannelResult holds the outcome of a channel
// close request.
type CloseChannelResult struct {
	ClosingTxid string
}

// CloseChannel initiates a channel close. If force is
// true, a unilateral close is performed. The function
// blocks until the closing transaction is broadcast
// (ClosePending update) and returns the closing txid.
func (c *Client) CloseChannel(
	channelPoint string,
	force bool,
	satPerVbyte uint64,
) (*CloseChannelResult, error) {
	rpc := c.rpc()
	if rpc == nil {
		return nil, errNotConnected
	}

	// The channel point goes through the same strict parser
	// as every other fund-moving call site: a close must
	// target exactly the output the operator chose, so a
	// value that does not parse aborts the request instead
	// of being reinterpreted. LND accepts the txid in its
	// display form here, so no byte order handling is
	// needed.
	op, err := parseOutpoint(channelPoint)
	if err != nil {
		return nil, err
	}

	req := &lnrpc.CloseChannelRequest{
		ChannelPoint: &lnrpc.ChannelPoint{
			FundingTxid: &lnrpc.ChannelPoint_FundingTxidStr{
				FundingTxidStr: op.TxidStr,
			},
			OutputIndex: op.OutputIndex,
		},
		Force: force,
	}
	if satPerVbyte > 0 {
		req.SatPerVbyte = satPerVbyte
	}

	// Use a long timeout — force closes can take
	// time and cooperative closes need peer
	// communication over Tor
	ctx, cancel := c.callCtx(120 * time.Second)
	defer cancel()

	stream, err := rpc.CloseChannel(ctx, req)
	if err != nil {
		c.handleError(err)
		return nil, fmt.Errorf(
			"close channel: %w", err)
	}

	// Wait for the first update (ClosePending)
	// which contains the closing transaction txid
	for {
		update, err := stream.Recv()
		if err != nil {
			return nil, fmt.Errorf(
				"close stream: %w", err)
		}

		switch u := update.Update.(type) {
		case *lnrpc.CloseStatusUpdate_ClosePending:
			txid := u.ClosePending.GetTxid()
			// Reverse bytes for display
			txidDisplay := fmt.Sprintf("%x", txid)
			if len(txid) == 32 {
				reversed := make([]byte, 32)
				for i := 0; i < 32; i++ {
					reversed[i] = txid[31-i]
				}
				txidDisplay = fmt.Sprintf(
					"%x", reversed)
			}
			return &CloseChannelResult{
				ClosingTxid: txidDisplay,
			}, nil

		case *lnrpc.CloseStatusUpdate_ChanClose:
			// Channel fully resolved — shouldn't
			// happen before ClosePending but handle
			// it gracefully
			return &CloseChannelResult{
				ClosingTxid: "resolved",
			}, nil
		}
	}
}

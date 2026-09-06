package lndrpc

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
)

type ChannelCloseRequest struct {
	ChannelPoint string
	Force        bool
	SatPerVbyte  uint64
}

// Submitted means the RPC was attempted, not that LND accepted the close.
// A pending transaction is a candidate, and confirmation does not prove that
// force-close outputs have matured or been swept.
type ChannelCloseResult struct {
	Submitted   bool
	Confirmed   bool
	ClosingTxid string
	Err         error
}

type ChannelCloseState struct {
	Found        bool
	Active       bool
	StatusFlags  string
	PendingHTLCs int
	Closing      bool
}

// ChannelCloseState reads only close eligibility facts, without alias lookups.
// Both reads share a timeout, but they are not an atomic snapshot or a lock.
func (c *Client) ChannelCloseState(point string) (ChannelCloseState, error) {
	var state ChannelCloseState
	if c == nil {
		return state, errNotConnected
	}
	if _, err := parseOutpoint(point); err != nil {
		return state, err
	}
	rpc := c.rpc()
	if rpc == nil {
		return state, errNotConnected
	}
	ctx, cancel := c.callCtx(defaultTimeout)
	defer cancel()
	pending, err := rpc.PendingChannels(ctx, &lnrpc.PendingChannelsRequest{})
	if err != nil {
		c.handleError(err)
		return state, err
	}
	if pending == nil {
		return state, errors.New("LND returned no pending-channel state")
	}
	for _, ch := range pending.GetWaitingCloseChannels() {
		if ch.GetChannel().GetChannelPoint() == point {
			state.Closing = true
		}
	}
	for _, ch := range pending.GetPendingForceClosingChannels() {
		if ch.GetChannel().GetChannelPoint() == point {
			state.Closing = true
		}
	}
	channels, err := rpc.ListChannels(ctx, &lnrpc.ListChannelsRequest{})
	if err != nil {
		c.handleError(err)
		return state, err
	}
	if channels == nil {
		return state, errors.New("LND returned no channel state")
	}
	for _, ch := range channels.GetChannels() {
		if ch.GetChannelPoint() == point {
			state.Found = true
			state.Active = ch.GetActive()
			state.StatusFlags = ch.GetChanStatusFlags()
			state.PendingHTLCs = len(ch.GetPendingHtlcs())
			break
		}
	}
	return state, nil
}

// CloseChannel makes one bounded submission and waits for a pending or confirmed
// transaction. Canceling the stream is not evidence that LND canceled the close.
func (c *Client) CloseChannel(input ChannelCloseRequest) ChannelCloseResult {
	if c == nil {
		return ChannelCloseResult{Err: errNotConnected}
	}
	rpc := c.rpc()
	if rpc == nil {
		return ChannelCloseResult{Err: errNotConnected}
	}
	op, err := parseOutpoint(input.ChannelPoint)
	if err != nil {
		return ChannelCloseResult{Err: err}
	}
	if input.SatPerVbyte > math.MaxInt64/1000 || (input.Force && input.SatPerVbyte != 0) {
		return ChannelCloseResult{Err: errors.New("Invalid close fee policy")}
	}
	req := &lnrpc.CloseChannelRequest{
		ChannelPoint: &lnrpc.ChannelPoint{
			FundingTxid: &lnrpc.ChannelPoint_FundingTxidStr{FundingTxidStr: op.TxidStr},
			OutputIndex: op.OutputIndex,
		},
		Force: input.Force, SatPerVbyte: input.SatPerVbyte,
	}
	ctx, cancel := c.callCtx(120 * time.Second)
	defer cancel()
	result := ChannelCloseResult{Submitted: true}
	stream, err := rpc.CloseChannel(ctx, req)
	if err != nil {
		c.handleError(err)
		result.Err = fmt.Errorf("close channel: %w", err)
		return result
	}
	if stream == nil {
		result.Err = errors.New("LND returned no close stream")
		return result
	}
	update, err := stream.Recv()
	if err != nil {
		c.handleError(err)
		result.Err = fmt.Errorf("close stream: %w", err)
		return result
	}
	if update == nil {
		result.Err = errors.New("LND returned an empty close update")
		return result
	}
	var raw []byte
	switch u := update.Update.(type) {
	case *lnrpc.CloseStatusUpdate_ClosePending:
		raw = u.ClosePending.GetTxid()
	case *lnrpc.CloseStatusUpdate_ChanClose:
		if !u.ChanClose.GetSuccess() {
			result.Err = errors.New("LND did not report a successful close")
			return result
		}
		raw = u.ChanClose.GetClosingTxid()
		result.Confirmed = true
	default:
		result.Err = errors.New("LND returned an unexpected close update")
		return result
	}
	if len(raw) != 32 {
		result.Confirmed = false
		result.Err = errors.New("LND returned an invalid closing transaction ID")
		return result
	}
	var reversed [32]byte
	for i := range raw {
		reversed[31-i] = raw[i]
	}
	result.ClosingTxid = hex.EncodeToString(reversed[:])
	return result
}

// NormalizeChannelPoint validates an exact funding outpoint and returns its
// canonical display form for request and identity comparisons.
func NormalizeChannelPoint(point string) (string, error) {
	op, err := parseOutpoint(point)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", strings.ToLower(op.TxidStr), op.OutputIndex), nil
}

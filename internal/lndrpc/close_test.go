package lndrpc

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc"
)

type closeRPC struct {
	lnrpc.LightningClient
	requests                []*lnrpc.CloseChannelRequest
	stream                  lnrpc.Lightning_CloseChannelClient
	err                     error
	closeCtx                context.Context
	pending                 *lnrpc.PendingChannelsResponse
	channels                *lnrpc.ListChannelsResponse
	pendingErr, channelsErr error
	pendingCtx, channelsCtx context.Context
}

func (r *closeRPC) CloseChannel(ctx context.Context, req *lnrpc.CloseChannelRequest, _ ...grpc.CallOption) (lnrpc.Lightning_CloseChannelClient, error) {
	r.requests = append(r.requests, req)
	r.closeCtx = ctx
	return r.stream, r.err
}
func (r *closeRPC) PendingChannels(ctx context.Context, _ *lnrpc.PendingChannelsRequest, _ ...grpc.CallOption) (*lnrpc.PendingChannelsResponse, error) {
	r.pendingCtx = ctx
	return r.pending, r.pendingErr
}
func (r *closeRPC) ListChannels(ctx context.Context, _ *lnrpc.ListChannelsRequest, _ ...grpc.CallOption) (*lnrpc.ListChannelsResponse, error) {
	r.channelsCtx = ctx
	return r.channels, r.channelsErr
}

type closeStream struct {
	grpc.ClientStream
	update *lnrpc.CloseStatusUpdate
	err    error
}

func (s *closeStream) Recv() (*lnrpc.CloseStatusUpdate, error) { return s.update, s.err }

func TestCloseWireIntentAndBounds(t *testing.T) {
	point := strings.Repeat("ab", 32) + ":7"
	rpc := &closeRPC{stream: &closeStream{err: io.EOF}}
	client := &Client{lightning: rpc}
	for _, input := range []ChannelCloseRequest{{ChannelPoint: point}, {ChannelPoint: point, SatPerVbyte: 21}, {ChannelPoint: point, Force: true}} {
		result := client.CloseChannel(input)
		req := rpc.requests[len(rpc.requests)-1]
		if !result.Submitted || result.Err == nil || req.ChannelPoint.GetFundingTxidStr() != strings.Repeat("ab", 32) || req.ChannelPoint.OutputIndex != 7 || req.Force != input.Force || req.SatPerVbyte != input.SatPerVbyte {
			t.Fatalf("changed close intent: %+v", req)
		}
		if req.NoWait || req.TargetConf != 0 || req.MaxFeePerVbyte != 0 || req.DeliveryAddress != "" {
			t.Fatal("introduced an unapproved close policy")
		}
		deadline, ok := rpc.closeCtx.Deadline()
		if !ok || time.Until(deadline) > 120*time.Second || rpc.closeCtx.Err() != context.Canceled {
			t.Fatal("close stream lost bounded lifetime")
		}
	}
	for _, input := range []ChannelCloseRequest{{ChannelPoint: "bad"}, {ChannelPoint: point, Force: true, SatPerVbyte: 1}, {ChannelPoint: point, SatPerVbyte: math.MaxInt64/1000 + 1}} {
		result := client.CloseChannel(input)
		if result.Submitted || result.Err == nil || len(rpc.requests) != 3 {
			t.Fatal("invalid close reached RPC")
		}
	}
}
func TestCloseStreamOutcomes(t *testing.T) {
	raw := make([]byte, 32)
	raw[0] = 1
	raw[31] = 2
	want := "02" + strings.Repeat("00", 30) + "01"
	pending := func(tx []byte) *lnrpc.CloseStatusUpdate {
		return &lnrpc.CloseStatusUpdate{Update: &lnrpc.CloseStatusUpdate_ClosePending{ClosePending: &lnrpc.PendingUpdate{Txid: tx}}}
	}
	confirmed := func(success bool) *lnrpc.CloseStatusUpdate {
		return &lnrpc.CloseStatusUpdate{Update: &lnrpc.CloseStatusUpdate_ChanClose{ChanClose: &lnrpc.ChannelCloseUpdate{ClosingTxid: raw, Success: success}}}
	}
	for _, tc := range []struct {
		name             string
		stream           *closeStream
		rpcErr           error
		valid, confirmed bool
	}{
		{"pending", &closeStream{update: pending(raw)}, nil, true, false},
		{"confirmed", &closeStream{update: confirmed(true)}, nil, true, true},
		{"unsuccessful final", &closeStream{update: confirmed(false)}, nil, false, false},
		{"short ID", &closeStream{update: pending([]byte{1})}, nil, false, false},
		{"empty ID", &closeStream{update: pending(nil)}, nil, false, false},
		{"EOF", &closeStream{err: io.EOF}, nil, false, false},
		{"deadline", &closeStream{err: context.DeadlineExceeded}, nil, false, false},
		{"RPC error", nil, errors.New("daemon refused"), false, false},
		{"missing stream", nil, nil, false, false},
		{"empty update", &closeStream{}, nil, false, false},
		{"unexpected update", &closeStream{update: &lnrpc.CloseStatusUpdate{Update: &lnrpc.CloseStatusUpdate_CloseInstant{CloseInstant: &lnrpc.InstantUpdate{}}}}, nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rpc := &closeRPC{err: tc.rpcErr}
			if tc.stream != nil {
				rpc.stream = tc.stream
			}
			result := (&Client{lightning: rpc}).CloseChannel(ChannelCloseRequest{ChannelPoint: strings.Repeat("a", 64) + ":0"})
			if !result.Submitted || len(rpc.requests) != 1 {
				t.Fatal("lost attempted submission or retried")
			}
			if tc.valid {
				if result.Err != nil || result.ClosingTxid != want || result.Confirmed != tc.confirmed {
					t.Fatalf("bad closing transaction: %+v", result)
				}
			} else if result.Err == nil || result.ClosingTxid != "" || result.Confirmed {
				t.Fatalf("malformed/failed update claimed success: %+v", result)
			}
		})
	}
}
func TestCloseStateReadsExactChannelAndFailsClosed(t *testing.T) {
	point := strings.Repeat("a", 64) + ":7"
	for _, tc := range []struct {
		name          string
		change        func(*closeRPC)
		closing, fail bool
	}{
		{"normal", func(*closeRPC) {}, false, false},
		{"waiting", func(r *closeRPC) {
			r.pending.WaitingCloseChannels = []*lnrpc.PendingChannelsResponse_WaitingCloseChannel{{Channel: &lnrpc.PendingChannelsResponse_PendingChannel{ChannelPoint: point}}}
		}, true, false},
		{"force", func(r *closeRPC) {
			r.pending.PendingForceClosingChannels = []*lnrpc.PendingChannelsResponse_ForceClosedChannel{{Channel: &lnrpc.PendingChannelsResponse_PendingChannel{ChannelPoint: point}}}
		}, true, false},
		{"pending error", func(r *closeRPC) { r.pendingErr = errors.New("read failed") }, false, true},
		{"channels error", func(r *closeRPC) { r.channelsErr = errors.New("read failed") }, false, true},
		{"empty pending", func(r *closeRPC) { r.pending = nil }, false, true},
		{"empty channels", func(r *closeRPC) { r.channels = nil }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rpc := &closeRPC{pending: &lnrpc.PendingChannelsResponse{}, channels: &lnrpc.ListChannelsResponse{Channels: []*lnrpc.Channel{
				{ChannelPoint: strings.Repeat("a", 64) + ":0", Active: true, ChanStatusFlags: "other"},
				{ChannelPoint: point, Active: false, ChanStatusFlags: "ChanStatusDefault", PendingHtlcs: []*lnrpc.HTLC{{}}},
			}}}
			tc.change(rpc)
			state, err := (&Client{lightning: rpc}).ChannelCloseState(point)
			if tc.fail {
				if err == nil {
					t.Fatal("failed read became eligible")
				}
				return
			}
			if err != nil || !state.Found || state.Active || state.StatusFlags != "ChanStatusDefault" || state.PendingHTLCs != 1 || state.Closing != tc.closing {
				t.Fatalf("wrong channel state: %+v %v", state, err)
			}
			if rpc.pendingCtx != rpc.channelsCtx || rpc.pendingCtx.Err() != context.Canceled {
				t.Fatal("state reads lost shared bounded lifetime")
			}
			state, err = (&Client{lightning: rpc}).ChannelCloseState(strings.Repeat("c", 64) + ":7")
			if err != nil || state.Found || state.Closing {
				t.Fatal("substituted another channel")
			}
		})
	}
	rpc := &closeRPC{}
	if _, err := (&Client{lightning: rpc}).ChannelCloseState("bad"); err == nil || rpc.pendingCtx != nil {
		t.Fatal("invalid identity reached read RPC")
	}
	var disconnected *Client
	if _, err := disconnected.ChannelCloseState(point); err == nil {
		t.Fatal("nil client read accepted")
	}
	if result := disconnected.CloseChannel(ChannelCloseRequest{ChannelPoint: point}); result.Submitted || result.Err == nil {
		t.Fatal("nil client submission accepted")
	}
}

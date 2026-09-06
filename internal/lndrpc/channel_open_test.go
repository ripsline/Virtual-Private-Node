package lndrpc

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc"
)

type channelOpenRPC struct {
	lnrpc.LightningClient
	requests []*lnrpc.OpenChannelRequest
	response *lnrpc.ChannelPoint
	err      error
}

func (c *channelOpenRPC) OpenChannelSync(_ context.Context, req *lnrpc.OpenChannelRequest, _ ...grpc.CallOption) (*lnrpc.ChannelPoint, error) {
	c.requests = append(c.requests, req)
	return c.response, c.err
}

func TestChannelFundingWireBoundary(t *testing.T) {
	rpc := &channelOpenRPC{}
	client := &Client{lightning: rpc}
	input := ChannelOpenRequest{
		Pubkey:     "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798",
		AmountSats: 250000, Private: true, Taproot: true, SatPerVbyte: 12,
		Outpoints: []string{strings.Repeat("ab", 32) + ":7"}, SpendUnconfirmed: true,
	}
	for _, max := range []bool{false, true} {
		input.FundMax = max
		if max {
			input.AmountSats = 0
			input.SatPerVbyte = 0
			input.Taproot = false
			input.Private = false
		}
		client.OpenChannel(input)
		req := rpc.requests[len(rpc.requests)-1]
		wantType := lnrpc.CommitmentType_TAPROOT
		if max {
			wantType = lnrpc.CommitmentType_UNKNOWN_COMMITMENT_TYPE
		}
		if req.CommitmentType != wantType || req.FundMax != max || req.LocalFundingAmount != input.AmountSats ||
			req.SatPerVbyte != input.SatPerVbyte || req.Private != input.Private || req.ScidAlias != input.Private ||
			req.MinConfs != 0 || !req.SpendUnconfirmed || len(req.Outpoints) != 1 ||
			req.Outpoints[0].TxidStr != strings.Repeat("ab", 32) || req.Outpoints[0].OutputIndex != 7 {
			t.Fatalf("wire request changed the funding intent: %+v", req)
		}
	}
	for _, invalid := range []ChannelOpenRequest{
		{Pubkey: input.Pubkey},
		{Pubkey: input.Pubkey, Outpoints: []string{input.Outpoints[0], "malformed"}},
		{Pubkey: input.Pubkey, Outpoints: []string{input.Outpoints[0], input.Outpoints[0]}},
		{Pubkey: input.Pubkey, Outpoints: input.Outpoints, Taproot: true},
		{Pubkey: "invalid", Outpoints: input.Outpoints},
		{Pubkey: input.Pubkey, Outpoints: input.Outpoints, FundMax: true, AmountSats: 250000},
	} {
		result := client.OpenChannel(invalid)
		if result.Submitted || result.Err == nil || len(rpc.requests) != 2 {
			t.Fatalf("invalid request reached funding RPC: %+v", result)
		}
	}
}

func TestChannelFundingResultBoundary(t *testing.T) {
	raw := make([]byte, 32)
	raw[0], raw[31] = 1, 2
	valid := &lnrpc.ChannelPoint{FundingTxid: &lnrpc.ChannelPoint_FundingTxidBytes{FundingTxidBytes: raw}, OutputIndex: 7}
	input := ChannelOpenRequest{Pubkey: "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798", AmountSats: 250000,
		Outpoints: []string{strings.Repeat("ab", 32) + ":7"}}
	for _, tc := range []struct {
		name     string
		response *lnrpc.ChannelPoint
		err      error
		success  bool
	}{
		{"broadcast", valid, nil, true}, {"lost response", nil, errors.New("connection lost"), false},
		{"empty response", nil, nil, false}, {"missing ID", &lnrpc.ChannelPoint{}, nil, false},
		{"short ID", &lnrpc.ChannelPoint{FundingTxid: &lnrpc.ChannelPoint_FundingTxidBytes{FundingTxidBytes: []byte{1}}}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rpc := &channelOpenRPC{response: tc.response, err: tc.err}
			result := (&Client{lightning: rpc}).OpenChannel(input)
			if !result.Submitted || len(rpc.requests) != 1 {
				t.Fatal("lost funding call ownership")
			}
			if tc.success {
				if result.Err != nil || result.OutputIndex != 7 || result.FundingTxID != "02"+strings.Repeat("00", 30)+"01" {
					t.Fatalf("bad funding result: %+v", result)
				}
			} else if result.Err == nil || result.FundingTxID != "" {
				t.Fatalf("invalid response reported as broadcast: %+v", result)
			}
		})
	}
}

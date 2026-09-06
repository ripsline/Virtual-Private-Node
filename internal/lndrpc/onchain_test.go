package lndrpc

import (
	"context"
	"strings"
	"testing"

	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc"
)

type sendCoinsRPC struct {
	lnrpc.LightningClient
	requests []*lnrpc.SendCoinsRequest
}

func (c *sendCoinsRPC) SendCoins(_ context.Context, req *lnrpc.SendCoinsRequest, _ ...grpc.CallOption) (*lnrpc.SendCoinsResponse, error) {
	c.requests = append(c.requests, req)
	return &lnrpc.SendCoinsResponse{Txid: "broadcast"}, nil
}

func TestSendCoinsWireBoundaryPreservesIntentAndRejectsMalformedSelection(t *testing.T) {
	rpc := &sendCoinsRPC{}
	client := &Client{lightning: rpc}
	input := SendCoinsRequest{Address: "destination", AmountSats: 1000, SatPerVbyte: 9, Label: "reviewed", SpendUnconfirmed: true,
		Outpoints: []string{strings.Repeat("a", 64) + ":7"}}
	for _, sendAll := range []bool{false, true} {
		input.SendAll = sendAll
		if sendAll {
			input.AmountSats = 0
		}
		if _, err := client.SendCoins(input); err != nil {
			t.Fatal(err)
		}
		req := rpc.requests[len(rpc.requests)-1]
		if req.Amount != input.AmountSats || req.SendAll != sendAll || req.Addr != input.Address || req.Label != input.Label ||
			req.SatPerVbyte != 9 || req.MinConfs != 0 || !req.SpendUnconfirmed || len(req.Outpoints) != 1 ||
			req.Outpoints[0].TxidStr != strings.Repeat("a", 64) || req.Outpoints[0].OutputIndex != 7 {
			t.Fatalf("wire request changes reviewed intent: %+v", req)
		}
	}
	input.Outpoints = append(input.Outpoints, "malformed")
	if _, err := client.SendCoins(input); err == nil || len(rpc.requests) != 2 {
		t.Fatal("malformed selection reached send RPC")
	}
}

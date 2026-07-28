// internal/lndrpc/client_test.go

package lndrpc

import (
	"fmt"
	"testing"
)

func TestNodeInfoFields(t *testing.T) {
	info := &NodeInfo{
		Pubkey: "02abc123", Alias: "mynode", Channels: 5,
		Peers: 10, BlockHeight: 850000, SyncedChain: true,
		SyncedGraph: true, Version: "0.20.0-beta",
	}
	if info.Channels != 5 {
		t.Errorf("Channels: got %d", info.Channels)
	}
}

func TestWalletBalanceFields(t *testing.T) {
	bal := &WalletBalance{TotalBalance: "1000000"}
	if bal.TotalBalance != "1000000" {
		t.Errorf("TotalBalance: got %q", bal.TotalBalance)
	}
}

func TestChannelFields(t *testing.T) {
	ch := Channel{Capacity: 1000000, LocalBalance: 600000, Active: true, PeerAlias: "ACINQ"}
	if ch.Capacity != 1000000 {
		t.Errorf("Capacity: got %d", ch.Capacity)
	}
	if ch.PeerAlias != "ACINQ" {
		t.Errorf("PeerAlias: got %q", ch.PeerAlias)
	}
}

func TestPendingChannelInfoFields(t *testing.T) {
	info := &PendingChannelInfo{PendingOpen: 2}
	if info.PendingOpen != 2 {
		t.Errorf("PendingOpen: got %d", info.PendingOpen)
	}
}

func TestSatStr(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"}, {1000000, "1000000"}, {-500, "-500"},
	}
	for _, tt := range tests {
		if got := satStr(tt.input); got != tt.want {
			t.Errorf("satStr(%d): got %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNilClientSafety(t *testing.T) {
	c := &Client{}
	if _, err := c.GetInfo(); err == nil {
		t.Error("should error")
	}
	if _, err := c.GetWalletBalance(); err == nil {
		t.Error("should error")
	}
	if _, err := c.ListChannels(); err == nil {
		t.Error("should error")
	}
	if _, err := c.GetPendingChannels(); err == nil {
		t.Error("should error")
	}
	if _, err := c.GetNewAddress(); err == nil {
		t.Error("should error")
	}
	if _, err := c.ListPeers(); err == nil {
		t.Error("should error")
	}
	if err := c.ConnectPeer("a", "b"); err == nil {
		t.Error("should error")
	}
	if _, err := c.OpenChannel("a", 100000, false, false, nil, false, 0); err == nil {
		t.Error("should error")
	}
}

// The staged-certificate self-heal keys off this classifier: a
// TLS verification failure means the board copy may be stale; a
// down service or a locked wallet must not trigger a re-stage.
func TestIsCertificateError(t *testing.T) {
	certErrs := []string{
		"rpc error: code = Unavailable desc = connection error: " +
			"desc = \"transport: authentication handshake failed: " +
			"tls: failed to verify certificate: x509: " +
			"certificate signed by unknown authority\"",
		"x509: certificate is valid for 127.0.0.1, not ::1",
		"tls: bad certificate",
	}
	for _, msg := range certErrs {
		if !isCertificateError(errFromString(msg)) {
			t.Errorf("not classified as certificate error: %q", msg)
		}
	}
	otherErrs := []string{
		"rpc error: code = Unavailable desc = connection refused",
		// Observed in production when the client dialed LND by
		// the name localhost on a box with IPv6 disabled: the
		// name resolved to ::1 and the kernel refused the
		// address. An address-family failure, not a stale
		// certificate — a re-stage must never fire on it.
		"rpc error: code = Unavailable desc = connection error: " +
			"desc = \"transport: Error while dialing: dial tcp " +
			"[::1]:10009: connect: cannot assign requested " +
			"address\"",
		"rpc error: code = Unimplemented desc = unknown service " +
			"lnrpc.Lightning",
		"wallet locked, unlock it to enable full RPC access",
		"context deadline exceeded",
	}
	for _, msg := range otherErrs {
		if isCertificateError(errFromString(msg)) {
			t.Errorf("wrongly classified as certificate error: %q",
				msg)
		}
	}
	if isCertificateError(nil) {
		t.Error("nil classified as certificate error")
	}
}

func errFromString(msg string) error {
	return fmt.Errorf("%s", msg)
}

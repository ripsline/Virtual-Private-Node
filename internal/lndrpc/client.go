// internal/lndrpc/client.go

// Package lndrpc provides a gRPC client for LND.
//
// The client reads the TLS certificate and admin macaroon from
// the staging board — root-staged copies the admin user reads
// directly, no privileged operation on the read path — and
// holds the macaroon in memory for the duration of the process.
// The macaroon is injected into every gRPC call as metadata.
//
// Connection uses TLS to the loopback address. The macaroon
// never crosses the network. When the TUI process exits, the
// macaroon is gone from memory.
//
// This package only performs read operations. Fund-moving RPCs
// (SendPayment, OpenChannel, etc.) are added in later changes
// with explicit confirmation flows.
package lndrpc

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/lightningnetwork/lnd/lnrpc"

	"github.com/virtualprivatenode/vpn/internal/helper"
	"github.com/virtualprivatenode/vpn/internal/logger"
	"github.com/virtualprivatenode/vpn/internal/paths"
)

// Client wraps an LND gRPC connection with macaroon authentication.
type Client struct {
	conn        *grpc.ClientConn
	lightning   lnrpc.LightningClient
	macaroonHex string
	network     string
	mu          sync.RWMutex
}

// New creates a new LND gRPC client. It reads the TLS certificate
// and admin macaroon, establishes the connection, and verifies it
// with a GetInfo call.
//
// Returns a client even if LND is not available — RPC methods
// check for a live connection internally and return
// errNotConnected if the connection is nil.
func New(network string) *Client {
	c := &Client{network: network}
	if err := c.connect(); err != nil {
		logger.Status("LND gRPC not available: %v", err)
		return c
	}
	return c
}

func (c *Client) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dial(true)
}

// dial establishes the connection; the caller holds c.mu.
//
// allowHeal enables the staged-certificate self-heal: LND can
// regenerate its TLS certificate OUTSIDE any helper operation —
// an automatic restart after a crash, a reboot, a config change
// applied by a reinstall — and the staged board copy then no
// longer matches the certificate LND serves. That staleness
// reliably surfaces exactly here, as a TLS verification failure
// on the test call. On one, the client asks the helper to
// re-stage the LND credentials (rate-limited process-wide) and
// dials again once with the refreshed board copy.
func (c *Client) dial(allowHeal bool) error {
	// Read the staged TLS cert copy (fail-noisy: a missing
	// staged fact names itself and points at the journal).
	certData, err := helper.ReadBoard(paths.StateLNDTLSCert)
	if err != nil {
		return fmt.Errorf("read TLS cert: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(certData) {
		return fmt.Errorf("failed to parse TLS cert")
	}

	tlsCreds := credentials.NewClientTLSFromCert(certPool, "")

	// Read the staged admin macaroon copy (staged at wallet
	// creation; re-staged whenever an operation invalidates it).
	macBytes, err := helper.ReadBoard(paths.StateLNDMacaroon)
	if err != nil {
		return fmt.Errorf("read macaroon: %w", err)
	}
	c.macaroonHex = hex.EncodeToString(macBytes)

	// Connect
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(tlsCreds),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(50 * 1024 * 1024)),
	}

	// Dial by the shared loopback constant — the same value
	// lnd.conf binds to. Never by the name localhost: with
	// IPv6 disabled on the node, that name can resolve to an
	// IPv6 address the connection cannot use.
	conn, err := grpc.NewClient(paths.LNDGRPCEndpoint, opts...)
	if err != nil {
		return fmt.Errorf("grpc connect: %w", err)
	}

	c.conn = conn
	c.lightning = lnrpc.NewLightningClient(conn)

	// Test the connection with a longer timeout.
	// During IBD, LND's GetInfo queries Bitcoin Core which can be slow.
	ctx, cancel := context.WithTimeout(c.macaroonCtx(), 30*time.Second)
	defer cancel()

	_, err = c.lightning.GetInfo(ctx, &lnrpc.GetInfoRequest{})
	if err == nil {
		logger.Status("LND gRPC connected and ready")
		return nil
	}
	if allowHeal && isCertificateError(err) &&
		requestCredentialRestage() {
		logger.Status("LND gRPC: staged TLS certificate looks "+
			"stale (%v) — re-staged, reconnecting", err)
		c.conn.Close()
		c.conn = nil
		c.lightning = nil
		return c.dial(false)
	}
	logger.Status("LND gRPC connected, waiting for RPC ready: %v", err)
	return nil
}

// isCertificateError reports whether a gRPC connect error looks
// like a TLS certificate verification failure (as opposed to a
// down service, a locked wallet, or a plain timeout). Matching
// on the error text is unavoidable — grpc flattens the tls/x509
// error chain into a string. Pure — unit-tested.
func isCertificateError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "x509") ||
		strings.Contains(msg, "certificate")
}

// Certificate self-heal state: at most one re-stage request per
// interval, process-wide — a wrong classification must not turn
// every reconnect poll into helper traffic.
var (
	certRestageMu sync.Mutex
	certRestageAt time.Time
)

const certRestageInterval = 5 * time.Minute

// requestCredentialRestage asks the helper to refresh the staged
// LND credentials from current reality. Reports whether a
// re-stage actually happened (rate limit passed and the helper
// succeeded), so the caller only re-dials when the board copy
// could have changed.
func requestCredentialRestage() bool {
	certRestageMu.Lock()
	defer certRestageMu.Unlock()
	if !certRestageAt.IsZero() &&
		time.Since(certRestageAt) < certRestageInterval {
		return false
	}
	certRestageAt = time.Now()
	if err := helper.Call(
		helper.VerbStageLNDCredentials, nil, nil); err != nil {
		logger.Status("stage-lnd-credentials: %v", err)
		return false
	}
	return true
}

// Reconnect attempts to re-establish the gRPC connection.
// Called when an RPC fails, indicating LND may have restarted.
func (c *Client) Reconnect() {
	c.mu.Lock()
	if c.conn != nil {
		c.conn.Close()
	}
	c.conn = nil
	c.lightning = nil
	c.mu.Unlock()

	if err := c.connect(); err != nil {
		logger.Status("LND gRPC reconnect failed: %v", err)
	}
}

// Close shuts down the gRPC connection.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
		c.lightning = nil
	}
}

// macaroonCtx returns a context with the macaroon injected as gRPC metadata.
func (c *Client) macaroonCtx() context.Context {
	md := metadata.New(map[string]string{
		"macaroon": c.macaroonHex,
	})
	return metadata.NewOutgoingContext(context.Background(), md)
}

// callCtx returns a context with macaroon and a timeout.
func (c *Client) callCtx(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.macaroonCtx(), timeout)
}

// rpc returns the Lightning client, or nil if not connected.
func (c *Client) rpc() lnrpc.LightningClient {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lightning
}

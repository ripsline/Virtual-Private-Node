// internal/config/network.go

package config

import (
	"fmt"
	"strings"
)

const (
	NetworkMainnet      = "mainnet"
	NetworkTestnet4     = "testnet4"
	NetworkPublicSignet = "public-signet"
)

// The public signet identity is part of the installation contract, not merely
// a command-line selector. A custom challenge must never be substituted here:
// any future controlled signet is a separate immutable profile.
const (
	PublicSignetGenesis   = "00000008819873e925422c1ff0f99f7cc9bbb232af63a077a480a3633bee1ef6"
	PublicSignetChallenge = "512103ad5e0edad18cb1f0fc0d28a3d4f1f3e445640337489abb10404f2d1e086be430210359ef5021964fe22d6f8e05b2463c9540ce96883fe3b278760f048f5189f2e6c452ae"
)

// NetworkConfig is the closed, durable deployment profile shared by the
// installer, lifecycle authority, helpers, wrappers, and TUI. Name is VPN's
// immutable profile identifier. CoreNetwork and LNDNetwork are deliberately
// separate upstream identifiers: public-signet maps to "signet" in both
// daemons and must never use a /public-signet daemon-state directory.
type NetworkConfig struct {
	Name                    string
	DisplayName             string
	TestingOnly             bool
	BitcoinFlag             string
	BitcoinCLIFlag          string
	CoreNetwork             string
	CoreDataDir             string
	ExpectedGenesis         string
	ExpectedSignetChallenge string
	LNDBitcoinFlag          string
	LNDNetwork              string
	RPCPort                 int
	P2PPort                 int
	ZMQBlockPort            int
	ZMQTxPort               int
	Bech32HRP               string
	Base58Prefixes          string
	InvoicePrefix           string
	AddressPlaceholder      string
	InvoicePlaceholder      string
}

var networkConfigs = map[string]NetworkConfig{
	NetworkMainnet: {
		Name:               NetworkMainnet,
		DisplayName:        "Mainnet",
		CoreNetwork:        "main",
		ExpectedGenesis:    "000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f",
		LNDBitcoinFlag:     "bitcoin.mainnet=true",
		LNDNetwork:         "mainnet",
		RPCPort:            8332,
		P2PPort:            8333,
		ZMQBlockPort:       28332,
		ZMQTxPort:          28333,
		Bech32HRP:          "bc",
		Base58Prefixes:     "13",
		InvoicePrefix:      "lnbc",
		AddressPlaceholder: "bc1p...",
		InvoicePlaceholder: "lnbc...",
	},
	NetworkTestnet4: {
		Name:               NetworkTestnet4,
		DisplayName:        "Testnet4",
		TestingOnly:        true,
		BitcoinFlag:        "testnet4=1",
		BitcoinCLIFlag:     "-testnet4",
		CoreNetwork:        "testnet4",
		CoreDataDir:        "testnet4",
		ExpectedGenesis:    "00000000da84f2bafbbc53dee25a72ae507ff4914b867c565be350b0da8bf043",
		LNDBitcoinFlag:     "bitcoin.testnet4=true",
		LNDNetwork:         "testnet4",
		RPCPort:            48332,
		P2PPort:            48333,
		ZMQBlockPort:       28334,
		ZMQTxPort:          28335,
		Bech32HRP:          "tb",
		Base58Prefixes:     "2mn",
		InvoicePrefix:      "lntb",
		AddressPlaceholder: "tb1p...",
		InvoicePlaceholder: "lntb...",
	},
	NetworkPublicSignet: {
		Name:                    NetworkPublicSignet,
		DisplayName:             "Public signet",
		TestingOnly:             true,
		BitcoinFlag:             "signet=1",
		BitcoinCLIFlag:          "-signet",
		CoreNetwork:             "signet",
		CoreDataDir:             "signet",
		ExpectedGenesis:         PublicSignetGenesis,
		ExpectedSignetChallenge: PublicSignetChallenge,
		LNDBitcoinFlag:          "bitcoin.signet=true",
		LNDNetwork:              "signet",
		RPCPort:                 38332,
		P2PPort:                 38333,
		ZMQBlockPort:            28336,
		ZMQTxPort:               28337,
		Bech32HRP:               "tb",
		Base58Prefixes:          "2mn",
		InvoicePrefix:           "lntbs",
		AddressPlaceholder:      "tb1p...",
		InvoicePlaceholder:      "lntbs...",
	},
}

// SupportedNetworks returns the immutable profile identifiers in stable
// product order. The returned slice is a new value and may be modified by the
// caller without changing the profile table.
func SupportedNetworks() []string {
	return []string{NetworkMainnet, NetworkTestnet4, NetworkPublicSignet}
}

func NetworkConfigFromName(name string) (*NetworkConfig, error) {
	net, ok := networkConfigs[name]
	if !ok {
		return nil, fmt.Errorf(
			"unknown network %q: must be %s",
			name, strings.Join(SupportedNetworks(), ", "))
	}
	copy := net
	return &copy, nil
}

func ValidateNetwork(name string) error {
	_, err := NetworkConfigFromName(name)
	return err
}

// AcceptsOnChainAddress performs the TUI's deliberately shallow network
// prefix check. LND remains the authoritative full decoder. Unknown profiles
// cannot reach this method because lookup fails closed.
func (n *NetworkConfig) AcceptsOnChainAddress(address string) bool {
	if strings.HasPrefix(strings.ToLower(address), n.Bech32HRP+"1") {
		return true
	}
	if address == "" || !strings.ContainsRune(n.Base58Prefixes, rune(address[0])) {
		return false
	}
	return true
}

// AcceptsInvoicePrefix distinguishes testnet4's lntb HRP from public signet's
// lntbs HRP. A plain HasPrefix check is unsafe because every lntbs invoice also
// begins with lntb. After the network prefix, BOLT11 permits either the bech32
// separator (amountless invoice) or a decimal amount.
func (n *NetworkConfig) AcceptsInvoicePrefix(invoice string) bool {
	invoice = strings.ToLower(invoice)
	if !strings.HasPrefix(invoice, n.InvoicePrefix) ||
		len(invoice) == len(n.InvoicePrefix) {
		return false
	}
	next := invoice[len(n.InvoicePrefix)]
	return next == '1' || (next >= '0' && next <= '9')
}

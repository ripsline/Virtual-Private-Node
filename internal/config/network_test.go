// internal/config/network_test.go

package config

import "testing"

func TestNetworkProfiles(t *testing.T) {
	tests := []struct {
		name       string
		core       string
		lnd        string
		invoice    string
		rpc, p2p   int
		zmqB, zmqT int
		testing    bool
	}{
		{NetworkMainnet, "main", "mainnet", "lnbc", 8332, 8333, 28332, 28333, false},
		{NetworkTestnet4, "testnet4", "testnet4", "lntb", 48332, 48333, 28334, 28335, true},
		{NetworkPublicSignet, "signet", "signet", "lntbs", 38332, 38333, 28336, 28337, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			net, err := NetworkConfigFromName(tt.name)
			if err != nil {
				t.Fatal(err)
			}
			if net.Name != tt.name || net.CoreNetwork != tt.core ||
				net.LNDNetwork != tt.lnd || net.InvoicePrefix != tt.invoice ||
				net.RPCPort != tt.rpc || net.P2PPort != tt.p2p ||
				net.ZMQBlockPort != tt.zmqB || net.ZMQTxPort != tt.zmqT ||
				net.TestingOnly != tt.testing {
				t.Fatalf("unexpected profile: %+v", net)
			}
			if net.ExpectedGenesis == "" || net.LNDBitcoinFlag == "" ||
				net.AddressPlaceholder == "" ||
				net.InvoicePlaceholder == "" {
				t.Fatalf("incomplete profile: %+v", net)
			}
		})
	}
}

func TestNetworkLookupFailsClosed(t *testing.T) {
	for _, name := range []string{"", "bogus", "testnet", "signet", "controlled-signet-v1"} {
		if net, err := NetworkConfigFromName(name); err == nil || net != nil {
			t.Errorf("NetworkConfigFromName(%q) = %+v, %v; want nil plus error", name, net, err)
		}
		if err := ValidateNetwork(name); err == nil {
			t.Errorf("ValidateNetwork(%q) succeeded", name)
		}
	}
}

func TestSupportedNetworksReturnsCopy(t *testing.T) {
	one := SupportedNetworks()
	one[0] = "changed"
	two := SupportedNetworks()
	if two[0] != NetworkMainnet {
		t.Fatalf("profile order mutated: %v", two)
	}
}

func TestPublicSignetIdentityIsPinned(t *testing.T) {
	net, err := NetworkConfigFromName(NetworkPublicSignet)
	if err != nil {
		t.Fatal(err)
	}
	if net.ExpectedGenesis != PublicSignetGenesis ||
		net.ExpectedSignetChallenge != PublicSignetChallenge ||
		net.BitcoinFlag != "signet=1" || net.BitcoinCLIFlag != "-signet" ||
		net.LNDBitcoinFlag != "bitcoin.signet=true" {
		t.Fatalf("public signet identity drifted: %+v", net)
	}
}

func TestInvoicePrefixesDoNotAlias(t *testing.T) {
	tests := []struct {
		network string
		accept  string
		reject  []string
	}{
		{NetworkMainnet, "lnbc1example", []string{"lntb1example", "lntbs1example"}},
		{NetworkTestnet4, "lntb10u1example", []string{"lnbc1example", "lntbs1example"}},
		{NetworkPublicSignet, "lntbs1example", []string{"lnbc1example", "lntb1example"}},
	}
	for _, tt := range tests {
		profile, err := NetworkConfigFromName(tt.network)
		if err != nil {
			t.Fatal(err)
		}
		if !profile.AcceptsInvoicePrefix(tt.accept) {
			t.Errorf("%s rejected %q", tt.network, tt.accept)
		}
		for _, invoice := range tt.reject {
			if profile.AcceptsInvoicePrefix(invoice) {
				t.Errorf("%s accepted %q", tt.network, invoice)
			}
		}
	}
}

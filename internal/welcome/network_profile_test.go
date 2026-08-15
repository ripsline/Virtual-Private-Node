package welcome

import (
	"strings"
	"testing"

	"github.com/virtualprivatenode/vpn/internal/config"
)

func TestOnChainPrefixValidationUsesInstalledProfile(t *testing.T) {
	tests := []struct {
		network string
		accept  string
		reject  string
	}{
		{config.NetworkMainnet, "bc1qexample000", "tb1qexample000"},
		{config.NetworkTestnet4, "tb1qexample000", "bc1qexample000"},
		{config.NetworkPublicSignet, "tb1qexample000", "sb1qexample000"},
	}
	for _, tt := range tests {
		if !isValidOnChainAddr(tt.accept, tt.network) {
			t.Errorf("%s rejected %q", tt.network, tt.accept)
		}
		if isValidOnChainAddr(tt.reject, tt.network) {
			t.Errorf("%s accepted %q", tt.network, tt.reject)
		}
	}
	if isValidOnChainAddr("bc1qexample000", "signet") {
		t.Fatal("unknown raw signet profile accepted an address")
	}
}

func TestNetworkSpecificInputPlaceholders(t *testing.T) {
	tests := []struct {
		network string
		address string
		invoice string
	}{
		{config.NetworkMainnet, "bc1p...", "lnbc..."},
		{config.NetworkTestnet4, "tb1p...", "lntb..."},
		{config.NetworkPublicSignet, "tb1p...", "lntbs..."},
	}
	for _, tt := range tests {
		if got := newOnChainAddrInput(tt.network).Placeholder; got != tt.address {
			t.Errorf("%s address placeholder %q, want %q", tt.network, got, tt.address)
		}
		if got := newSendPayReqInput(tt.network).Placeholder; got != tt.invoice {
			t.Errorf("%s invoice placeholder %q, want %q", tt.network, got, tt.invoice)
		}
	}
}

func TestLightningInvoicePrefilterUsesInstalledProfile(t *testing.T) {
	for _, network := range config.SupportedNetworks() {
		cfg := config.Default()
		cfg.Network = network
		profile, err := cfg.NetworkConfig()
		if err != nil {
			t.Fatal(err)
		}
		screen := NewSendScreen(&ScreenContext{Cfg: cfg})
		screen.sendInput.SetValue(profile.InvoicePrefix + "1example")
		_, cmd := screen.submitSendPayment()
		if cmd == nil || screen.inputError != "" {
			t.Errorf("%s invoice rejected before LND: %q", network, screen.inputError)
		}

		foreign := "lnbc1example"
		if network == config.NetworkMainnet {
			foreign = "lntb1example"
		}
		screen.sendInput.SetValue(foreign)
		_, cmd = screen.submitSendPayment()
		if cmd != nil || !strings.Contains(screen.inputError, "not for") {
			t.Errorf("%s foreign invoice result cmd=%v error=%q",
				network, cmd != nil, screen.inputError)
		}
	}
}

func TestTestingNetworkBanner(t *testing.T) {
	for _, network := range config.SupportedNetworks() {
		cfg := config.Default()
		cfg.Network = network
		m := Model{cfg: cfg}
		banner := m.renderNetworkBanner()
		if network == config.NetworkMainnet && banner != "" {
			t.Errorf("mainnet banner = %q", banner)
		}
		if network != config.NetworkMainnet &&
			(!strings.Contains(banner, "TESTING NETWORK") ||
				!strings.Contains(banner, "no mainnet value")) {
			t.Errorf("%s banner = %q", network, banner)
		}
	}
}

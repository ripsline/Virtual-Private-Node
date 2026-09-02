package welcome

import (
	"testing"

	"charm.land/bubbles/v2/key"
)

func TestWalletHomeHelpPreservesUnknownState(t *testing.T) {
	tests := []struct {
		name string
		new  func(*ScreenContext) interface{ HelpBindings() []key.Binding }
	}{
		{
			"on-chain",
			func(ctx *ScreenContext) interface{ HelpBindings() []key.Binding } {
				return NewOnChainHomeScreen(ctx, &OnChainContext{})
			},
		},
		{
			"off-chain",
			func(ctx *ScreenContext) interface{ HelpBindings() []key.Binding } {
				return NewWalletHomeScreen(ctx)
			},
		},
		{
			"channels",
			func(ctx *ScreenContext) interface{ HelpBindings() []key.Binding } {
				return NewChannelsHomeScreen(ctx)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			unknown := test.new(&ScreenContext{State: &RuntimeState{}})
			if got := unknown.HelpBindings()[0].Help().Desc; got != "retry wallet state" {
				t.Fatalf("unknown wallet help = %q", got)
			}

			absent := test.new(&ScreenContext{
				State: &RuntimeState{WalletKnown: true},
			})
			if got := absent.HelpBindings()[0].Help().Desc; got != "create wallet" {
				t.Fatalf("absent wallet help = %q", got)
			}
		})
	}
}

package tui

import "testing"

func TestChannelOpenRejectsInvalidTaprootPrivacyToggles(t *testing.T) {
	t.Run("public while Taproot", func(t *testing.T) {
		s := &ChannelOpenScreen{
			private:   true,
			taproot:   true,
			toggleIdx: 0,
		}
		s.handleToggleKey("space")
		if !s.private || s.error == "" {
			t.Fatalf("invalid toggle accepted: private=%v error=%q",
				s.private, s.error)
		}
	})

	t.Run("Taproot while public", func(t *testing.T) {
		s := &ChannelOpenScreen{
			private:   false,
			taproot:   false,
			toggleIdx: 1,
		}
		s.handleToggleKey("space")
		if s.taproot || s.error == "" {
			t.Fatalf("invalid toggle accepted: taproot=%v error=%q",
				s.taproot, s.error)
		}
	})
}

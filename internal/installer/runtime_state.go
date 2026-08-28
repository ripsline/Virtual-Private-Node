package installer

import (
	"fmt"
	"os"

	"github.com/virtualprivatenode/vpn/internal/config"
	"github.com/virtualprivatenode/vpn/internal/paths"
)

// WalletExists observes LND's authoritative wallet database rather than a
// duplicated TUI-written boolean. A non-regular or symlinked object is an
// error, not evidence that a wallet exists.
func WalletExists(network string) (bool, error) {
	profile, err := config.NetworkConfigFromName(network)
	if err != nil {
		return false, err
	}
	return walletExistsAt(paths.LNDWalletDB(profile.LNDNetwork))
}

func walletExistsAt(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect LND wallet state: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("LND wallet state %s is not a regular file", path)
	}
	return true, nil
}

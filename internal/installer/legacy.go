package installer

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/virtualprivatenode/vpn/internal/paths"
)

const serviceLayoutMarkerContent = "dedicated-service-identities-v1\n"

// guardFreshServiceLayout is the fail-closed migration boundary.
// The dedicated-layout marker authorizes a fresh install or a
// resume that began under this code. Without it, any durable vpn,
// LND, or Syncthing state means this is an older/shared layout and
// the installer must not chown or rewrite live state.
func guardFreshServiceLayout() error {
	data, err := os.ReadFile(paths.ServiceLayoutMarker)
	switch {
	case err == nil:
		return serviceLayoutConflict(true, string(data), nil)
	case !os.IsNotExist(err):
		return fmt.Errorf("read service-layout marker: %w", err)
	}

	var found []string
	for _, p := range []string{
		paths.ConfigFile,
		paths.BitcoinConf,
		paths.BitcoinDataDir,
		paths.BitcoindService,
		paths.LNDConf,
		paths.LNDDataDir,
		paths.LNDService,
		paths.SyncthingDir,
		paths.SyncthingDataDir,
		paths.SyncthingService,
		paths.StateDir,
	} {
		if _, err := os.Lstat(p); err == nil {
			found = append(found, p)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect legacy layout at %s: %w", p, err)
		}
	}
	// acquireRunLock creates an empty ledger on a truly fresh
	// machine, so only a non-empty file is evidence of a prior
	// installer generation.
	if fi, err := os.Stat(paths.InstallStateFile); err == nil &&
		fi.Size() > 0 {
		found = append(found, paths.InstallStateFile)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect install ledger: %w", err)
	}
	for _, name := range []string{bitcoinUser, lndUser, syncthingUser} {
		if _, err := user.Lookup(name); err == nil {
			found = append(found, "user:"+name)
		} else {
			var unknown user.UnknownUserError
			if !errors.As(err, &unknown) {
				return fmt.Errorf("inspect service user %s: %w", name, err)
			}
		}
	}
	if _, err := user.LookupGroup(backupGroup); err == nil {
		found = append(found, "group:"+backupGroup)
	} else {
		var unknown user.UnknownGroupError
		if !errors.As(err, &unknown) {
			return fmt.Errorf("inspect backup group %s: %w", backupGroup, err)
		}
	}

	return serviceLayoutConflict(false, "", found)
}

// serviceLayoutConflict is the pure decision at the migration
// boundary; filesystem discovery stays in guardFreshServiceLayout.
func serviceLayoutConflict(
	markerPresent bool, marker string, found []string,
) error {
	if markerPresent {
		if marker == serviceLayoutMarkerContent {
			return nil
		}
		return fmt.Errorf(
			"service-layout marker %s is invalid — refusing to "+
				"modify service identities", paths.ServiceLayoutMarker)
	}
	if len(found) == 0 {
		return nil
	}
	return fmt.Errorf(
		"existing node state uses an unmarked legacy service layout "+
			"(%s) — automatic migration is not supported yet; no "+
			"ownership, configuration, or service changes were made",
		strings.Join(found, ", "))
}

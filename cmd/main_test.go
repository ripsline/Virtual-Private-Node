// cmd/main_test.go

package main

import (
	"strings"
	"testing"
)

// Explicit dispatch: the command line alone decides the mode.
func TestParseArgs(t *testing.T) {
	cmd, _, err := parseArgs(nil)
	if err != nil || cmd != cmdConsole {
		t.Errorf("no args: got (%v,%v), want console", cmd, err)
	}

	cmd, opts, err := parseArgs([]string{"install"})
	if err != nil || cmd != cmdInstall {
		t.Fatalf("install: got (%v,%v)", cmd, err)
	}
	if opts.Network != "" || opts.Unattended || opts.UntilBake {
		t.Errorf("install: unexpected opts %+v", opts)
	}

	cmd, opts, err = parseArgs(
		[]string{"install", "--testnet4", "--unattended"})
	if err != nil || cmd != cmdInstall {
		t.Fatalf("install flags: got (%v,%v)", cmd, err)
	}
	if opts.Network != "testnet4" || !opts.Unattended {
		t.Errorf("install flags: got %+v", opts)
	}

	if _, _, err := parseArgs(
		[]string{"install", "--bogus"}); err == nil {
		t.Error("unknown install flag accepted")
	}
	if _, _, err := parseArgs([]string{"bogus"}); err == nil {
		t.Error("unknown command accepted")
	}

	for _, v := range []string{"version", "--version", "-v"} {
		if cmd, _, err := parseArgs([]string{v}); err != nil ||
			cmd != cmdVersion {
			t.Errorf("%s: got (%v,%v), want version", v, cmd, err)
		}
	}

	cmd, opts, err = parseArgs([]string{
		"install", "--unattended", "--allow-console-only"})
	if err != nil || cmd != cmdInstall || !opts.AllowConsoleOnly {
		t.Errorf("allow-console-only: got (%v,%+v,%v)",
			cmd, opts, err)
	}

	cmd, _, err = parseArgs([]string{"helperd"})
	if err != nil || cmd != cmdHelperd {
		t.Errorf("helperd: got (%v,%v)", cmd, err)
	}
	if _, _, err := parseArgs(
		[]string{"helperd", "--flag"}); err == nil {
		t.Error("helperd with arguments accepted")
	}

	cmd, _, err = parseArgs([]string{"stage-lnd-cert"})
	if err != nil || cmd != cmdStageLNDCert {
		t.Errorf("stage-lnd-cert: got (%v,%v)", cmd, err)
	}
	if _, _, err := parseArgs(
		[]string{"stage-lnd-cert", "--flag"}); err == nil {
		t.Error("stage-lnd-cert with arguments accepted")
	}

	for _, network := range []string{"mainnet", "testnet4"} {
		cmd, opts, err = parseArgs(
			[]string{"publish-lnd-backup", network})
		if err != nil || cmd != cmdPublishLNDBackup ||
			opts.Network != network {
			t.Errorf("publisher %s: got (%v,%+v,%v)",
				network, cmd, opts, err)
		}
	}
	for _, args := range [][]string{
		{"publish-lnd-backup"},
		{"publish-lnd-backup", "signet"},
		{"publish-lnd-backup", "mainnet", "/tmp/destination"},
	} {
		if _, _, err := parseArgs(args); err == nil {
			t.Errorf("invalid publisher arguments accepted: %v", args)
		}
	}
}

func TestUsageDescribesFreshInstallAndResume(t *testing.T) {
	text := usage()
	if strings.Contains(text, "install or reinstall") {
		t.Fatal("usage still advertises reinstall")
	}
	if !strings.Contains(text, "resume a recognized interruption") {
		t.Fatal("usage does not describe supported resume")
	}
}

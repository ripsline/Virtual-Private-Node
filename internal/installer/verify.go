// Package installer — verify.go
//
// GPG signature and checksum verification for all software.
// Downloads signing keys, verifies fingerprints, checks
// signatures on manifests, then verifies tarball checksums.
package installer

import (
    "fmt"
    "os"
    "os/exec"
    "strings"
)

// ── Trusted signing keys ─────────────────────────────────
//
// Keys are downloaded at install time and verified by fingerprint.
// Fingerprints are hardcoded — they don't change when keys are renewed.

// bitcoinCoreSigners lists trusted Bitcoin Core builders.
// We require 2 out of 5 valid signatures.
var bitcoinCoreSigners = []struct {
    name        string
    fingerprint string
    keyURL      string
}{
    {
        name:        "fanquake",
        fingerprint: "152812300785C96444D3334D17565732E08E5E41",
        keyURL:      "https://raw.githubusercontent.com/bitcoin-core/guix.sigs/main/builder-keys/fanquake.gpg",
    },
    {
        name:        "guggero",
        fingerprint: "F4FC70F07310028424EFC20A8E4256593F177720",
        keyURL:      "https://raw.githubusercontent.com/bitcoin-core/guix.sigs/main/builder-keys/guggero.gpg",
    },
    {
        name:        "hebasto",
        fingerprint: "E86AE73439625BBEE306AAE6B66D427F873CB1A3",
        keyURL:      "https://raw.githubusercontent.com/bitcoin-core/guix.sigs/main/builder-keys/hebasto.gpg",
    },
    {
        name:        "theStack",
        fingerprint: "D1DBF2C4B96F2DEBF4C16654410108112E7EA81F",
        keyURL:      "https://raw.githubusercontent.com/bitcoin-core/guix.sigs/main/builder-keys/theStack.gpg",
    },
    {
        name:        "willcl-ark",
        fingerprint: "6A8F9C266528E25AEB1D7731C2371D91CB716EA7",
        keyURL:      "https://raw.githubusercontent.com/bitcoin-core/guix.sigs/main/builder-keys/willcl-ark.gpg",
    },
}

// lndSigner is the key used to sign LND releases (Roasbeef).
var lndSigner = struct {
    name        string
    fingerprint string
    keyURL      string
}{
    name:        "roasbeef",
    fingerprint: "296212681AADF05656A2CDEE90525F7DEEE0AD86",
    keyURL:      "https://raw.githubusercontent.com/lightningnetwork/lnd/master/scripts/keys/roasbeef.asc",
}

// litSigner is the key used to sign LIT releases (ViktorT-11).
var litSigner = struct {
    name        string
    fingerprint string
    keyID       string
}{
    name:        "ViktorT-11",
    fingerprint: "C20A78516A0944900EBFCA29961CC8259AE675D4",
    keyID:       "C20A78516A0944900EBFCA29961CC8259AE675D4",
}

// ── GPG setup ────────────────────────────────────────────

// ensureGPG makes sure gpg is installed.
func ensureGPG() error {
    if _, err := exec.LookPath("gpg"); err == nil {
        return nil
    }
    cmd := exec.Command("apt-get", "install", "-y", "-qq", "gnupg")
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("install gpg: %s: %s", err, output)
    }
    return nil
}

// ── Key import ───────────────────────────────────────────

// importBitcoinCoreKeys downloads and imports Bitcoin Core builder keys.
// Verifies each key's fingerprint after import.
func importBitcoinCoreKeys() error {
    for _, signer := range bitcoinCoreSigners {
        keyFile := fmt.Sprintf("/tmp/btc-key-%s.gpg", signer.name)
        if err := download(signer.keyURL, keyFile); err != nil {
            // Non-fatal: we might still get enough valid sigs
            continue
        }

        cmd := exec.Command("gpg", "--batch", "--import", keyFile)
        cmd.CombinedOutput()
        os.Remove(keyFile)

        // Verify fingerprint
        if !gpgHasFingerprint(signer.fingerprint) {
            return fmt.Errorf("key fingerprint mismatch for %s", signer.name)
        }
    }
    return nil
}

// importLNDKey downloads and imports Roasbeef's signing key.
func importLNDKey() error {
    keyFile := "/tmp/lnd-key-roasbeef.asc"
    if err := download(lndSigner.keyURL, keyFile); err != nil {
        return fmt.Errorf("download LND signing key: %w", err)
    }
    defer os.Remove(keyFile)

    cmd := exec.Command("gpg", "--batch", "--import", keyFile)
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("import LND key: %s: %s", err, output)
    }

    if !gpgHasFingerprint(lndSigner.fingerprint) {
        return fmt.Errorf("LND key fingerprint mismatch")
    }
    return nil
}

// importLITKey imports ViktorT-11's signing key from keyserver.
func importLITKey() error {
    cmd := exec.Command("gpg", "--batch", "--keyserver",
        "hkps://keyserver.ubuntu.com", "--recv-keys", litSigner.keyID)
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("import LIT key: %s: %s", err, output)
    }

    if !gpgHasFingerprint(litSigner.fingerprint) {
        return fmt.Errorf("LIT key fingerprint mismatch")
    }
    return nil
}

// ── Signature verification ───────────────────────────────

// verifyBitcoinCoreSigs verifies that SHA256SUMS.asc has at least
// minValid valid signatures from our trusted builder set.
// Returns error if fewer than minValid signatures are valid.
func verifyBitcoinCoreSigs(minValid int) error {
    sumsFile := "/tmp/SHA256SUMS"
    sigFile := "/tmp/SHA256SUMS.asc"

    // Check both files exist
    if _, err := os.Stat(sumsFile); err != nil {
        return fmt.Errorf("SHA256SUMS not found")
    }
    if _, err := os.Stat(sigFile); err != nil {
        return fmt.Errorf("SHA256SUMS.asc not found")
    }

    // Run gpg --verify and count valid signatures
    cmd := exec.Command("gpg", "--batch", "--verify",
        "--status-fd", "1", sigFile, sumsFile)
    output, _ := cmd.CombinedOutput()

    outputStr := string(output)
    validCount := 0

    for _, signer := range bitcoinCoreSigners {
        if strings.Contains(outputStr, signer.fingerprint) &&
            strings.Contains(outputStr, "GOODSIG") {
            validCount++
        }
    }

    if validCount < minValid {
        return fmt.Errorf(
            "insufficient valid signatures: got %d, need %d",
            validCount, minValid)
    }

    return nil
}

// verifyLNDSig verifies the LND manifest signature.
// verifyLNDSig verifies the LND manifest GPG signature.
// GPG checks the file content against the signature — the
// filename on disk doesn't need to match the original.
func verifyLNDSig(version string) error {
    manifestFile := "/tmp/manifest.txt"
    sigFile := fmt.Sprintf("/tmp/manifest-roasbeef-v%s.sig", version)

    if _, err := os.Stat(manifestFile); err != nil {
        return fmt.Errorf("LND manifest not found at %s", manifestFile)
    }

    // Download the detached signature file
    sigURL := fmt.Sprintf(
        "https://github.com/lightningnetwork/lnd/releases/download/v%s/manifest-roasbeef-v%s.sig",
        version, version)
    if err := download(sigURL, sigFile); err != nil {
        return fmt.Errorf("download LND signature: %w", err)
    }
    defer os.Remove(sigFile)

    // GPG verifies the content of manifestFile against sigFile.
    // The actual filename doesn't matter — only the bytes.
    cmd := exec.Command("gpg", "--batch", "--verify",
        "--status-fd", "1", sigFile, manifestFile)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("LND signature verification failed: %s", output)
    }

    if !strings.Contains(string(output), "GOODSIG") {
        return fmt.Errorf("LND signature invalid")
    }

    return nil
}

// verifyLITSig verifies the LIT manifest signature.
// verifyLITSig verifies the LIT manifest GPG signature.
// The manifest is saved as lit-manifest.txt but GPG only
// checks file content, not filename, so no rename needed.
func verifyLITSig(version string) error {
    manifestFile := "/tmp/lit-manifest.txt"
    sigFile := fmt.Sprintf("/tmp/manifest-ViktorT-11-v%s.sig", version)

    if _, err := os.Stat(manifestFile); err != nil {
        return fmt.Errorf("LIT manifest not found at %s", manifestFile)
    }

    // Download the detached signature file
    sigURL := fmt.Sprintf(
        "https://github.com/lightninglabs/lightning-terminal/releases/download/v%s/manifest-ViktorT-11-v%s.sig",
        version, version)
    if err := download(sigURL, sigFile); err != nil {
        return fmt.Errorf("download LIT signature: %w", err)
    }
    defer os.Remove(sigFile)

    // GPG verifies content, not filename — no rename needed
    cmd := exec.Command("gpg", "--batch", "--verify",
        "--status-fd", "1", sigFile, manifestFile)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("LIT signature verification failed: %s", output)
    }

    if !strings.Contains(string(output), "GOODSIG") {
        return fmt.Errorf("LIT signature invalid")
    }

    return nil
}

// ── Helpers ──────────────────────────────────────────────

// gpgHasFingerprint checks if a key with the given fingerprint
// exists in the GPG keyring.
func gpgHasFingerprint(fingerprint string) bool {
    cmd := exec.Command("gpg", "--batch", "--list-keys",
        "--with-colons", fingerprint)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return false
    }
    return strings.Contains(string(output), fingerprint)
}

// downloadBitcoinSigFile downloads SHA256SUMS.asc for Bitcoin Core.
func downloadBitcoinSigFile(version string) error {
    url := fmt.Sprintf(
        "https://bitcoincore.org/bin/bitcoin-core-%s/SHA256SUMS.asc", version)
    return download(url, "/tmp/SHA256SUMS.asc")
}
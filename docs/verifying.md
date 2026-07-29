## Release Verification

Verify signatures before installation.

These steps apply to the releases published so far, which were made
under the project's previous name and ship a binary called rlvpn. They
carry forward unchanged to v0.7.0, which will be the first release
under the new name.

### Import the release signing key

```bash
gpg --keyserver hkps://keys.openpgp.org --recv-keys AFA0EBACDC9A4C4AA7B0154AC97CE10F170BA5FE
```

### Download the release files

```bash
VERSION="0.6.3"
wget -q "https://github.com/virtualprivatenode/vpn/releases/download/v${VERSION}/rlvpn-${VERSION}-amd64.tar.gz"
wget -q "https://github.com/virtualprivatenode/vpn/releases/download/v${VERSION}/SHA256SUMS"
wget -q "https://github.com/virtualprivatenode/vpn/releases/download/v${VERSION}/SHA256SUMS.asc"
```

### Verify the signature

```bash
gpg --verify SHA256SUMS.asc SHA256SUMS
```

### Verify the checksum

```bash
sha256sum --check --ignore-missing SHA256SUMS
```

The bootstrap script performs this verification automatically during
installation. This section is for users who want to verify manually
before running the one-liner.
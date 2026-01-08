# ripsline Virtual Private Node

Open-source Bitcoin node installer with BTCPay Server, Lightning Network (LND), and optional Haven Nostr relay support.

## Features

- **BTCPay Server** - Self-hosted Bitcoin payment processor
- **Lightning Network (LND)** - With Simple Taproot Channels enabled
- **Lightning Terminal (LIT)** - Manage your Lightning node with a web interface
- **Tor Support** - Privacy-enhanced connections
- **Haven Relay** - Optional personal Nostr relay
- **NIP-05 Identity** - Nostr identity verification
- **Automated SSL** - Let's Encrypt certificates
- **Security Hardened** - UFW firewall, secure defaults

## Quick Start

### Prerequisites

- Fresh Ubuntu 24 LTS VPS
- Minimum 4GB RAM
- 80GB+ storage for Pruned Bitcoin blockchain
- Domain name with DNS configured to point to your VPS IP

### Installation

```bash
# Login as root
sudo su -

# Clone the repository
git clone https://github.com/ripsline/virtual-private-node.git
cd virtual-private-node

# Run the installer
sudo bash install-ripsline.sh
```

The installer will:
1. Verify DNS configuration
2. Install system dependencies
3. Set up BTCPay Server with Lightning Network
4. Configure nginx with SSL
5. Set up firewall rules
6. Create helper scripts for management

### Post-Installation

After installation completes:

1. **Reboot your server** (recommended for kernel updates)
2. **Wait 2-3 minutes** for services to start
3. **Access your node** at `https://yourdomain.com`
4. **Create your admin account** in BTCPay Server
5. **Wait 1-5 days** for Bitcoin blockchain to sync

## Directory Structure

After installation, your server will have:

```
/root/ripsline/
├── btcpayserver-docker/       # BTCPay Server installation
├── config/                    # Configuration templates
│   ├── nginx-btcpay.conf
│   ├── nginx-haven.conf
│   ├── lnd-custom.yml
│   └── nostr.json.template
├── scripts/                   # Helper scripts
│   ├── install-haven.sh
│   ├── backup-and-scb.sh
│   ├── fix-lnd-host.sh
│   ├── update-ripsline.sh
│   └── import-notes.sh
├── .installed                 # Installation lock file
└── RIPSLINE_INFO.txt          # Your installation details
```

## Helper Scripts

### Backup & Channel Backup (SCB)

**CRITICAL**: Run this after opening any Lightning channel!

```bash
sudo /root/ripsline/scripts/backup-and-scb.sh
```

This will:
- Create a full BTCPay backup
- Display your Lightning channel backup (SCB)
- Show instructions for downloading files

**Save your channel backup** to a password manager (KeePass, 1Password, Bitwarden) - this is the only way to recover Lightning funds if your VPS fails!

### Fix LND Host (After BTCPay Updates)

After updating BTCPay Server, run this to fix Lightning connectivity:

```bash
sudo /root/ripsline/scripts/fix-lnd-host.sh
```

### Install Haven Nostr Relay

Install a personal Nostr relay on your node:

```bash
sudo /root/ripsline/scripts/install-haven.sh
```

Features:
- Personal Nostr relay for your notes and other stuff
- Private chat relay (Web of Trust)
- Media storage (Blossom)
- Import existing notes from other relays

### Update ripsline Scripts

Update helper scripts and configs (does not update BTCPay):

```bash
sudo /root/ripsline/scripts/update-ripsline.sh
```

## Updating BTCPay Server

To update BTCPay Server itself:

```bash
sudo su -
```

```bash
cd /root/ripsline/btcpayserver-docker
```

```bash
btcpay-update.sh
```

After updating BTCPay, always run the LND fix script:

```bash
/root/ripsline/scripts/fix-lnd-host.sh
```

## NIP-05 Nostr Identity

Your node includes NIP-05 identity verification support.

1. Edit the configuration file:
```bash
sudo nano /var/www/html/.well-known/nostr.json
```

2. Replace `yourname` with your desired username
3. Replace `YOUR_HEX_PUBKEY_HERE` with your Nostr public key (hex format)
4. Save and exit (Ctrl+X, Y, Enter)

Your identity will be: `yourname@yourdomain.com`

Public URL: `https://yourdomain.com/.well-known/nostr.json`

## Useful Commands

```bash
# Check BTCPay status
docker ps

# Check nginx status
systemctl status nginx

# Reload nginx
systemctl reload nginx

# Check firewall status
ufw status

# View SSL certificate info
certbot certificates
```

## Lightning Channel Management

### Opening Channels

1. Connect BTCPay to Zeus Wallet
2. Or use BTCPay Server's Lightning interface
3. **IMPORTANT**: After opening ANY channel, run backup script!

### Channel Backup

```bash
sudo /root/ripsline/scripts/backup-and-scb.sh
```

Save the channel backup to your password manager **immediately**!

## Troubleshooting

### BTCPay Not Accessible

1. Check if services are running:
```bash
docker ps
```

2. Check nginx:
```bash
systemctl status nginx
nginx -t
```

3. Check firewall:
```bash
ufw status
```

### Lightning Not Connecting

```bash
# Run the fix script
sudo /root/ripsline/scripts/fix-lnd-host.sh

# Check LND status
docker logs btcpayserver_lnd_bitcoin
```

## Security Recommendations

1. **Change SSH password** in your VPS provider's control panel
2. **Enable SSH key authentication** - see [docs](https://ripsline.com/docs/ssh-keys/)
3. **Disable password authentication** after setting up SSH keys
4. **Backup your Lightning channels** after any changes
5. **Keep your system updated**

## Support

- **Documentation**: https://ripsline.com/docs
- **Email**: support@ripsline.com
- **Issues**: https://github.com/ripsline/virtual-private-node/issues

## For Customer Orders (ripsline.com)

If you purchased through ripsline.com, you'll receive a personalized installer with your information pre-filled (domain, email, order ID).

The `templates/install-template.sh` file in this repo is used to generate those personalized installers.

## License

MIT License - See LICENSE file for details

## Contributing

Contributions welcome! Please open an issue or pull request.

---

**Note**: This is a self-hosted solution. You are responsible for maintaining and securing your server. Always backup your Lightning channels and Bitcoin wallet seed!

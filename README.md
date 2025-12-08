# ripsline Virtual-Private-Node Auto-Installer

Official GitHub for ripsline Virtual Private Node

One-command installation of a complete BTCPay Server stack with Bitcoin, Lightning Network (LND), Tor, Lightning Terminal, NIP-05 Nostr Identity, & Nostr Zaps.

## 🚀 Quick Start

### Prerequisites

Before running the installer, you need:

1. **A fresh VPS** running Ubuntu 24 LTS
   - Recommended: 4GB RAM, 2 CPU cores, 90GB storage

2. **A domain name** with DNS configured
   - Create an A record pointing to your VPS IP address
   - Example: `btcpay.yourdomain.com → 123.456.789.10`
   - ⚠️ **Wait 5-10 minutes** for DNS propagation before running the script

3. **Root access** to your VPS
   ```bash
   sudo su -
   ```

### Installation

**Option 1: Direct execution (recommended)**
```bash
curl -sSL https://raw.githubusercontent.com/ripsline/Virtual-Private-Node/main/install.sh | sudo bash
```

**Option 2: Download then execute**
```bash
wget https://raw.githubusercontent.com/ripsline/Virtual-Private-Node/main/install.sh
chmod +x install.sh
sudo ./install.sh
```

**Option 3: Manual copy-paste**
1. Copy the entire `install.sh` script
2. On your VPS: `nano install.sh`
3. Paste the script and save (Ctrl+X, Y, Enter)
4. Run: `chmod +x install.sh && sudo ./install.sh`

### During Installation

The script will prompt you for:

1. **Your domain** (e.g., `btcpay.yourdomain.com`)
2. **Your email** (for Let's Encrypt SSL certificates)

The script will automatically:
- Detect your VPS IP address
- Generate a secure Lightning Terminal password
- Configure firewall rules
- Install and configure BTCPay Server

Installation takes **15-30 minutes** depending on your VPS speed.

## 📦 What Gets Installed

This installer configures:

- ✅ **BTCPay Server** (latest version)
- ✅ **Bitcoin Core** (mainnet, pruned for storage optimization)
- ✅ **Lightning Network Daemon (LND)**
- ✅ **Tor** (for privacy and .onion access)
- ✅ **Lightning Terminal** (advanced LND management)
- ✅ **SSL/TLS** via Let's Encrypt (automatic HTTPS)
- ✅ **UFW Firewall** (properly configured)

### Firewall Rules

The installer opens these ports:
- `22/tcp` - SSH (keep this secure!)
- `80/tcp` - HTTP (redirects to HTTPS)
- `443/tcp` - HTTPS
- `9735/tcp` - Lightning Network

## 🎯 After Installation

### Access Your Server

Visit: `https://your-domain.com`

You'll be prompted to:
1. Create your admin account
2. Set up your Bitcoin wallet (Optional)
3. Configure Lightning Network

### Important Credentials

**Save these from the installation output:**

- **Lightning Terminal Password** - You'll need this to access Lightning Terminal
- **VPS IP Address** - For LND configuration
- **Domain** - Your BTCPay Server URL

## 🔧 Post-Installation

### LND Announceable Host Fix

After BTCPay Server updates, your LND announceable host may reset. To fix:

```bash
/root/BTCPayServer/fix-lnd-host.sh
```

This script:
- Sets `BTCPAY_ANNOUNCEABLE_HOST` to your VPS IP
- Restarts LND automatically

### Useful Commands

**Check BTCPay Server status:**
```bash
cd /root/BTCPayServer/btcpayserver-docker
./btcpay-down.sh  # Stop
./btcpay-up.sh    # Start
```

**View logs:**
```bash
cd /root/BTCPayServer/btcpayserver-docker
docker logs btcpayserver_btcpayserver_1 --tail 100
docker logs btcpayserver_lnd_bitcoin --tail 100
```

**Update BTCPay Server:**
```bash
cd /root/BTCPayServer/btcpayserver-docker
./btcpay-update.sh
# Then run the LND fix:
/root/BTCPayServer/fix-lnd-host.sh
```

**Check Bitcoin sync status:**
```bash
docker exec btcpayserver_bitcoind bitcoin-cli getblockchaininfo
```

**Check LND status:**
```bash
docker exec btcpayserver_lnd_bitcoin lncli getinfo
```

## 📁 File Locations

- **BTCPay installation:** `/root/BTCPayServer/btcpayserver-docker/`
- **LND fix script:** `/root/BTCPayServer/fix-lnd-host.sh`
- **Bitcoin data:** `/var/lib/docker/volumes/generated_bitcoin_datadir/`
- **LND data:** `/var/lib/docker/volumes/generated_lnd_bitcoin_datadir/`
- **Environment config:** `/root/BTCPayServer/.env`

## 🔒 Security Recommendations

1. **Change SSH port** (optional but recommended):
   ```bash
   nano /etc/ssh/sshd_config
   # Change Port 22 to something else, e.g., Port 2222
   # Then: ufw allow 2222/tcp && ufw delete allow 22/tcp
   systemctl restart sshd
   ```

2. **Disable password authentication** (use SSH keys):
   ```bash
   nano /etc/ssh/sshd_config
   # Set: PasswordAuthentication no
   systemctl restart sshd
   ```
3. **Backup your Lightning wallet regularly**

4. **Use strong passwords & 2FA** for your BTCPay admin account

## 🆘 Troubleshooting

### "Domain not accessible"

- Check DNS propagation: `nslookup your-domain.com`
- Wait 5-10 minutes and try again
- Verify A record points to correct IP

### "Certificate error"

- Let's Encrypt rate limits: 5 failed attempts per hour
- Check domain is correctly pointing to your server
- Wait an hour and try again

### "LND won't start"

- Run the fix script: `/root/BTCPayServer/fix-lnd-host.sh`
- Check logs: `docker logs btcpayserver_lnd_bitcoin`

### "Can't connect to Lightning peers"

- Verify port 9735 is open: `ufw status`
- Check your VPS provider doesn't block ports
- Run: `/root/BTCPayServer/fix-lnd-host.sh`

### "Bitcoin still syncing"

Initial Bitcoin blockchain sync takes **24-72 hours** depending on your VPS speed. Check progress:

```bash
docker exec btcpayserver_bitcoind bitcoin-cli getblockchaininfo | grep progress
```

## 🔄 Backup & Restore

### Backup Important Data

**Backup channel database** (before updates):
```bash
docker cp btcpayserver_lnd_bitcoin:/data/chain/bitcoin/mainnet/channel.backup ./channel.backup
```

### Restore

See official BTCPay Server documentation for restore procedures.

## 📚 Additional Resources

- **BTCPay Server Docs:** https://docs.btcpayserver.org/
- **LND Documentation:** https://docs.lightning.engineering/

## ⚠️ Disclaimer

This is an automated installer. Always:
- Test on a staging server first
- Understand what the script does before running
- Keep backups of important data
- Use at your own risk

## 📝 What's Different From Default BTCPay?

This installer includes:
- ✅ Taproot Channel support
- ✅ NIP-05 Nostr Username & Zaps Capability
- ✅ Storage optimization (`opt-save-storage-xs`)
- ✅ Tor support (`opt-add-tor`)
- ✅ Lightning Terminal (`opt-add-lightning-terminal`)
- ✅ Automatic LND announceable host fix
- ✅ Pre-configured firewall rules
- ✅ Mainnet-only (no testnet)

## 🤝 Contributing

Found a bug or want to improve the installer? Contributions welcome!

## 📄 License

MIT License - Use freely, modify as needed.

---

**Made with ⚡ for the Bitcoin community**

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
   - Example: `node.yourdomain.com → 123.456.789.10`
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

1. **Your domain** (e.g., `node.yourdomain.com`)
2. **Your email** (for Let's Encrypt SSL certificates)

The script will automatically:
- Detect your VPS IP address
- Generate a secure Lightning Terminal password
- Configure firewall rules
- Install enhanced BTCPay Server Config

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
- ✅ **Taproot Channel Support**
- ✅ **NIP-05 Nostr Username**

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

**View logs:**
```bash
cd /root/BTCPayServer/btcpayserver-docker
docker logs btcpayserver_bitcoind --tail 100
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
cd ~/BTCPayServer/btcpayserver-docker
./bitcoin-cli.sh getblockchaininfo | grep verificationprogress
```

**Check LND status:**
```bash
cd ~/BTCPayServer/btcpayserver-docker
./bitcoin-lncli.sh getinfo
```

## 📁 File Locations

- **BTCPay installation:** `/root/BTCPayServer/btcpayserver-docker/`
- **LND fix script:** `/root/BTCPayServer/fix-lnd-host.sh`
- **Backup script:** `/root/BTCPayServer/backup-btcpay-and-save-scb.sh`
- **Bitcoin data:** `/var/lib/docker/volumes/generated_bitcoin_datadir/`
- **LND data:** `/var/lib/docker/volumes/generated_lnd_bitcoin_datadir/`
- **Environment config:** `/root/BTCPayServer/.env`

## 🔒 Security Recommendations

1. **Disable password authentication** (use SSH keys):
   
   see: **https://ripsline.com/docs/ssh-keys/**
  
2. **Run backup script after each channel open**

   see: **https://ripsline.com/docs/channel-backups/**

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

Initial Bitcoin blockchain sync takes **1-5 days** depending on your VPS speed.

## 🔄 Backup & Restore

### Backup Important Data

**Backup channel database** (before updates):
```bash
/root/BTCPayServer/backup-btcpay-and-save-scb.sh
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

## 📝 What's Different From Default BTCPay?

This installer includes:
- ✅ Taproot Channel support
- ✅ NIP-05 Nostr Username & Zaps Capability
- ✅ Storage optimization (`opt-save-storage-xs`)
- ✅ Tor support (`opt-add-tor`)
- ✅ Lightning Terminal (`opt-add-lightning-terminal`)
- ✅ Automatic LND announceable host fix
- ✅ Pre-configured firewall rules

## 📄 License

MIT License - Use freely, modify as needed.

---

**Made with ⚡ for the Bitcoin community**

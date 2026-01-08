#!/bin/bash
set -e

# ripsline Virtual Private Node Auto-Installer
# Customer: {{CUSTOMER_NAME}}
# Order ID: {{CUSTOMER_ID}}
# Generated: {{GENERATION_DATE}}

echo "================================================"
echo "  ripsline Virtual Private Node Installation"
echo "  Customer: {{CUSTOMER_NAME}}"
echo "================================================"
echo ""

# Check if running as root, if not request sudo
if [ "$EUID" -ne 0 ]; then 
   echo "🔐 Root access required."
   echo "   Re-launching with sudo (you may be prompted for password)..."
   exec sudo "$0" "$@"
   exit
fi

echo "✅ Running as root, starting installation..."
echo ""

# Check for installation lock file
if [ -f "/root/ripsline/.installed" ]; then
    echo "⚠️  ⚠️  ⚠️  ripsline Already Installed! ⚠️  ⚠️  ⚠️"
    echo ""
    echo "Installation detected on: $(cat /root/ripsline/.installed)"
    echo ""
    echo "🚨 Re-running this script will DELETE ALL DATA including:"
    echo "   • Bitcoin wallet and funds"
    echo "   • Lightning channels (PERMANENT FUND LOSS!)"
    echo "   • Store configurations"
    echo "   • Invoice history"
    echo "   • Payment data"
    echo "   • Everything you've set up"
    echo ""
    echo "If you need to:"
    echo "   • Change domain: Contact support@ripsline.com"
    echo "   • Update scripts: Use 'sudo /root/ripsline/scripts/update-ripsline.sh'"
    echo "   • Update BTCPay: Use 'btcpay-update.sh' command"
    echo "   • Reset/reinstall: Contact support for backup assistance"
    echo ""
    read -p "Are you SURE you want to delete everything? Type 'DELETE EVERYTHING' to continue: " CONFIRM
    
    if [ "$CONFIRM" != "DELETE EVERYTHING" ]; then
        echo ""
        echo "✅ Installation cancelled. Your data is safe."
        exit 0
    fi
    
    echo ""
    echo "🚨 FINAL WARNING: This CANNOT be undone!"
    echo "You will LOSE all Bitcoin and Lightning funds if you proceed!"
    echo ""
    read -p "Type 'YES I AM SURE' to proceed: " FINAL_CONFIRM
    
    if [ "$FINAL_CONFIRM" != "YES I AM SURE" ]; then
        echo ""
        echo "✅ Installation cancelled. Your data is safe."
        exit 0
    fi
    
    echo ""
    echo "⚠️  Proceeding with deletion in 10 seconds... (Ctrl+C to cancel)"
    sleep 10
fi

# Check and configure swap if needed
echo "💾 Checking swap configuration..."

if swapon --show | grep -q .; then
    echo "✅ Swap already configured"
    swapon --show
else
    echo "   No swap found, creating 4GB swap..."
    fallocate -l 4G /swap.img
    chmod 600 /swap.img
    mkswap /swap.img
    swapon /swap.img
    echo "/swap.img none swap sw 0 0" >> /etc/fstab
    echo "✅ 4GB swap created and enabled"
fi
echo ""

# Auto-detect IP address (force IPv4)
echo "🔍 Detecting your VPS IP address..."
DETECTED_IP=$(curl -4 -s ifconfig.me || curl -4 -s icanhazip.com || curl -4 -s ipinfo.io/ip)

if [ -z "$DETECTED_IP" ]; then
    echo "❌ Could not auto-detect IP address."
    read -p "Please enter your VPS IP manually: " DETECTED_IP
fi

echo "✅ Detected IP: $DETECTED_IP"
echo ""

# Pre-configured values (from plugin)
DEFAULT_DOMAIN="{{DOMAIN}}"
LETSENCRYPT_EMAIL="{{EMAIL}}"
CUSTOMER_ID="{{CUSTOMER_ID}}"

# Check if domain is provided, if not prompt for it
if [ -z "$DEFAULT_DOMAIN" ]; then
    echo "⚠️  No pre-configured domain found"
    echo ""
    read -p "Enter your domain (e.g., example.com or node.example.com): " DEFAULT_DOMAIN
    echo ""
fi

# Check if email is provided, if not prompt for it
if [ -z "$LETSENCRYPT_EMAIL" ]; then
    echo "⚠️  No email configured"
    echo ""
    read -p "Enter your email address (for SSL certificates): " LETSENCRYPT_EMAIL
    echo ""
fi

# Check if customer ID is provided, if not generate one
if [ -z "$CUSTOMER_ID" ]; then
    CUSTOMER_ID="MANUAL-$(date +%s)"
fi

# Prompt for domain confirmation (with option to override)
echo "🌐 Domain Configuration"
echo "   Suggested domain: $DEFAULT_DOMAIN"
echo "   (This will be your ripsline Virtual Private Node URL)"
echo ""
read -p "Use this domain? (y/n) [y]: " USE_DEFAULT
USE_DEFAULT=${USE_DEFAULT:-y}

if [ "$USE_DEFAULT" = "y" ] || [ "$USE_DEFAULT" = "Y" ]; then
    BTCPAY_HOST="$DEFAULT_DOMAIN"
    echo "✅ Using: $BTCPAY_HOST"
else
    read -p "Enter your custom domain: " CUSTOM_DOMAIN
    BTCPAY_HOST="$CUSTOM_DOMAIN"
    echo "✅ Using custom domain: $BTCPAY_HOST"
fi

echo ""

# Lightning Terminal Password Setup (Auto-generated)
echo "🔑 Lightning Terminal Password Setup"
echo "   Generating secure password for Lightning Terminal..."
echo ""

# Generate strong random password (25 characters, safe characters only)
LIT_PASSWD=$(LC_ALL=C tr -dc 'A-Za-z0-9@#%+=._-' < /dev/urandom | head -c 25)

echo "✅ Lightning Terminal password generated and configured"
echo "   (You can view or change this later in your BTCPay settings)"
echo ""

# DNS Verification Check
echo "🔍 Checking DNS configuration..."
echo "   Verifying $BTCPAY_HOST points to $DETECTED_IP..."
echo ""

RESOLVED_IP=""

# Try nslookup
if command -v nslookup &> /dev/null; then
    RESOLVED_IP=$(nslookup $BTCPAY_HOST 2>/dev/null | grep -A1 "Name:" | grep "Address:" | tail -1 | awk '{print $2}' 2>/dev/null | grep -v "#") || true
fi

# Fallback to dig if nslookup failed or not installed
if [ -z "$RESOLVED_IP" ] && command -v dig &> /dev/null; then
    RESOLVED_IP=$(dig +short $BTCPAY_HOST @8.8.8.8 2>/dev/null | tail -1 | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$') || true
fi

# Fallback to host command
if [ -z "$RESOLVED_IP" ] && command -v host &> /dev/null; then
    RESOLVED_IP=$(host $BTCPAY_HOST 2>/dev/null | grep "has address" | awk '{print $4}' | head -1) || true
fi

# Check if DNS resolved at all
if [ -z "$RESOLVED_IP" ]; then
    echo "❌ DNS NOT CONFIGURED OR NOT PROPAGATED YET"
    echo "================================================"
    echo ""
    echo "$BTCPAY_HOST does not resolve to any IP address."
    echo ""
    echo "Please configure DNS and try again."
    echo "Check propagation at: https://dnschecker.org"
    echo ""
    echo "================================================"
    exit 1
fi

# Check if DNS points to THIS server
if [ "$RESOLVED_IP" != "$DETECTED_IP" ]; then
    echo "❌ DNS MISMATCH"
    echo "================================================"
    echo ""
    echo "$BTCPAY_HOST points to: $RESOLVED_IP"
    echo "This server's IP is: $DETECTED_IP"
    echo ""
    echo "Please fix DNS and try again."
    echo "Check propagation at: https://dnschecker.org"
    echo ""
    echo "================================================"
    exit 1
fi

echo "✅ DNS configured correctly!"
echo "   $BTCPAY_HOST → $DETECTED_IP"
echo ""

echo "🔥 Starting system update (this will take 2-5 minutes)..."
apt update && DEBIAN_FRONTEND=noninteractive apt upgrade -y
echo "✅ System packages updated successfully."
echo ""

echo "🌐 Installing nginx and certbot..."
apt install -y nginx certbot python3-certbot-nginx
echo "✅ Nginx and certbot installed"
echo ""

# Download ripsline scripts and configs
echo "📥 Downloading ripsline scripts and configurations..."
cd /tmp
rm -rf virtual-private-node
git clone https://github.com/ripsline/virtual-private-node.git
echo "✅ Downloaded"
echo ""

# Create ripsline directory structure
echo "📁 Creating ripsline directory structure..."
mkdir -p /root/ripsline
cd /root/ripsline

# Copy config and scripts
echo "📋 Installing ripsline scripts and configurations..."
cp -r /tmp/virtual-private-node/config /root/ripsline/
cp -r /tmp/virtual-private-node/scripts /root/ripsline/
chmod +x /root/ripsline/scripts/*.sh
rm -rf /tmp/virtual-private-node
echo "✅ Scripts and configurations installed"
echo ""

echo "🔥 Starting BTCPay installation..."
echo ""

# Install and configure UFW Firewall
echo "📡 Installing and configuring firewall..."
if ! command -v ufw &> /dev/null; then
    echo "   UFW not found, installing..."
    apt install -y ufw
fi
ufw --force reset
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 9735/tcp
ufw --force enable
echo "✅ Firewall configured"
echo ""

# Clone BTCPay repository
echo "📦 Cloning BTCPay repository..."
cd /root/ripsline
if [ -d "btcpayserver-docker" ]; then
    echo "   Directory exists, removing old installation..."
    rm -rf btcpayserver-docker
fi
git clone https://github.com/btcpayserver/btcpayserver-docker
cd btcpayserver-docker

# Set environment variables
echo "⚙️  Configuring ripsline Virtual Private Node..."
export BTCPAY_HOST="$BTCPAY_HOST"
export NBITCOIN_NETWORK="mainnet"
export BTCPAYGEN_CRYPTO1="btc"
export BTCPAYGEN_LIGHTNING="lnd"
export BTCPAYGEN_EXCLUDE_FRAGMENTS="nginx-https"
export BTCPAYGEN_ADDITIONAL_FRAGMENTS="opt-save-storage-xs;opt-add-tor;opt-add-lightning-terminal;opt-lnd-config.custom"
export REVERSEPROXY_HTTP_PORT="10080"
export BTCPAY_ENABLE_SSH=false
export LETSENCRYPT_EMAIL="$LETSENCRYPT_EMAIL"
export ACME_CA_URI="production"
export LIT_PASSWD="$LIT_PASSWD"

# Create custom LND configuration for Simple Taproot Channels
echo "⚙️  Configuring LND with Simple Taproot Channels..."
cp /root/ripsline/config/lnd-custom.yml docker-compose-generator/docker-fragments/opt-lnd-config.custom.yml
echo "✅ LND Taproot configuration created"
echo ""

# Create temporary nginx config (HTTP only for initial setup)
echo "📝 Creating temporary nginx configuration..."
mkdir -p /var/www/html
cat /root/ripsline/config/nginx-btcpay.conf | sed "s/DOMAIN_PLACEHOLDER/$BTCPAY_HOST/g" | sed '/#SSL_START/,/#SSL_END/d' | sed '/#NO_SSL_START/d' | sed '/#NO_SSL_END/d' > /etc/nginx/sites-available/ripsline-btcpay

# Enable site
ln -sf /etc/nginx/sites-available/ripsline-btcpay /etc/nginx/sites-enabled/
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl restart nginx
echo "✅ Temporary nginx configuration active"
echo ""

# Run setup
echo "🚀 Running BTCPay setup (this will take 15-30 minutes)..."
echo "   Feel free to grab a coffee ☕"
echo ""
. ./btcpay-setup.sh -i

echo ""
echo "⏳ Waiting for BTCPayServer to start..."
sleep 30

# Verify BTCPay is responding on internal port
echo "🔍 Verifying BTCPayServer is running..."
MAX_ATTEMPTS=10
ATTEMPT=0
while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
    if curl -s http://127.0.0.1:10080 > /dev/null 2>&1; then
        echo "✅ BTCPayServer is responding on internal port 10080"
        break
    fi
    ATTEMPT=$((ATTEMPT + 1))
    if [ $ATTEMPT -lt $MAX_ATTEMPTS ]; then
        echo "   Waiting for BTCPayServer to start... (attempt $ATTEMPT/$MAX_ATTEMPTS)"
        sleep 10
    else
        echo "⚠️  BTCPayServer not responding yet, but continuing with SSL setup..."
    fi
done
echo ""

# Get SSL certificate
echo "🔒 Obtaining SSL certificate..."
echo "   This will temporarily stop nginx..."
systemctl stop nginx
certbot certonly --standalone -d $BTCPAY_HOST --email $LETSENCRYPT_EMAIL --agree-tos --non-interactive

# Create SSL configuration files if they don't exist
if [ ! -f /etc/letsencrypt/options-ssl-nginx.conf ]; then
    echo "   Creating SSL configuration files..."
    curl -s https://raw.githubusercontent.com/certbot/certbot/master/certbot-nginx/certbot_nginx/_internal/tls_configs/options-ssl-nginx.conf > /etc/letsencrypt/options-ssl-nginx.conf
fi

if [ ! -f /etc/letsencrypt/ssl-dhparams.pem ]; then
    curl -s https://raw.githubusercontent.com/certbot/certbot/master/certbot/certbot/ssl-dhparams.pem > /etc/letsencrypt/ssl-dhparams.pem
fi

systemctl start nginx
echo "✅ SSL certificate obtained"
echo ""

# Create NIP-05 Nostr identity file
echo "🆔 Setting up NIP-05 Nostr identity support..."
mkdir -p /var/www/html/.well-known
cp /root/ripsline/config/nostr.json.template /var/www/html/.well-known/nostr.json

chown -R www-data:www-data /var/www/html/.well-known
chmod 755 /var/www/html/.well-known
chmod 644 /var/www/html/.well-known/nostr.json
echo "✅ NIP-05 template created at /var/www/html/.well-known/nostr.json"
echo "   Edit this file to add your Nostr public key (hex format)"
echo ""

# Update nginx config with HTTPS
echo "📝 Updating nginx configuration with SSL..."
cat /root/ripsline/config/nginx-btcpay.conf | sed "s/DOMAIN_PLACEHOLDER/$BTCPAY_HOST/g" | sed 's/#SSL_START//g' | sed 's/#SSL_END//g' > /etc/nginx/sites-available/ripsline-btcpay

nginx -t
systemctl reload nginx
echo "✅ HTTPS configuration active"
echo ""

# Update fix-lnd-host.sh with IP
echo "🔧 Configuring LND host fix script..."
sed -i "s/IP_PLACEHOLDER/$DETECTED_IP/g" /root/ripsline/scripts/fix-lnd-host.sh

# Run the fix
echo "🔌 Setting LND announceable host..."
/root/ripsline/scripts/fix-lnd-host.sh
echo ""

# Update backup script with IP
echo "📋 Configuring backup script..."
sed -i "s/IP_PLACEHOLDER/$DETECTED_IP/g" /root/ripsline/scripts/backup-and-scb.sh
echo ""

# Save customer info file
cat > /root/ripsline/RIPSLINE_INFO.txt << EOFINFO
================================================
ripsline Virtual Private Node Installation
================================================
Customer: {{CUSTOMER_NAME}}
Order ID: {{CUSTOMER_ID}}
Installation Date: $(date)

Configuration:
- Domain: $BTCPAY_HOST
- VPS IP: $DETECTED_IP
- Email: $LETSENCRYPT_EMAIL

Directory Structure:
- Main directory: /root/ripsline
- BTCPay location: /root/ripsline/btcpayserver-docker
- Config files: /root/ripsline/config
- Scripts: /root/ripsline/scripts

Important Scripts:
- LND fix: /root/ripsline/scripts/fix-lnd-host.sh
- Backup & SCB: /root/ripsline/scripts/backup-and-scb.sh
- Install Haven: /root/ripsline/scripts/install-haven.sh
- Update ripsline: /root/ripsline/scripts/update-ripsline.sh

Configuration Files:
- Nginx config: /etc/nginx/sites-available/ripsline-btcpay
- NIP-05 Nostr: /var/www/html/.well-known/nostr.json

SSL Certificate:
- Certificate: /etc/letsencrypt/live/$BTCPAY_HOST/fullchain.pem
- Private Key: /etc/letsencrypt/live/$BTCPAY_HOST/privkey.pem
- Auto-renewal: Configured (certbot timer)

NIP-05 Nostr Identity:
- File location: /var/www/html/.well-known/nostr.json
- Public URL: https://$BTCPAY_HOST/.well-known/nostr.json
- To configure: nano /var/www/html/.well-known/nostr.json
- Replace "yourname" with your desired username
- Replace "YOUR_HEX_PUBKEY_HERE" with your Nostr public key (hex format)
- Your identity will be: yourname@$BTCPAY_HOST
- Docs: https://ripsline.com/docs/nip05

Support: support@ripsline.com
================================================
EOFINFO

# Create installation lock file
echo "🔒 Creating installation lock file..."
touch /root/ripsline/.installed
echo "Installed: $(date)" > /root/ripsline/.installed
echo "Order: {{CUSTOMER_ID}}" >> /root/ripsline/.installed
echo "Domain: $BTCPAY_HOST" >> /root/ripsline/.installed

echo ""
echo "================================================"
echo "  ✅ Installation Complete!"
echo "================================================"
echo ""
echo "🎉 Your ripsline Virtual Private Node is ready!"
echo ""
echo "📍 Access your server:"
echo "   https://$BTCPAY_HOST"
echo ""
echo "🆔 NIP-05 Nostr Identity:"
echo "   Template: /var/www/html/.well-known/nostr.json"
echo "   URL: https://$BTCPAY_HOST/.well-known/nostr.json"
echo "   Configure: nano /var/www/html/.well-known/nostr.json"
echo "   Your identity: yourname@$BTCPAY_HOST"
echo ""
echo "📝 Your details saved to:"
echo "   /root/ripsline/RIPSLINE_INFO.txt"
echo ""
echo "🔄 After BTCPay updates, run:"
echo "   /root/ripsline/scripts/fix-lnd-host.sh"
echo ""
echo "📋 Lightning Channel Backups:"
echo "   Script: /root/ripsline/scripts/backup-and-scb.sh"
echo "   ⚠️  Run this after opening ANY new Lightning channel!"
echo ""
echo "📧 Need help? support@ripsline.com"
echo "   Reference: {{CUSTOMER_ID}}"
echo ""
echo "================================================"
echo ""
echo "🔄 IMPORTANT: Reboot Recommended"
echo "   A kernel update was probably installed during setup."
echo "   Please reboot your Virtual Private Node now:"
echo ""
echo "   Type: reboot"
echo ""
echo "   Your BTCPayServer will automatically restart after reboot."
echo "   Wait 2-3 minutes, then access: https://$BTCPAY_HOST"
echo ""
echo "================================================"
echo ""
echo "⏳ Important Notes:"
echo "   • Bitcoin blockchain sync: 1-5 days"
echo "   • You can start creating BTCPay admin right away!"
echo "   • Create your admin account at the URL above"
echo ""
echo "================================================"
echo ""
echo "⚠️ Security Reminder:"
echo "   • You should have already changed your VPS SSH password in FlokiNET Client Portal"
echo "   • If you haven't already done so, please change your FlokiNET client portal password (VPS provider)!"
echo "   • Please follow the Docs to enable SSH key access only: https://ripsline.com/docs/ssh-keys/"
echo ""
echo "================================================"
echo ""

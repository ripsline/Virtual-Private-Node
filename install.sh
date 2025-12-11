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
if [ -f "/root/BTCPayServer/.installed" ]; then
    echo "⚠️  ⚠️  ⚠️  BTCPayServer Already Installed! ⚠️  ⚠️  ⚠️"
    echo ""
    echo "Installation detected on: $(cat /root/BTCPayServer/.installed)"
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

DEFAULT_DOMAIN=""
LETSENCRYPT_EMAIL=""
CUSTOMER_ID=""

# Check if domain is provided, if not prompt for it
if [ -z "$DEFAULT_DOMAIN" ]; then
    echo "⚠️  No pre-configured domain found"
    echo ""
    read -p "Enter your domain (e.g., example.com or rlvpn.example.com): " DEFAULT_DOMAIN
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
# Excludes quotes, backticks, and problematic shell characters
LIT_PASSWD=$(LC_ALL=C tr -dc 'A-Za-z0-9@#%+=._-' < /dev/urandom | head -c 25)

echo "✅ Lightning Terminal password generated and configured"
echo "   (You can view or change this later in your BTCPay settings)"
echo ""

# DNS Verification Check
echo "🔍 Checking DNS configuration..."
echo "   Verifying $BTCPAY_HOST points to $DETECTED_IP..."
echo ""

# Try multiple DNS lookup methods
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
    echo "Possible causes:"
    echo ""
    echo "1. DNS not configured yet"
    echo "   → You need to set up the A record"
    echo ""
    echo "2. DNS configured but not propagated yet"
    echo "   → Wait 10-30 more minutes and try again"
    echo "   → Check propagation: https://dnschecker.org"
    echo ""
    echo "If you haven't configured DNS yet:"
    echo ""
    echo "1. Log into your domain DNS panel"
    echo "   (domain registrar)"
    echo ""
    echo "2. Create an A record:"
    echo "   Hostname: @ (for root domain) or subdomain name"
    echo "   Type: A"
    echo "   Points to: $DETECTED_IP"
    echo "   TTL: 3600"
    echo ""
    echo "3. Save changes and wait 10-30 minutes for propagation"
    echo ""
    echo "4. Check propagation at: https://dnschecker.org"
    echo "   Search for: $BTCPAY_HOST"
    echo ""
    echo "5. Run this script again once DNS shows your IP"
    echo ""
    echo "================================================"
    exit 1
fi

# Check if DNS points to THIS server
if [ "$RESOLVED_IP" != "$DETECTED_IP" ]; then
    echo "❌ DNS MISMATCH"
    echo "================================================"
    echo ""
    echo "$BTCPAY_HOST currently points to: $RESOLVED_IP"
    echo "But this server's IP is: $DETECTED_IP"
    echo ""
    echo "Possible causes:"
    echo ""
    echo "1. DNS not propagated yet"
    echo "   → Wait 10-30 more minutes and try again"
    echo ""
    echo "2. Wrong IP configured in DNS"
    echo "   → Check your DNS settings"
    echo "   → The A record should point to: $DETECTED_IP"
    echo ""
    echo "3. Running script on wrong server"
    echo "   → Make sure you're on the correct VPS"
    echo ""
    echo "To fix:"
    echo "• If you just configured DNS: Wait and try again"
    echo "• If DNS configured >1 hour ago: Verify DNS settings"
    echo ""
    echo "Check DNS propagation at: https://dnschecker.org"
    echo "Search for: $BTCPAY_HOST"
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

echo "🔥 Starting installation..."
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

# Create BTCPay directory
echo "📁 Creating BTCPay directory..."
cd /root
mkdir -p BTCPayServer
cd BTCPayServer

# Clone repository
echo "📦 Cloning BTCPay repository..."
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
cat > docker-compose-generator/docker-fragments/opt-lnd-config.custom.yml << 'LND_CUSTOM_CONFIG'
version: '3'
services:
  lnd_bitcoin:
    environment:
      LND_EXTRA_ARGS: |
        protocol.simple-taproot-chans=true
LND_CUSTOM_CONFIG
echo "✅ LND Taproot configuration created"
echo ""

# Create temporary nginx config (HTTP only for initial setup)
echo "📝 Creating temporary nginx configuration..."
mkdir -p /var/www/html
cat > /etc/nginx/sites-available/btcpayserver << 'NGINX_HTTP_CONFIG'
server {
    listen 80;
    server_name DOMAIN_PLACEHOLDER;
    
    location /.well-known/acme-challenge/ {
        root /var/www/html;
        allow all;
    }
    
    location / {
        proxy_pass http://127.0.0.1:10080;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        
        proxy_buffer_size 128k;
        proxy_buffers 4 256k;
        proxy_busy_buffers_size 256k;
    }
}
NGINX_HTTP_CONFIG

# Replace placeholder with actual domain
sed -i "s/DOMAIN_PLACEHOLDER/$BTCPAY_HOST/g" /etc/nginx/sites-available/btcpayserver

# Enable site
ln -sf /etc/nginx/sites-available/btcpayserver /etc/nginx/sites-enabled/
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
cat > /var/www/html/.well-known/nostr.json << 'NOSTR_JSON'
{
  "names": {
    "yourname": "YOUR_HEX_PUBKEY_HERE"
  }
}
NOSTR_JSON

chown -R www-data:www-data /var/www/html/.well-known
chmod 755 /var/www/html/.well-known
chmod 644 /var/www/html/.well-known/nostr.json
echo "✅ NIP-05 template created at /var/www/html/.well-known/nostr.json"
echo "   Edit this file to add your Nostr public key (hex format)"
echo ""

# Create final nginx config with HTTPS and Lightning Terminal WebSocket support
echo "📝 Updating nginx configuration with SSL and Lightning Terminal support..."
cat > /etc/nginx/sites-available/btcpayserver << 'NGINX_HTTPS_CONFIG'
server {
    listen 80;
    server_name DOMAIN_PLACEHOLDER;
    
    location /.well-known/acme-challenge/ {
        root /var/www/html;
        allow all;
    }
    
    location / {
        return 301 https://$server_name$request_uri;
    }
}

server {
    listen 443 ssl http2;
    server_name DOMAIN_PLACEHOLDER;
    
    ssl_certificate /etc/letsencrypt/live/DOMAIN_PLACEHOLDER/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/DOMAIN_PLACEHOLDER/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;
    
    location = /.well-known/nostr.json {
        root /var/www/html;
        add_header Access-Control-Allow-Origin * always;
        add_header Content-Type application/json;
    }
    
    # Lightning Terminal specific configuration with WebSocket support
    location /lit {
        proxy_pass http://127.0.0.1:10080;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        
        # WebSocket support - CRITICAL for Lightning Terminal
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        
        # Timeout settings for long-lived connections
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
        proxy_connect_timeout 3600s;
        
        # Buffer settings
        proxy_buffering off;
        proxy_buffer_size 128k;
        proxy_buffers 4 256k;
        proxy_busy_buffers_size 256k;
    }
    
    # Default location for all other traffic
    location / {
        proxy_pass http://127.0.0.1:10080;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        
        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        
        # Buffer settings
        proxy_buffer_size 128k;
        proxy_buffers 4 256k;
        proxy_busy_buffers_size 256k;
    }
}
NGINX_HTTPS_CONFIG

# Replace all placeholders with actual domain
sed -i "s/DOMAIN_PLACEHOLDER/$BTCPAY_HOST/g" /etc/nginx/sites-available/btcpayserver

# Test and reload nginx
nginx -t
systemctl reload nginx
echo "✅ HTTPS configuration with Lightning Terminal WebSocket support active"
echo ""

# Create fix-lnd-host.sh script
echo "🔧 Creating LND host fix script..."
cat > /root/BTCPayServer/fix-lnd-host.sh << 'EOFSCRIPT'
#!/bin/bash
# Auto-fix BTCPAY_ANNOUNCEABLE_HOST after BTCPayServer updates
# Customer: {{CUSTOMER_NAME}} ({{CUSTOMER_ID}})

rlVPN_IP="IP_PLACEHOLDER"

# Edit the .env file
sed -i "s/^BTCPAY_ANNOUNCEABLE_HOST=.*/BTCPAY_ANNOUNCEABLE_HOST=$rlVPN_IP/" /root/BTCPayServer/.env

# Recreate LND container with updated config (reads new .env values)
cd /root/BTCPayServer/btcpayserver-docker/Generated
docker-compose -f docker-compose.generated.yml up -d --force-recreate lnd_bitcoin

# Restart LIT
docker restart generated-lnd_lit-1

echo "✅ BTCPAY_ANNOUNCEABLE_HOST set to $rlVPN_IP, LND recreated with new config, LIT restarted"
EOFSCRIPT

# Replace IP placeholder
sed -i "s/IP_PLACEHOLDER/$DETECTED_IP/g" /root/BTCPayServer/fix-lnd-host.sh

# Make executable
chmod +x /root/BTCPayServer/fix-lnd-host.sh

# Run the fix
echo "🔌 Setting LND announceable host..."
/root/BTCPayServer/fix-lnd-host.sh
echo ""

# Create backup-btcpay-and-save-scb.sh script
echo "📋 Creating backup script..."
cat > /root/BTCPayServer/backup-btcpay-and-save-scb.sh << 'EOFBACKUPSCRIPT'
#!/bin/bash
# BTCPay Server Backup Script with Channel Backup (SCB) Display
# Creates full backup and displays channel.backup for easy copy/paste
# Location: /root/BTCPayServer/backup-btcpay-and-save-scb.sh

set -e

echo "================================================"
echo "  BTCPay Server Backup & Channel Backup (SCB)"
echo "================================================"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
   echo "❌ This script must be run as root"
   echo "   Please run: sudo $0"
   exit 1
fi

rlVPN_IP="IP_PLACEHOLDER"

# Date for backup filename reference
BACKUP_DATE=$(date +%Y-%m-%d)

echo "🔄 Step 1: Creating full BTCPay Server backup..."
echo "   (This includes database, settings, and all data)"
echo ""

# Change to btcpay-docker directory
cd /root/BTCPayServer/btcpayserver-docker

# Run the official BTCPay backup script
if ./btcpay-backup.sh; then
    echo ""
    echo "✅ Full backup created successfully"
else
    echo ""
    echo "❌ Backup failed. Please check error messages above."
    exit 1
fi

echo ""
echo "================================================"
echo "📦 Full Backup Download (Optional - For Merchants)"
echo "================================================"
echo ""
echo "The full backup includes:"
echo "  • Complete database (invoices, payments, store data)"
echo "  • Channel backup (included below)"
echo "  • Configuration files"
echo "  • Everything needed to restore your BTCPay Server"
echo ""
echo "To download the full backup to YOUR LOCAL MACHINE:"
echo ""
echo "1. Open a NEW terminal on your local Dedicated Device"
echo "2. Run this command:"
echo ""
echo "   scp -i ~/.ssh/id_ed25519_VirtualPrivateNode root@$rlVPN_IP:/var/lib/docker/volumes/backup_datadir/_data/backup.tar.gz ./btcpay-backup-$BACKUP_DATE.tar.gz"
echo ""
echo "3. The file will download to your current directory"
echo ""
echo "💡 Merchants should download the full backup weekly/monthly"
echo "   for business records and invoice history."
echo ""

echo ""
echo "🔍 Step 2: Extracting Lightning Channel Backup (SCB)..."
echo ""

# Path to live channel.backup file
CHANNEL_BACKUP_PATH="/var/lib/docker/volumes/generated_lnd_bitcoin_datadir/_data/data/chain/bitcoin/mainnet/channel.backup"

# Check if channel.backup exists
if [ ! -f "$CHANNEL_BACKUP_PATH" ]; then
    echo "⚠️  Warning: channel.backup file not found"
    echo "   This might mean:"
    echo "   • No Lightning channels have been opened yet"
    echo "   • LND is not running"
    echo ""
    echo "   If you haven't opened any Lightning channels yet, this is normal."
    echo ""
    read -p "Press Enter to continue..."
    CHANNEL_BACKUP_FOUND=false
else
    CHANNEL_BACKUP_FOUND=true
fi

echo ""
echo "================================================"
echo "📋 CRITICAL: Lightning Channel Backup (SCB)"
echo "    ⚠️  THIS IS YOUR MOST IMPORTANT BACKUP ⚠️"
echo "================================================"
echo ""

if [ "$CHANNEL_BACKUP_FOUND" = true ]; then
    echo "✅ Channel backup found and ready to save"
    echo ""
    echo "This backup allows you to recover Lightning channel funds"
    echo "in case of VPS failure or data loss."
    echo ""
    echo "INSTRUCTIONS:"
    echo "1. Copy EVERYTHING below (including the BEGIN/END lines)"
    echo "2. Paste into a secure location:"
    echo "   • Password manager (KeePass, 1Password, Bitwarden)"
    echo ""
    echo "3. Suggested filename: channel-backup-$BACKUP_DATE.txt"
    echo ""
    echo "⚠️  IMPORTANT: Rerun & Save this EVERY TIME you open a new channel!"
    echo ""
    echo "⚠️  SECURITY: This contains sensitive channel data."
    echo "   Only store in encrypted password managers or encrypted files."
    echo "   Do NOT paste in plain text files, emails, or unencrypted notes."
    echo ""
    echo "================================================"
    echo ""
    
    # Display base64 encoded channel.backup
    echo "--- BEGIN CHANNEL BACKUP $BACKUP_DATE ---"
    base64 "$CHANNEL_BACKUP_PATH"
    echo "--- END CHANNEL BACKUP $BACKUP_DATE ---"
    
    echo ""
    echo "================================================"
    echo ""
    echo "🔑 PASTE IT INTO YOUR PASSWORD MANAGER RIGHT NOW!"
    echo "   • KeePass"
    echo "   • 1Password" 
    echo "   • Bitwarden"
    echo ""
    echo "💡 Label it: Lightning Channel Backup - $BACKUP_DATE"
    echo "⚠️  Without this, you CANNOT recover your Lightning funds!"
    echo ""
    echo "To recover later (if needed):"
    echo "  1. Save the text to a file: channel-backup.txt"
    echo "  2. Remove the BEGIN/END lines (keep only base64 data)"
    echo "  3. Decode: base64 -d channel-backup.txt > channel.backup"
    echo "  4. Use during LND recovery process"
    echo ""
else
    echo "⚠️  No channel backup available yet"
    echo ""
    echo "Once you open your first Lightning channel, run this script"
    echo "again to get your channel backup."
    echo ""
fi

echo "================================================"
echo ""
echo "Summary:"
echo "  📦 Full BTCPay backup available for download (optional)"
echo "  🔑 Channel backup displayed above - DID YOU SAVE IT?"
echo ""
echo "================================================"
echo ""
EOFBACKUPSCRIPT

# Replace IP placeholder in backup script
sed -i "s/IP_PLACEHOLDER/$DETECTED_IP/g" /root/BTCPayServer/backup-btcpay-and-save-scb.sh

# Make executable
chmod +x /root/BTCPayServer/backup-btcpay-and-save-scb.sh
echo "✅ Backup script created"
echo ""

# Save customer info file
cat > /root/BTCPayServer/CUSTOMER_INFO.txt << EOFINFO
================================================
ripsline Virtual Private Node Installation Details
================================================
Customer: {{CUSTOMER_NAME}}
Order ID: {{CUSTOMER_ID}}
Installation Date: $(date)

Configuration:
- Domain: $BTCPAY_HOST
- rlVPN IP: $DETECTED_IP
- Email: $LETSENCRYPT_EMAIL

Important Files:
- BTCPay location: /root/BTCPayServer
- LND fix script: /root/BTCPayServer/fix-lnd-host.sh
- Backup script: /root/BTCPayServer/backup-btcpay-and-save-scb.sh
- Nginx config: /etc/nginx/sites-available/btcpayserver
- This info file: /root/BTCPayServer/CUSTOMER_INFO.txt

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
touch /root/BTCPayServer/.installed
echo "Installed: $(date)" > /root/BTCPayServer/.installed
echo "Order: {{CUSTOMER_ID}}" >> /root/BTCPayServer/.installed
echo "Domain: $BTCPAY_HOST" >> /root/BTCPayServer/.installed

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
echo "   Template created at: /var/www/html/.well-known/nostr.json"
echo "   Public URL: https://$BTCPAY_HOST/.well-known/nostr.json"
echo "   To configure your identity:"
echo "     1. nano /var/www/html/.well-known/nostr.json"
echo "     2. Replace 'yourname' with your username"
echo "     3. Replace 'YOUR_HEX_PUBKEY_HERE' with your hex public key"
echo "     4. Save and exit (Ctrl+X, Y, Enter)"
echo "   Your identity will be: yourname@$BTCPAY_HOST"
echo "   Docs: https://ripsline.com/docs/nip05"
echo ""
echo "📝 Your details saved to:"
echo "   /root/BTCPayServer/CUSTOMER_INFO.txt"
echo ""
echo "🔄 After BTCPay updates, run:"
echo "   /root/BTCPayServer/fix-lnd-host.sh"
echo ""
echo "📋 Lightning Channel Backups:"
echo "   Script: /root/BTCPayServer/backup-btcpay-and-save-scb.sh"
echo "   ⚠️  Run this after opening ANY new Lightning channel!"
echo ""
echo "================================================"
echo ""
echo "🔄 IMPORTANT: Reboot Recommended"
echo "   A kernel update was probably installed during setup."
echo "   Please reboot your Virtual Private Node now to load the new kernel:"
echo ""
echo "   Type: reboot Press: enter/return"
echo ""
echo "   Your BTCPayServer will automatically restart after reboot."
echo "   Wait 2-3 minutes, then access: https://$BTCPAY_HOST"
echo ""
echo "================================================"
echo ""
echo "⏳ Important Notes:"
echo "   *Bitcoin blockchain sync: 1-5 days"
echo "   *You can start creating BTCPay admin right away!"
echo "   *Create your admin account at the URL above"
echo ""
echo "================================================"
echo ""

#!/bin/bash
set -e

# BTCPay Server Auto-Installer
# Run this on a fresh Ubuntu/Debian VPS as root

echo "================================================"
echo "  BTCPay Server Auto-Installer"
echo "================================================"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
   echo "❌ Please run as root (use: sudo su -)"
   exit 1
fi

# Auto-detect VPS IP
VPS_IP=$(curl -s ifconfig.me || curl -s icanhazip.com || curl -s ipinfo.io/ip)
echo "✅ Detected VPS IP: $VPS_IP"
echo ""

# Get domain from user
read -p "Enter your domain (e.g., btcpay.yourdomain.com): " BTCPAY_HOST
if [ -z "$BTCPAY_HOST" ]; then
    echo "❌ Domain cannot be empty!"
    exit 1
fi

# Get email from user
read -p "Enter your email for Let's Encrypt: " LETSENCRYPT_EMAIL
if [ -z "$LETSENCRYPT_EMAIL" ]; then
    echo "❌ Email cannot be empty!"
    exit 1
fi

# Generate random Lightning Terminal password
LIT_PASSWD=$(openssl rand -base64 16 | tr -d "=+/" | cut -c1-20)

echo ""
echo "================================================"
echo "  Configuration Summary"
echo "================================================"
echo "Domain: $BTCPAY_HOST"
echo "Email: $LETSENCRYPT_EMAIL"
echo "VPS IP: $VPS_IP"
echo "Lightning Terminal Password: $LIT_PASSWD"
echo ""
echo "⚠️  IMPORTANT: Make sure your domain DNS A record"
echo "   points to $VPS_IP before continuing!"
echo ""
read -p "Continue? (yes/no): " CONFIRM

if [ "$CONFIRM" != "yes" ]; then
    echo "Installation cancelled."
    exit 0
fi

echo ""
echo "🔥 Starting installation..."
echo ""

# Configure UFW Firewall
echo "📡 Configuring firewall..."
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
echo "📦 Cloning BTCPay Server..."
git clone https://github.com/btcpayserver/btcpayserver-docker
cd btcpayserver-docker

# Set environment variables
echo "⚙️  Configuring BTCPay Server..."
export BTCPAY_HOST="$BTCPAY_HOST"
export NBITCOIN_NETWORK="mainnet"
export BTCPAYGEN_CRYPTO1="btc"
export BTCPAYGEN_LIGHTNING="lnd"
export BTCPAYGEN_ADDITIONAL_FRAGMENTS="opt-save-storage-xs;opt-add-tor;opt-add-lightning-terminal"
export BTCPAY_ENABLE_SSH=false
export LETSENCRYPT_EMAIL="$LETSENCRYPT_EMAIL"
export ACME_CA_URI="production"
export LIT_PASSWD="$LIT_PASSWD"

# Run setup
echo "🚀 Running BTCPay setup (this may take several minutes)..."
. ./btcpay-setup.sh -i

# Create fix-lnd-host.sh script
echo "🔧 Creating LND host fix script..."
cat > /root/BTCPayServer/fix-lnd-host.sh << 'EOFSCRIPT'
#!/bin/bash
# Auto-fix BTCPAY_ANNOUNCEABLE_HOST after BTCPayServer updates

VPS_IP="{{VPS_IP}}"

# Edit the .env file
sed -i "s/^BTCPAY_ANNOUNCEABLE_HOST=.*/BTCPAY_ANNOUNCEABLE_HOST=$VPS_IP/" /root/BTCPayServer/btcpayserver-docker/.env

# Restart LND
docker restart btcpayserver_lnd_bitcoin

echo "✅ BTCPAY_ANNOUNCEABLE_HOST set to $VPS_IP and LND restarted"
EOFSCRIPT

# Replace placeholder with actual IP
sed -i "s/{{VPS_IP}}/$VPS_IP/" /root/BTCPayServer/fix-lnd-host.sh

# Make executable
chmod +x /root/BTCPayServer/fix-lnd-host.sh

# Run the fix
echo "🔌 Setting LND announceable host..."
/root/BTCPayServer/fix-lnd-host.sh

echo ""
echo "================================================"
echo "  ✅ Installation Complete!"
echo "================================================"
echo ""
echo "Access your BTCPay Server at: https://$BTCPAY_HOST"
echo ""
echo "⚡ Lightning Terminal Password: $LIT_PASSWD"
echo "   (Save this password securely!)"
echo ""
echo "📝 Important files:"
echo "   - BTCPay location: /root/BTCPayServer"
echo "   - LND fix script: /root/BTCPayServer/fix-lnd-host.sh"
echo ""
echo "🔄 After BTCPay updates, run:"
echo "   /root/BTCPayServer/fix-lnd-host.sh"
echo ""
echo "================================================"

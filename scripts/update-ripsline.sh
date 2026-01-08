#!/bin/bash
set -e

# ripsline Update Script
# Updates ripsline scripts and configurations (NOT BTCPay - use btcpay-update.sh for that)
# Location: /root/ripsline/scripts/update-ripsline.sh

echo "================================================"
echo "  ripsline Virtual Private Node - Update"
echo "================================================"
echo ""

# Check root
if [ "$EUID" -ne 0 ]; then 
   echo "❌ This script must be run as root"
   echo "   Please run: sudo /root/ripsline/scripts/update-ripsline.sh"
   exit 1
fi

# Verify ripsline is installed
if [ ! -f "/root/ripsline/.installed" ]; then
    echo "❌ ripsline installation not found!"
    exit 1
fi

echo "This will update ripsline scripts and configurations."
echo ""
echo "⚠️  Note: This does NOT update BTCPay Server itself."
echo "   To update BTCPay, use: cd /root/ripsline/btcpayserver-docker && btcpay-update.sh"
echo ""
echo "What will be updated:"
echo "  • Helper scripts in /root/ripsline/scripts/"
echo "  • Configuration templates in /root/ripsline/config/"
echo ""
echo "What will NOT be touched:"
echo "  • BTCPay Server installation"
echo "  • Your BTCPay data and settings"
echo ""

read -p "Continue with update? (y/n): " CONFIRM

if [ "$CONFIRM" != "y" ]; then
    echo "Update cancelled."
    exit 0
fi

echo ""

# Create timestamp for backups
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

echo "📥 Downloading latest ripsline updates from GitHub..."
cd /tmp
rm -rf virtual-private-node
git clone https://github.com/ripsline/virtual-private-node.git

echo ""
echo "🔍 Checking for configuration changes..."

# Check if BTCPay nginx config changed
BTCPAY_NGINX_CHANGED=false
if ! diff -q /root/ripsline/config/nginx-btcpay.conf /tmp/virtual-private-node/config/nginx-btcpay.conf > /dev/null 2>&1; then
    BTCPAY_NGINX_CHANGED=true
fi

# Check if Haven nginx config changed
HAVEN_NGINX_CHANGED=false
if ! diff -q /root/ripsline/config/nginx-haven.conf /tmp/virtual-private-node/config/nginx-haven.conf > /dev/null 2>&1; then
    HAVEN_NGINX_CHANGED=true
fi

# Check if Haven is installed
HAVEN_INSTALLED=false
if [ -f "/etc/nginx/sites-available/haven-relay" ]; then
    HAVEN_INSTALLED=true
fi

# Backup active nginx configs if they're being updated
if [ "$BTCPAY_NGINX_CHANGED" = true ]; then
    echo "📋 Backing up BTCPay nginx config..."
    cp /etc/nginx/sites-available/ripsline-btcpay /etc/nginx/sites-available/ripsline-btcpay.backup-$TIMESTAMP
    echo "✅ BTCPay config backed up to: /etc/nginx/sites-available/ripsline-btcpay.backup-$TIMESTAMP"
fi

if [ "$HAVEN_NGINX_CHANGED" = true ] && [ "$HAVEN_INSTALLED" = true ]; then
    echo "📋 Backing up Haven nginx config..."
    cp /etc/nginx/sites-available/haven-relay /etc/nginx/sites-available/haven-relay.backup-$TIMESTAMP
    echo "✅ Haven config backed up to: /etc/nginx/sites-available/haven-relay.backup-$TIMESTAMP"
fi

echo ""
echo "📋 Updating scripts..."
cp -r /tmp/virtual-private-node/scripts/* /root/ripsline/scripts/
chmod +x /root/ripsline/scripts/*.sh

echo "📋 Updating config templates..."
cp -r /tmp/virtual-private-node/config/* /root/ripsline/config/

# Cleanup
rm -rf /tmp/virtual-private-node

echo ""
echo "✅ Update complete!"
echo ""
echo "Updated files:"
echo "  • Scripts: /root/ripsline/scripts/"
echo "  • Config templates: /root/ripsline/config/"
echo ""

# Show BTCPay nginx update instructions if config changed
if [ "$BTCPAY_NGINX_CHANGED" = true ]; then
    echo "================================================"
    echo "📝 BTCPay Nginx Config Updated"
    echo "================================================"
    echo ""
    
    # Try to read domain from .env
    if [ -f "/root/ripsline/.env" ]; then
        DOMAIN=$(grep "^BTCPAY_HOST=" /root/ripsline/.env | cut -d'=' -f2 | tr -d '"' | tr -d "'")
        
        if [ -n "$DOMAIN" ]; then
            echo "To apply the updated BTCPay nginx config:"
            echo ""
            echo "💡 Run this command:"
            echo "   sudo cp /root/ripsline/config/nginx-btcpay.conf /etc/nginx/sites-available/ripsline-btcpay && sudo sed -i 's/DOMAIN_PLACEHOLDER/$DOMAIN/g' /etc/nginx/sites-available/ripsline-btcpay && sudo nginx -t && sudo systemctl reload nginx"
            echo ""
            echo "🔄 To rollback if there are issues:"
            echo "   sudo cp /etc/nginx/sites-available/ripsline-btcpay.backup-$TIMESTAMP /etc/nginx/sites-available/ripsline-btcpay && sudo systemctl reload nginx"
        else
            echo "⚠️  Could not detect your domain from .env file"
            echo "   Manually update /etc/nginx/sites-available/ripsline-btcpay if needed"
        fi
    else
        echo "⚠️  .env file not found, could not detect domain"
        echo "   Manually update /etc/nginx/sites-available/ripsline-btcpay if needed"
    fi
    echo ""
    echo "================================================"
    echo ""
fi

# Show Haven nginx update instructions if config changed AND Haven is installed
if [ "$HAVEN_NGINX_CHANGED" = true ] && [ "$HAVEN_INSTALLED" = true ]; then
    echo "================================================"
    echo "📝 Haven Nginx Config Updated"
    echo "================================================"
    echo ""
    
    # Try to read Haven relay domain from Haven's .env
    if [ -f "/home/haven/.env" ]; then
        RELAY_DOMAIN=$(grep "^RELAY_URL=" /home/haven/.env | cut -d'=' -f2 | tr -d '"' | tr -d "'")
        
        if [ -n "$RELAY_DOMAIN" ]; then
            echo "To apply the updated Haven nginx config:"
            echo ""
            echo "💡 Run this command:"
            echo "   sudo cp /root/ripsline/config/nginx-haven.conf /etc/nginx/sites-available/haven-relay && sudo sed -i 's/RELAY_DOMAIN/$RELAY_DOMAIN/g' /etc/nginx/sites-available/haven-relay && sudo nginx -t && sudo systemctl reload nginx"
            echo ""
            echo "🔄 To rollback if there are issues:"
            echo "   sudo cp /etc/nginx/sites-available/haven-relay.backup-$TIMESTAMP /etc/nginx/sites-available/haven-relay && sudo systemctl reload nginx"
        else
            echo "⚠️  Could not detect your relay domain from Haven .env file"
            echo "   Manually update /etc/nginx/sites-available/haven-relay if needed"
        fi
    else
        echo "⚠️  Haven .env file not found, could not detect relay domain"
        echo "   Manually update /etc/nginx/sites-available/haven-relay if needed"
    fi
    echo ""
    echo "================================================"
    echo ""
fi

echo "To update BTCPay Server itself, run: cd /root/ripsline/btcpayserver-docker && btcpay-update.sh"
echo ""

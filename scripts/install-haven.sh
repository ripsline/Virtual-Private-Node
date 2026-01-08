#!/bin/bash
set -e

# Haven Relay Installer
# Installs Haven personal Nostr relay for ripsline VPN
# Should be run from: sudo /root/ripsline/scripts/install-haven.sh

echo "================================================"
echo "  Haven Relay Installer"
echo "  Personal Nostr Relay for ripsline VPN"
echo "================================================"
echo ""

# Check root
if [ "$EUID" -ne 0 ]; then 
   exec sudo "$0" "$@"
   exit
fi

echo "✅ Running as root"
echo ""

# Verify ripsline is installed
if [ ! -f "/root/ripsline/.installed" ]; then
    echo "❌ ripsline Virtual Private Node not found!"
    echo "   Please install ripsline first with install-ripsline.sh"
    exit 1
fi

# Try to read email from ripsline .env
LETSENCRYPT_EMAIL=""
if [ -f "/root/ripsline/.env" ]; then
    LETSENCRYPT_EMAIL=$(grep "^LETSENCRYPT_EMAIL=" /root/ripsline/.env | cut -d'=' -f2)
    if [ -n "$LETSENCRYPT_EMAIL" ]; then
        echo "✅ Using email from ripsline installation: $LETSENCRYPT_EMAIL"
        echo ""
    fi
else
    echo "⚠️  Could not find /root/ripsline/.env"
    echo "   Falling back to manual configuration"
    echo ""
fi

# Get relay domain
echo "📍 Enter your relay domain"
echo "   Example: relay.yourdomain.com"
echo ""
echo "⚠️  Note: You will need to create an A record for this subdomain"
echo "   in your domain registrar pointing to your VPS IP before proceeding."
echo ""
read -p "Enter relay domain: " RELAY_DOMAIN
echo ""

# If email wasn't found in .env, prompt for it
if [ -z "$LETSENCRYPT_EMAIL" ]; then
    echo "📧 SSL Certificate Email"
    read -p "Enter email for SSL certificate notifications: " LETSENCRYPT_EMAIL
    echo ""
fi

# Detect IP
VPS_IP=$(curl -4 -s ifconfig.me)
echo "VPS IP: $VPS_IP"
echo ""

# DNS check
echo "Checking DNS..."
RESOLVED_IP=$(dig +short $RELAY_DOMAIN @8.8.8.8 | tail -1)

if [ "$RESOLVED_IP" != "$VPS_IP" ]; then
    echo "⚠️  DNS: $RELAY_DOMAIN → $RESOLVED_IP (expected $VPS_IP)"
    read -p "Continue anyway? (y/n): " CONTINUE
    [ "$CONTINUE" != "y" ] && exit 1
else
    echo "✅ DNS correct"
fi
echo ""

# Get owner npub
echo "Haven requires your Nostr npub (public key)"
echo "Find your npub in your Nostr client settings"
echo "(Damus, Amethyst, Primal, Nostur, etc.)"
echo ""
read -p "Enter your npub (starts with npub1...): " OWNER_NPUB
echo ""

if [ -z "$OWNER_NPUB" ]; then
    echo "❌ npub is required for Haven to function"
    echo "   You can find it in your Nostr client settings"
    exit 1
fi

# Validate npub format (basic check)
if [[ ! "$OWNER_NPUB" =~ ^npub1 ]]; then
    echo "⚠️  Warning: npub should start with 'npub1'"
    read -p "Continue anyway? (y/n): " CONTINUE
    [ "$CONTINUE" != "y" ] && exit 1
fi
echo ""

# Create user
echo "Creating haven user..."
if id "haven" &>/dev/null; then
    echo "   User 'haven' already exists"
else
    adduser --disabled-password --gecos "" haven
    echo "✅ User 'haven' created"
fi
mkdir -p /home/haven
cd /home/haven
echo ""

# Download Haven
echo "Downloading Haven..."
LATEST_URL=$(curl -sL https://api.github.com/repos/bitvora/haven/releases/latest | grep "browser_download_url.*Linux_x86_64.tar.gz" | cut -d '"' -f 4)

if [ -z "$LATEST_URL" ]; then
    echo "❌ Could not find Haven download URL"
    echo "   Check: https://github.com/bitvora/haven/releases"
    exit 1
fi

echo "   Downloading from: $LATEST_URL"
curl -L -o haven.tar.gz "$LATEST_URL"
tar -xzf haven.tar.gz
chmod +x haven
echo "✅ Haven downloaded and extracted"
echo ""

# Copy example files to working files
echo "Setting up configuration files..."
cp .env.example .env
cp relays_import.example.json relays_import.json
cp relays_blastr.example.json relays_blastr.json
echo "✅ Configuration templates created"
echo ""

# Update .env with user's values using sed
echo "Configuring Haven with your settings..."
sed -i "s|^OWNER_NPUB=.*|OWNER_NPUB=\"$OWNER_NPUB\"|" .env
sed -i "s|^RELAY_URL=.*|RELAY_URL=\"$RELAY_DOMAIN\"|" .env

# Also update individual relay npubs to match owner
sed -i "s|^PRIVATE_RELAY_NPUB=.*|PRIVATE_RELAY_NPUB=\"$OWNER_NPUB\"|" .env
sed -i "s|^CHAT_RELAY_NPUB=.*|CHAT_RELAY_NPUB=\"$OWNER_NPUB\"|" .env
sed -i "s|^OUTBOX_RELAY_NPUB=.*|OUTBOX_RELAY_NPUB=\"$OWNER_NPUB\"|" .env
sed -i "s|^INBOX_RELAY_NPUB=.*|INBOX_RELAY_NPUB=\"$OWNER_NPUB\"|" .env

echo "✅ Haven configured"
echo ""

# Create update-haven.sh helper script
echo "Creating update-haven.sh helper script..."
cat > update-haven.sh << 'UPDATESCRIPT'
#!/bin/bash
set -e

echo "================================================"
echo "  Haven - Update to Latest Version"
echo "================================================"
echo ""

# Check root
if [ "$EUID" -ne 0 ]; then 
   echo "❌ Run with sudo: sudo /home/haven/update-haven.sh"
   exit 1
fi

echo "Checking for latest Haven release..."
LATEST_URL=$(curl -sL https://api.github.com/repos/bitvora/haven/releases/latest | grep "browser_download_url.*Linux_x86_64.tar.gz" | cut -d '"' -f 4)
LATEST_VERSION=$(curl -sL https://api.github.com/repos/bitvora/haven/releases/latest | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST_URL" ]; then
    echo "❌ Could not find Haven release"
    exit 1
fi

echo "Latest version: $LATEST_VERSION"
echo ""
read -p "Update Haven to latest version? (y/n): " CONFIRM

if [ "$CONFIRM" != "y" ]; then
    echo "Update cancelled."
    exit 0
fi

echo ""
echo "Stopping Haven service..."
systemctl stop haven

echo "Backing up current version..."
if [ -f /home/haven/haven ]; then
    cp /home/haven/haven /home/haven/haven.backup
    echo "✅ Backup saved to: /home/haven/haven.backup"
fi

echo "Downloading latest Haven..."
cd /home/haven
rm -f haven.tar.gz
curl -L -o haven.tar.gz "$LATEST_URL"

echo "Extracting..."
tar -xzf haven.tar.gz
chmod +x haven
chown haven:haven haven

echo "Starting Haven service..."
systemctl start haven

sleep 3

if systemctl is-active --quiet haven; then
    echo ""
    echo "✅ Haven updated successfully to $LATEST_VERSION"
    echo ""
    echo "Commands:"
    echo "  systemctl status haven  - Check status"
    echo "  journalctl -u haven -f  - View logs"
else
    echo ""
    echo "⚠️  Haven may not have started properly"
    echo ""
    echo "To rollback to previous version:"
    echo "  sudo systemctl stop haven"
    echo "  sudo cp /home/haven/haven.backup /home/haven/haven"
    echo "  sudo systemctl start haven"
fi
UPDATESCRIPT

chmod +x update-haven.sh
echo "✅ update-haven.sh created"
echo ""

# Set ownership
chown -R haven:haven /home/haven
echo "✅ Ownership set"
echo ""

# Create systemd service
echo "Creating systemd service..."
cat > /etc/systemd/system/haven.service << EOF
[Unit]
Description=Haven Nostr Relay
After=network.target

[Service]
Type=simple
ExecStart=/home/haven/haven
WorkingDirectory=/home/haven
User=haven
Group=haven
Restart=always
RestartSec=10
MemoryMax=1G

# Security Hardening
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=read-only
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictRealtime=true
RestrictAddressFamilies=AF_INET AF_INET6
RestrictNamespaces=true
LockPersonality=true
ReadWritePaths=/home/haven

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable haven
systemctl start haven
sleep 3
echo ""

# Check if running
if systemctl is-active --quiet haven; then
    echo "✅ Haven service running"
else
    echo "❌ Haven failed to start"
    echo ""
    echo "Checking logs:"
    journalctl -u haven -n 30 --no-pager
    echo ""
    echo "Check configuration: nano /home/haven/.env"
    exit 1
fi
echo ""

# Test localhost
echo "Testing localhost:3355..."
sleep 2
if curl -s http://127.0.0.1:3355 > /dev/null 2>&1; then
    echo "✅ Haven responding on localhost"
else
    echo "⚠️  Not responding yet (may need more time)"
fi
echo ""

# SSL
echo "Getting SSL certificate..."
certbot certonly --nginx -d $RELAY_DOMAIN --non-interactive --agree-tos --email $LETSENCRYPT_EMAIL

if [ $? -eq 0 ]; then
    echo "✅ SSL certificate obtained"
else
    echo "⚠️  SSL certificate failed - you may need to configure it manually"
    echo "   Try: sudo certbot certonly --nginx -d $RELAY_DOMAIN"
fi
echo ""

# Nginx config
echo "Creating nginx configuration..."
cat /root/ripsline/config/nginx-haven.conf | sed "s/RELAY_DOMAIN/$RELAY_DOMAIN/g" > /etc/nginx/sites-available/haven-relay

ln -sf /etc/nginx/sites-available/haven-relay /etc/nginx/sites-enabled/

if nginx -t; then
    systemctl reload nginx
    echo "✅ Nginx configured and reloaded"
else
    echo "❌ Nginx configuration has errors"
    nginx -t
    exit 1
fi
echo ""

# Create info file
cat > /home/haven/HAVEN_INFO.txt << INFOFILE
================================================
Haven Relay Installation
================================================
Installation Date: $(date)
Domain: $RELAY_DOMAIN
VPS IP: $VPS_IP
Owner npub: $OWNER_NPUB

Relay Endpoints:
  Outbox (public):  wss://$RELAY_DOMAIN
  Private storage:  wss://$RELAY_DOMAIN/private
  Chat (WoT):       wss://$RELAY_DOMAIN/chat
  Inbox:            wss://$RELAY_DOMAIN/inbox
  Blossom Media:    https://$RELAY_DOMAIN

Configuration:
  Haven directory:  /home/haven
  Config file:      /home/haven/.env
  Service:          /etc/systemd/system/haven.service
  Nginx config:     /etc/nginx/sites-available/haven-relay

Helper Scripts:
  Import notes:     sudo /root/ripsline/scripts/import-notes.sh
  Update Haven:     sudo /home/haven/update-haven.sh

Useful Commands:
  systemctl status haven      - Check service status
  journalctl -u haven -f      - View live logs
  systemctl restart haven     - Restart service
  nano /home/haven/.env       - Edit configuration

To Use Your Relay:
  1. Open your Nostr client (Damus, Amethyst, Primal, etc.)
  2. Go to relay settings
  3. Add all relay endpoints above
  4. Set as your outbox relay (NIP-65 relay list)
  5. Your notes will now be stored on your personal relay!

Optional - Import Existing Notes:
  If you have existing Nostr notes on other relays:
  sudo /root/ripsline/scripts/import-notes.sh

Advanced Configuration:
  Edit /home/haven/.env to customize:
  - Rate limiters
  - Web of Trust settings
  - Backup providers (S3)
  - Database backend (BadgerDB/LMDB)
  - And more...

Support: support@ripsline.com
Docs: https://github.com/bitvora/haven
================================================
INFOFILE

chown haven:haven /home/haven/HAVEN_INFO.txt

echo "================================================"
echo "  ✅ Haven Relay Installation Complete!"
echo "================================================"
echo ""
echo "🎉 Your personal Nostr relay is ready!"
echo ""
echo "📍 Relay Endpoints:"
echo "   Outbox:   wss://$RELAY_DOMAIN"
echo "   Private:  wss://$RELAY_DOMAIN/private"
echo "   Chat:     wss://$RELAY_DOMAIN/chat"
echo "   Inbox:    wss://$RELAY_DOMAIN/inbox"
echo "   Blossom:  https://$RELAY_DOMAIN"
echo ""
echo "📱 To Use Your Relay:"
echo "   1. Open your Nostr client (Damus, Amethyst, Primal, etc.)"
echo "   2. Go to relay settings"
echo "   3. Add all relay endpoints above"
echo "   4. Set as your outbox relay (NIP-65 relay list)"
echo "   5. Your notes will now be stored on your personal relay!"
echo ""
echo "📋 Helper Scripts:"
echo "   Import existing notes:  sudo /root/ripsline/scripts/import-notes.sh"
echo "   Update Haven:           sudo /home/haven/update-haven.sh"
echo ""
echo "📖 Full info: /home/haven/HAVEN_INFO.txt"
echo ""
echo "🔧 Useful Commands:"
echo "   systemctl status haven     - Check if running"
echo "   journalctl -u haven -f     - View logs"
echo ""
echo "================================================"
echo ""

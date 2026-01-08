#!/bin/bash
set -e

echo "================================================"
echo "  Haven - Import Existing Nostr Notes"
echo "================================================"
echo ""

# Check root
if [ "$EUID" -ne 0 ]; then 
   echo "❌ Run with sudo: sudo /root/ripsline/scripts/import-notes.sh"
   exit 1
fi

echo "This will import your existing Nostr notes from other relays."
echo "Import sources are configured in: /home/haven/relays_import.json"
echo ""
echo "Current import relays:"
cat /home/haven/relays_import.json | grep "wss://" | sed 's/.*"\(wss:\/\/[^"]*\)".*/  - \1/'
echo ""
echo "⚠️  Import can take 5-60 minutes depending on your note history."
echo ""
read -p "Continue with import? (y/n): " CONFIRM

if [ "$CONFIRM" != "y" ]; then
    echo "Import cancelled."
    exit 0
fi

echo ""
echo "Stopping Haven service..."
systemctl stop haven

echo "Starting import..."
echo "(This may take a while - grab a coffee ☕)"
echo ""

cd /home/haven
sudo -u haven ./haven --import

echo ""
echo "Starting Haven service..."
systemctl start haven

sleep 3

if systemctl is-active --quiet haven; then
    echo "✅ Haven restarted successfully"
    echo ""
    echo "Import complete! Your notes are now in your personal relay."
else
    echo "⚠️  Haven may not have started properly"
    echo "   Check status: systemctl status haven"
    echo "   Check logs: journalctl -u haven -f"
fi

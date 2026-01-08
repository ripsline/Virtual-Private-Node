#!/bin/bash
# BTCPay Server Backup Script with Channel Backup (SCB) Display
# Creates full backup and displays channel.backup for easy copy/paste
# Location: /root/ripsline/scripts/backup-and-scb.sh

set -e

echo "================================================"
echo "  BTCPay Server Backup & Channel Backup (SCB)"
echo "================================================"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
   echo "❌ This script must be run as root"
   echo "   Please run: sudo /root/ripsline/scripts/backup-and-scb.sh"
   exit 1
fi

# Detect original SSH user (before sudo)
if [ -n "$SUDO_USER" ]; then
    ACTUAL_USER="$SUDO_USER"
else
    ACTUAL_USER=$(who am i | awk '{print $1}')
fi

if [ -z "$ACTUAL_USER" ] || [ "$ACTUAL_USER" = "root" ]; then
    echo "⚠️  Warning: Could not detect non-root SSH user"
    echo "   Using 'root' for scp command (may not work on your VPS)"
    ACTUAL_USER="root"
fi

VPS_IP="IP_PLACEHOLDER"

# Date for backup filename reference
BACKUP_DATE=$(date +%Y-%m-%d)

echo "🔄 Step 1: Creating full BTCPay Server backup..."
echo "   (This includes database, settings, and all data)"
echo ""

# Change to btcpay-docker directory
cd /root/ripsline/btcpayserver-docker

# Run the official BTCPay backup script
if ./btcpay-backup.sh; then
    echo ""
    echo "✅ Full backup created successfully"
    echo ""
    echo "📋 Copying backup to accessible location..."
    cp /var/lib/docker/volumes/backup_datadir/_data/backup.tar.gz /tmp/backup.tar.gz
    chmod 644 /tmp/backup.tar.gz
    echo "✅ Backup copied to: /tmp/backup.tar.gz"
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
echo "   scp -i ~/.ssh/id_ed25519_VirtualPrivateNode $ACTUAL_USER@$VPS_IP:/tmp/backup.tar.gz ./"
echo ""
echo "3. The file will download to your current directory"
echo ""
echo "💡 Note: Backup will remain in /tmp/backup.tar.gz until:"
echo "   • Server reboot (auto-deleted)"
echo "   • Manual deletion: rm /tmp/backup.tar.gz"
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

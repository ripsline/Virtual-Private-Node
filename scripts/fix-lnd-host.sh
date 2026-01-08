#!/bin/bash
# Auto-fix BTCPAY_ANNOUNCEABLE_HOST after BTCPayServer updates
# Location: /root/ripsline/scripts/fix-lnd-host.sh

VPS_IP="IP_PLACEHOLDER"

# Edit the .env file
sed -i "s/^BTCPAY_ANNOUNCEABLE_HOST=.*/BTCPAY_ANNOUNCEABLE_HOST=$VPS_IP/" /root/ripsline/.env

# Recreate LND container with updated config (reads new .env values)
cd /root/ripsline/btcpayserver-docker/Generated
docker-compose -f docker-compose.generated.yml up -d --force-recreate lnd_bitcoin

# Restart LIT
docker restart generated-lnd_lit-1

echo "✅ BTCPAY_ANNOUNCEABLE_HOST set to $VPS_IP, LND recreated with new config, LIT restarted"

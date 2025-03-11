#!/usr/bin/env bash
set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

check() {
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓${NC} $1"
    else
        echo -e "${RED}✗${NC} $1"
        exit 1
    fi
}

echo "Running tests..."

# Check if binaries exist and are executable
echo "Checking binaries..."
test -x /usr/local/bin/holochain
check "Holochain binary exists"

/usr/local/bin/holochain --version
check "Holochain binary is runnable"

test -x /usr/local/bin/lair-keystore
check "Lair keystore binary exists"

/usr/local/bin/lair-keystore --version
check "Lair keystore binary is runnable"

# Check if directories exist with correct permissions
echo "Checking directories..."
test -d "/var/lib/holochain/data/lair"
check "Lair data directory exists"

# Check if lair is initialized
test -f "/var/lib/holochain/data/lair/lair-keystore-config.yaml"
check "Lair keystore is initialized"

# Check if services are properly configured
echo "Checking service configuration..."
systemctl cat lair-keystore
check "Lair keystore service is configured"

systemctl cat holochain
check "Holochain service is configured"

# Check if services are running
echo "Checking service status..."
systemctl status lair-keystore
check "Lair keystore service status"

systemctl is-active lair-keystore
check "Lair keystore service is active"

systemctl status holochain
check "Holochain service status"

systemctl is-active holochain
check "Holochain service is active"

echo -e "\n${GREEN}All tests passed!${NC}" 
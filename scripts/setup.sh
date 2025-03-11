#!/usr/bin/env bash
set -ex

# Function to wait for apt locks to be released
wait_for_apt_locks() {
    local timeout=600  # 10 minutes timeout
    local start_time=$(date +%s)

    while true; do
        if ! fuser /var/lib/dpkg/lock >/dev/null 2>&1 && \
           ! fuser /var/lib/apt/lists/lock >/dev/null 2>&1 && \
           ! fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1; then
            return 0
        fi

        local current_time=$(date +%s)
        local elapsed_time=$((current_time - start_time))
        
        if [ $elapsed_time -ge $timeout ]; then
            echo "Timeout waiting for apt locks after ${timeout} seconds"
            return 1
        fi

        echo "Waiting for apt locks to be released... (${elapsed_time}s elapsed)"
        sleep 5
    done
}

# Function to run apt commands with retries
apt_install() {
    local max_attempts=5
    local attempt=1

    while [ $attempt -le $max_attempts ]; do
        echo "Attempt $attempt of $max_attempts: Running apt-get $*"
        
        if wait_for_apt_locks; then
            if apt-get "$@"; then
                return 0
            fi
        fi

        attempt=$((attempt + 1))
        if [ $attempt -le $max_attempts ]; then
            echo "Retrying in 30 seconds..."
            sleep 30
        fi
    done

    echo "Failed to run apt-get $* after $max_attempts attempts"
    return 1
}

# Create directories with proper permissions
rm -rf /var/lib/holochain || true
mkdir -p /var/lib/holochain/{config,data}/{holochain,lair}

# Set root ownership and permissions
chown -R root:root /var/lib/holochain
chmod -R 755 /var/lib/holochain  # rwxr-xr-x

# Enable persistent journal and make it readable
mkdir -p /var/log/journal
systemd-tmpfiles --create --prefix /var/log/journal
chmod -R a+rX /var/log/journal
systemctl restart systemd-journald

# Install dependencies
apt_install update
apt_install install -y curl

# Stop services if they are running
systemctl stop holochain || true
systemctl stop lair-keystore || true
# Wait for processes to fully stop
sleep 5

# Ensure no lingering processes
pkill -f holochain || true
pkill -f lair-keystore || true
sleep 2

# Install holochain and lair-keystore
curl -L -o /usr/local/bin/holochain https://github.com/matthme/holochain-binaries/releases/download/holochain-binaries-0.4.1/holochain-v0.4.1-x86_64-unknown-linux-gnu
curl -L -o /usr/local/bin/lair-keystore https://github.com/matthme/holochain-binaries/releases/download/lair-binaries-0.5.3/lair-keystore-v0.5.3-x86_64-unknown-linux-gnu
curl -L -o /usr/local/bin/hc https://github.com/matthme/holochain-binaries/releases/download/hc-binaries-0.4.1/hc-v0.4.1-x86_64-unknown-linux-gnu

chmod 755 /usr/local/bin/holochain
chmod 755 /usr/local/bin/lair-keystore
chmod 755 /usr/local/bin/hc
chown root:root /usr/local/bin/holochain
chown root:root /usr/local/bin/lair-keystore
chown root:root /usr/local/bin/hc
# Copy config files first
cp /tmp/services/config/conductor-config.yaml /var/lib/holochain/config/
chown root:root /var/lib/holochain/config/conductor-config.yaml
chmod 644 /var/lib/holochain/config/conductor-config.yaml

# Set passwords
LAIR_PASSWORD=${LAIR_PASSWORD:-"secure-password"}
HOLOCHAIN_PASSWORD=${HOLOCHAIN_PASSWORD:-"secure-password"}

# Initialize lair-keystore and get connection URL
echo "Initializing lair-keystore..."
printf "%s" "$LAIR_PASSWORD" | /usr/local/bin/lair-keystore --lair-root '/var/lib/holochain/data/lair' init --piped

# Start lair-keystore temporarily to get URL
printf "%s" "$LAIR_PASSWORD" | /usr/local/bin/lair-keystore --lair-root '/var/lib/holochain/data/lair' server --piped &
LAIR_PID=$!
sleep 2  # Give lair-keystore time to start

# Get lair connection URL and update config
LAIR_CONNECTION_URL=$(/usr/local/bin/lair-keystore --lair-root '/var/lib/holochain/data/lair' url)
echo "Got lair connection URL: ${LAIR_CONNECTION_URL}"

# Update connection_url in config
sed -i "s|connection_url:.*|connection_url: \"${LAIR_CONNECTION_URL}\"|" /var/lib/holochain/config/conductor-config.yaml

# Kill temporary lair-keystore
kill $LAIR_PID

# Show updated config
echo "Updated conductor config:"
cat /var/lib/holochain/config/conductor-config.yaml

# Copy and set permissions for systemd service files
cp /tmp/services/systemd/*.service /etc/systemd/system/
chown root:root /etc/systemd/system/lair-keystore.service
chown root:root /etc/systemd/system/holochain.service
chmod 644 /etc/systemd/system/lair-keystore.service
chmod 644 /etc/systemd/system/holochain.service

# Enable and start services
systemctl daemon-reload
systemctl enable --now lair-keystore

sleep 5  # Give lair-keystore more time to start and create socket
systemctl enable --now holochain

# Show service status
echo "Lair-keystore service status:"
systemctl status lair-keystore
echo "Holochain service status:"
systemctl status holochain
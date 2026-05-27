# Setting Up an Always-On Holochain Node

This guide walks you through setting up a production-ready, always-on Holochain node on DigitalOcean. The process is broken down into three main parts:

1. **Deploying a DigitalOcean droplet** with Pulumi
2. **Choosing how to generate your agent keys**
3. **Downloading and installing your .happ file** with the respective agent key

## Part 1: Deploying a Droplet with Pulumi

Nodes are deployed using [Pulumi](https://www.pulumi.com/) with the DigitalOcean provider. The Pulumi program in this repository provisions droplets using cloud-init, which installs Holochain, Lair Keystore, and all required services automatically at first boot.

### Prerequisites

- [Pulumi CLI](https://www.pulumi.com/docs/install/)
- [Go](https://go.dev/dl/) (1.21+)
- A [DigitalOcean](https://www.digitalocean.com/) account with an API token
- SSH key added to your DigitalOcean account

### Configure Pulumi

```bash
# Set your DigitalOcean API token
pulumi config set --secret digitalocean:token

# Set the InfluxDB token for metrics shipping
pulumi config set --secret heart:influx-token

# Set the DigitalOcean project to assign droplets to
pulumi config set heart:project-name Holo

# Set the number of always-online nodes to deploy
pulumi config set heart:heart-always-online-count 1
```

### Deploy

```bash
pulumi up
```

Pulumi will show a preview of resources to be created. Confirm to proceed. The droplet will be created and cloud-init will run automatically on first boot to install and configure all services.

### Verify Deployment

Once the droplet is created, SSH in and check that services are running:

```bash
ssh root@YOUR_DROPLET_IP

# Check core services
systemctl status holochain
systemctl status lair-keystore

# Check registration status (runs on every boot)
systemctl status holochain-register

# View logs
journalctl -u holochain -f
journalctl -u holochain-register -f
```

Registration requires an admin to approve the node's key on the auth server. Once approved, the node will receive credentials and Holochain will be restarted automatically.

### Tear Down

```bash
pulumi destroy
```

## Part 2: Choose How to Generate Your Agent Keys

The node's agent key is created automatically by `holochain-register` during first boot and stored at `/var/lib/holochain/agent-pub-key`. You can also generate or import keys manually.

### Option 1: Use the Auto-Generated Key (Simplest)

After registration completes, retrieve the key the node created:

```bash
ssh root@YOUR_DROPLET_IP
cat /var/lib/holochain/agent-pub-key
```

Use this key when installing apps — see the README for the exact `hc sandbox call` command.

### Option 2: Generate a New Key within Lair

Generate an additional key directly in the Lair keystore on your node:

```bash
ssh root@YOUR_DROPLET_IP

hc sandbox call --running 8800 new-agent
```

Output example:
```
hc-sandbox: Added agent uhCAkN5IokFxdryZWUzR6Nb89wjVsiENaXp8uGsKbGJpT1SKxPzEm
```

Save this agent key — you'll need it for app installation.

### Option 3: Import from hc_seed_bundle_cli (Most Control)

For maximum control over your keys, use `hc_seed_bundle_cli` from [crates.io](https://crates.io/crates/hc_seed_bundle_cli):

#### 3a. Install hc_seed_bundle_cli locally

```bash
cargo install hc_seed_bundle_cli
```

#### 3b. Generate a seed bundle

```bash
hc_seed_bundle_cli generate -o my-agent-seed.yaml

# Or import from an existing seed phrase
hc_seed_bundle_cli generate --from-mnemonic "your twelve word mnemonic phrase here" -o my-agent-seed.yaml
```

#### 3c. Import to Lair on your droplet

```bash
scp my-agent-seed.yaml root@YOUR_DROPLET_IP:/tmp/

ssh root@YOUR_DROPLET_IP

hc_seed_bundle_cli import-lair /tmp/my-agent-seed.yaml

# Get the agent key for use in app installation
hc_seed_bundle_cli agent-key /tmp/my-agent-seed.yaml
```

## Part 3: Download and Install Your .happ File

### 1. Prepare Your App

```bash
ssh root@YOUR_DROPLET_IP

mkdir -p /var/lib/holochain/apps/

# Download your .happ file (replace with your actual URL)
curl -L -o /var/lib/holochain/apps/your-app.happ https://your-app-url.com/your-app.happ
```

### 2. Install the App

#### If you used Option 1 (Auto-generated key):

```bash
AGENT_KEY=$(cat /var/lib/holochain/agent-pub-key)
hc sandbox call --running 8800 install-app \
  --app-id "your-app-id" \
  --agent-key "${AGENT_KEY}" \
  /var/lib/holochain/apps/your-app.happ
```

#### If you used Option 2 or 3 (Pre-existing key):

```bash
hc sandbox call --running 8800 install-app \
  --app-id "your-app-id" \
  --agent-key uhCAkN5IokFxdryZWUzR6Nb89wjVsiENaXp8uGsKbGJpT1SKxPzEm \
  /var/lib/holochain/apps/your-app.happ
```

### 3. Verify Installation

```bash
# List installed apps
hc sandbox call --running 8800 list-apps

# Enable the app if needed
hc sandbox call --running 8800 enable-app --app-id "your-app-id"
```

## Post-Installation

### Wire the App into `hc-http-gw`

The explorer (and any other web2 caller) reaches your installed app via
the per-droplet `hc-http-gw` and a shared Cloudflare tunnel. After
`hc sandbox call install-app` succeeds:

```bash
# Tell hc-http-gw which app id(s) to expose, and which zome functions
# to allow on each.
hc-http-gw-configure --app-id "your-app-id" --allowed-fns '*'

# Verify gateway -> conductor on the local loopback:
curl -i http://127.0.0.1:8090/health
# Expected: HTTP/1.1 200 OK, body "Ok"

# Verify the full chain (browser -> CF worker -> tunnel -> hc-http-gw):
source /etc/heart-fleet/metadata
curl -i "https://${HEART_GATEWAY_HOSTNAME}/health"
# Expected: HTTP/2 200 from a Cloudflare-fronted response.
```

`hc-http-gw-configure` is safe to re-run any time the installed app
list changes — it rewrites `/etc/hc-http-gw/env` cleanly rather than
appending, and the `--app-id` flag can be passed multiple times to
expose more than one app from the same droplet.

**Important**: the `hc-http-gw` binary may be absent from
`/usr/local/bin` until the
[upstream binary release](./upstream-hc-http-gw-release-todo.md) lands.
The systemd unit is gated on `ConditionPathExists=/usr/local/bin/hc-http-gw`,
so this is a non-fatal "service disabled" state, not a boot failure.

### Monitoring Your Node

Host metrics (CPU, memory, disk, network) and Holochain metrics are shipped automatically to InfluxDB via Telegraf and the Holochain conductor respectively. Check service status with:

```bash
systemctl status telegraf
systemctl status holochain
systemctl status cloudflared
systemctl status hc-http-gw
journalctl -u holochain -f
journalctl -u lair-keystore -f
journalctl -u cloudflared -f
```

### Backup Important Data

Ensure you back up:
- Your seed bundle files (if using Option 3)
- `/var/lib/holochain/data/` — conductor databases and state
- `/var/lib/holochain/lair/` — keystore data
- Your `.happ` files in `/var/lib/holochain/apps/`

## Troubleshooting

### Services Not Starting

```bash
systemctl restart lair-keystore
systemctl restart holochain

journalctl -u holochain --no-pager -l
journalctl -u lair-keystore --no-pager -l
```

### Connection Issues

```bash
# Test admin interface
hc sandbox call --running 8800 list-apps

# Check if ports are open
ss -tlnp | grep 8800
```

### Registration Issues

```bash
# Check registration service logs
journalctl -u holochain-register --no-pager -l

# Re-run registration manually if needed
systemctl start holochain-register
```

### Tunnel + Gateway Issues

```bash
# Gateway is up but returns 403 for every zome call:
#   You haven't run hc-http-gw-configure yet, or the --app-id passed
#   doesn't match what's actually installed.
hc sandbox call --running 8800 list-apps   # see the real ids
cat /etc/hc-http-gw/env                    # see what gateway thinks

# Tunnel hostname returns 502/523 from Cloudflare:
#   cloudflared can't reach the local gateway, or the gateway is down.
systemctl status cloudflared hc-http-gw
journalctl -u cloudflared --no-pager -l | tail -50
curl -i http://127.0.0.1:8090/health       # bypasses tunnel entirely

# Tunnel hostname returns 530 / 1033 from Cloudflare:
#   Cloudflare side: tunnel id not found / DNS not yet propagated.
#   Wait for DNS, or check Pulumi state has the right tunnel id.
```

## Related Documentation

- [Setup Progenitor](./setup-progenitor.md) — Setting up progenitor nodes specifically
- [Install Agents](./install-agents.md) — Additional agent installation examples
- [Cloudflare Tunnel Cutover](./tunnel-cutover.md) — Staged cutover from the legacy laptop-hosted tuunnel to the Pulumi-managed `unyt-gateway` tunnel
- [Upstream `hc-http-gw` Release TODO](./upstream-hc-http-gw-release-todo.md) — Spec for the upstream binary release PR

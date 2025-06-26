# Setting Up an Always-On Holochain Node

> **Notice:** This guide is designed for automated deployment and will eventually be fully scriptable. Currently, it provides manual steps for each component.

This guide walks you through setting up a production-ready, always-on Holochain node on DigitalOcean. The process is broken down into three main parts:

1. **Setting up a DigitalOcean droplet** with a specific version of Holochain
2. **Choosing how to generate your agent keys**
3. **Downloading and installing your .happ file** with the respective agent key

## Part 1: Setting up a DigitalOcean Droplet with Holochain

### Prerequisites

Before you begin, ensure you have:

- [Terraform](https://www.terraform.io/) (v1.0.0+)
- [DigitalOcean Account](https://www.digitalocean.com/)
- DigitalOcean API Token
- SSH key added to DigitalOcean

### For zo-el: Creating a Reusable Droplet Snapshot

As zo-el, you'll create a base snapshot that others can reuse:

#### 1. Install Prerequisites

```bash
# Install Terraform
wget -O- https://apt.releases.hashicorp.com/gpg | \
    gpg --dearmor | \
    sudo tee /usr/share/keyrings/hashicorp-archive-keyring.gpg

echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] \
    https://apt.releases.hashicorp.com $(lsb_release -cs) main" | \
    sudo tee /etc/apt/sources.list.d/hashicorp.list

sudo apt-get update && sudo apt-get install terraform

# Generate SSH key if you don't have one
ssh-keygen -t rsa -b 4096
```

#### 2. Set up Environment Variables

```bash
# Create your environment file
cp .env.example .env
# If .env.example doesn't exist, create .env with the template below

# Edit with your values
nano .env

# Load environment variables
source .env
```

Your `.env` file should contain:
```bash
DO_TOKEN=your_digitalocean_api_token
SSH_KEY_ID=your_ssh_key_id_or_fingerprint
NODE_NAME=holochain-base-node
HOLOCHAIN_VERSION=0.5.2
LAIR_VERSION=0.6.1
DROPLET_SIZE=s-4vcpu-8gb
REGION=nyc3
LAIR_PASSWORD=secure-password
HOLOCHAIN_PASSWORD=secure-password
```

#### 3. Get Your SSH Key ID or Fingerprint

Choose one of these options to get your SSH key identifier:

**Option 1: Using Web Interface**
1. Go to Settings → Security → SSH Keys
2. You can use either:
   - The SSH key ID from the URL: `https://cloud.digitalocean.com/account/security?i=XXXXX`
   - The fingerprint shown directly in the SSH key list

**Option 2: Using doctl CLI**
```bash
# Install doctl
sudo snap install doctl
# or
brew install doctl  # for MacOS

# Authenticate with your API token
doctl auth init

# List SSH keys with their IDs and fingerprints
doctl compute ssh-key list
```

**Option 3: Get fingerprint locally**
```bash
# For RSA keys
ssh-keygen -E md5 -lf ~/.ssh/id_rsa.pub | awk '{print $2}' | cut -d':' -f2-

# For ED25519 keys
ssh-keygen -E md5 -lf ~/.ssh/id_ed25519.pub | awk '{print $2}' | cut -d':' -f2-
```

#### 4. Deploy the Base Node

```bash
# Initialize Terraform
cd terraform
terraform init

# Deploy the node
make deploy
```

#### 5. Verify Deployment

```bash
# SSH into your node
ssh root@$(terraform output -raw droplet_ip)

# Check services
systemctl status holochain
systemctl status lair-keystore

# View logs
journalctl -u holochain
journalctl -u lair-keystore
```

#### 6. Create a Snapshot

Once the deployment is complete and tested:

1. In the DigitalOcean dashboard:
   - Go to Droplets → Your Node → Snapshots
   - Create a snapshot with name: `holochain-v${HOLOCHAIN_VERSION}-base`
   - Make note of the snapshot ID for sharing with others

2. Document the snapshot details:
   - Snapshot ID
   - Holochain version
   - Lair version
   - Any specific configuration notes

### For Everyone Else: Using the Reusable Snapshot

Once zo-el has created the snapshot, anyone can deploy a node using:

#### Option A: Quick Deployment from Snapshot

```bash
# Clone the HEART repository
git clone https://github.com/your-org/heart.git
cd heart

# Set up your environment
cp .env.example .env
nano .env  # Add your DO_TOKEN and SSH_KEY_ID

# TODO: Modify terraform/main.tf to use the snapshot ID
# (This will be automated in future versions)

# Deploy using the snapshot
make deploy
```

#### Option B: Full Deployment from Scratch

```bash
# Follow the same steps as zo-el above
make deploy
```

## Part 2: Choose How to Generate Your Agent Keys

You have three options for key generation:

### Option 1: Automatic Key Generation (Simplest)

Let Holochain create the keys automatically when you install an app:

```bash
# SSH into your node
ssh root@YOUR_DROPLET_IP

# Create app directory
mkdir -p /var/lib/holochain/apps/

# Install app (this will create keys automatically)
hc sandbox call --running 8800 install-app /var/lib/holochain/apps/your-app.happ
```

### Option 2: Generate Keys within Lair

Generate keys directly in the Lair keystore on your node:

```bash
# SSH into your node
ssh root@YOUR_DROPLET_IP

# Create a new agent key
hc sandbox call --running 8800 new-agent
```

Output example:
```bash
root@heart-node:~# hc sandbox call --running 8800 new-agent
hc-sandbox: Added agent uhCAkN5IokFxdryZWUzR6Nb89wjVsiENaXp8uGsKbGJpT1SKxPzEm
```

Save this agent key - you'll need it for app installation.

### Option 3: Import from hc_seed_bundle_cli (Most Control)

For maximum control over your keys, use the `hc_seed_bundle_cli` available on [crates.io](https://crates.io/crates/hc_seed_bundle_cli):

#### 3a. Install hc_seed_bundle_cli locally

```bash
# Install from crates.io
cargo install hc_seed_bundle_cli
```

#### 3b. Generate a seed bundle

```bash
# Generate a new seed bundle
hc_seed_bundle_cli generate -o my-agent-seed.yaml

# Or import from existing seed phrase
hc_seed_bundle_cli generate --from-mnemonic "your twelve word mnemonic phrase here" -o my-agent-seed.yaml
```

#### 3c. Import to Lair on your droplet

```bash
# Copy the seed bundle to your droplet
scp my-agent-seed.yaml root@YOUR_DROPLET_IP:/tmp/

# SSH into your droplet
ssh root@YOUR_DROPLET_IP

# Import the seed bundle into Lair
hc_seed_bundle_cli import-lair /tmp/my-agent-seed.yaml

# Get the agent key for use in app installation
hc_seed_bundle_cli agent-key /tmp/my-agent-seed.yaml
```

## Part 3: Download and Install Your .happ File

### 1. Prepare Your App

```bash
# SSH into your node
ssh root@YOUR_DROPLET_IP

# Create apps directory
mkdir -p /var/lib/holochain/apps/

# Download your .happ file (replace with your actual URL)
curl -L -o /var/lib/holochain/apps/your-app.happ https://your-app-url.com/your-app.happ
```

### 2. Install the App

Choose the installation method based on your key generation choice:

#### If you used Option 1 (Automatic Keys):

```bash
# Install app with automatic key generation
hc sandbox call --running 8800 install-app /var/lib/holochain/apps/your-app.happ
```

#### If you used Option 2 or 3 (Pre-existing Keys):

```bash
# Install app with specific agent key
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

### 4. Interact with Your App

You can now interact with your app using:

- **piecework_cli** (pre-installed): `piecework_cli --help`
- **Direct hc calls**: `hc sandbox call --running 8800 [command]`
- **Custom client applications** connecting to the WebSocket interface on port 8800

## Post-Installation

### Monitoring Your Node

```bash
# Check service status
systemctl status holochain
systemctl status lair-keystore

# View logs in real-time
journalctl -u holochain -f
journalctl -u lair-keystore -f
```

### Backup Important Data

Ensure you backup:
- Your seed bundle files (if using Option 3)
- `/var/lib/holochain/data/` directory
- Your `.happ` files in `/var/lib/holochain/apps/`
- Your environment configuration (`.env` file)

### Network Configuration

Your node is configured to connect to:
- **Bootstrap Server**: `https://dev-test-bootstrap2.holochain.org/`
- **Signal Server**: `wss://dev-test-bootstrap2.holochain.org/`
- **Admin Interface**: `ws://localhost:8800` (WebSocket)

## Troubleshooting

### Services Not Starting

```bash
# Restart services
systemctl restart lair-keystore
systemctl restart holochain

# Check detailed logs
journalctl -u holochain --no-pager -l
journalctl -u lair-keystore --no-pager -l
```

### Connection Issues

```bash
# Test admin interface
hc sandbox call --running 8800 list-apps

# Check if ports are open
netstat -tlnp | grep 8800
```

### Key Import Issues

```bash
# Check lair status
lair-keystore --lair-root /var/lib/holochain/data/lair list-secrets

# Verify lair connection
cat /var/lib/holochain/config/conductor-config.yaml | grep connection_url
```

### Cleanup (when needed)

```bash
# If you need to destroy the droplet
terraform destroy \
  -var="do_token=${DO_TOKEN}" \
  -var="ssh_key_id=${SSH_KEY_ID}"
```

## What's Installed

Your always-on node includes:

- **Ubuntu 22.04** base system
- **Holochain Conductor** (version specified in .env)
- **Lair Keystore** (version specified in .env)
- **hc CLI tool** for admin operations
- **piecework_cli** for app interactions
- **Rust toolchain** for development
- **Systemd services** for automatic startup and monitoring

The setup script (`scripts/setup.sh`) handles:
- Dependency installation
- Binary downloads and permissions
- Service configuration
- Directory structure creation
- Automatic startup configuration

## Future Automation

This process is being automated for one-command deployment. The roadmap includes:

- [ ] Single script deployment
- [ ] Snapshot-based rapid deployment  
- [ ] Agent key management automation
- [ ] App version management
- [ ] Monitoring and alerting setup
- [ ] Automated backup procedures

## Related Documentation

- [Setup A DO that is running a Holochain conductor](./setup-do-holochain-server.md) - Technical details for the server setup
- [Setup Progenitor](./setup-progenitor.md) - Setting up progenitor nodes specifically
- [Install Agents](./install-agents.md) - Additional agent installation examples

---

> **Next Steps**: Once your node is running, you can deploy additional apps, set up monitoring, or configure backup procedures. For specific use cases like setting up Progenitor nodes, see the specialized documentation in this folder. 
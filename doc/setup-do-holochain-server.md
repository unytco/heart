# Setup Server

> **Note:** For a complete step-by-step guide including agent setup and app installation, see [Setup an Always-On Node](./setup-always-on-node.md). This document provides technical details for the server deployment only.

## Deployment

### Prerequisites

- [Terraform](https://www.terraform.io/) (v1.0.0+)
- [DigitalOcean Account](https://www.digitalocean.com/)
- DigitalOcean API Token
- SSH key added to DigitalOcean

Here are the deployment steps:

#### Prerequisites Setup

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

#### Environment Setup

```bash
  # Copy example env file
  cp .env.example .env

  # Edit with your values
  nano .env

  # Load environment variables
  source .env
```

#### DigitalOcean Setup

- Create a DigitalOcean account
- Generate an API token in the DigitalOcean dashboard
- Add your SSH key to DigitalOcean and get either the SSH key ID or fingerprint:

  Option 1: Using Web Interface

```bash
  1. Go to Settings -> Security -> SSH Keys
  2. You can use either:
     - The SSH key ID from the URL: https://cloud.digitalocean.com/account/security?i=XXXXX
     - The fingerprint shown directly in the SSH key list
```

Option 2: Using doctl CLI

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

Option 3: Get fingerprint locally

```bash
# For RSA keys
ssh-keygen -E md5 -lf ~/.ssh/id_rsa.pub | awk '{print $2}' | cut -d':' -f2-

# For ED25519 keys
ssh-keygen -E md5 -lf ~/.ssh/id_ed25519.pub | awk '{print $2}' | cut -d':' -f2-
```

#### Deploy

```bash
  # Initialize Terraform
  cd terraform
  terraform init

  make deploy
```

#### Redeploy

```bash
  make redeploy
```

#### Post-Deployment

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

#### Cleanup (when needed)

```bash
  terraform destroy \
    -var="do_token=${DO_TOKEN}" \
    -var="ssh_key_id=${SSH_KEY_ID}"
```

# HEART

**H**olochain **E**nvironment & **A**gent **R**untime **T**oolkit

A toolkit for quickly setting up and managing Holochain nodes. HEART provides automated setup, configuration, and testing for Holochain environments.

## Features

- Automated Holochain & Lair-keystore setup
- Systemd service management
- Infrastructure as Code with Terraform
- Testing framework
- Development tools

## Quick Start

```bash
make dev-init    # Initialize development environment
make dev-test   # Run tests
make cleanup    # Clean up resources
```

For more details, see [CONTRIBUTING.md](CONTRIBUTING.md)

## Overview

`hi` provides:

- Automated NixOS setup with Holonix environment
- Version-specific Holochain installations
- Standardized node configuration
- Local testing with Vagrant
- Cloud deployment with Terraform

## Deployment

### Prerequisites

- [Terraform](https://www.terraform.io/) (v1.0.0+)
- [DigitalOcean Account](https://www.digitalocean.com/)
- DigitalOcean API Token
- SSH key added to DigitalOcean

### Quick Start

1. Set up credentials:

   ```bash
   export DO_TOKEN="your_digitalocean_token"
   export SSH_KEY_ID="your_ssh_key_id"
   ```

2. Deploy a node:

   ```bash
   cd terraform
   terraform init
   terraform apply \
     -var="do_token=${DO_TOKEN}" \
     -var="ssh_key_id=${SSH_KEY_ID}" \
     -var="holochain_version=0.4"
   ```

### Configuration

Customize your deployment:

```bash
terraform apply \
  -var="do_token=${DO_TOKEN}" \
  -var="ssh_key_id=${SSH_KEY_ID}" \
  -var="node_name=custom-name" \
  -var="holochain_version=0.4" \
  -var="droplet_size=s-4vcpu-8gb" \
  -var="region=sfo3"
```

## Environment

Each node provides:

- NixOS with flakes enabled
- Holochain (version-specific)
- lair-keystore
- Development tools (jq, curl, git)

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and testing instructions.

## Roadmap

- [x] Basic NixOS setup with Holonix
- [x] Version-specific Holochain installations
- [x] Automated testing environment
- [ ] Piecework app installation
- [ ] Agent key management
- [ ] App version management
- [ ] Monitoring setup
- [ ] Backup procedures

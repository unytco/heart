# Setup Server

> **Note:** For a complete step-by-step guide including agent setup and app installation, see [Setup an Always-On Node](./setup-always-on-node.md).

Nodes are deployed to DigitalOcean using Pulumi. See the [README](../README.md#pulumi-setup) for configuration options and the [Always-On Node guide](./setup-always-on-node.md) for deployment steps.

## What Gets Provisioned

Each droplet is an Ubuntu 22.04 server provisioned via cloud-init (`cloudinit/default/cloud-config.yaml`). Cloud-init installs and configures:

- Holochain conductor and Lair Keystore (specific versions baked into the cloud-config)
- `hc` CLI tool
- `holo-keyutil` for key operations during registration
- Telegraf for host metrics (CPU, memory, disk, network → InfluxDB)
- Systemd services: `lair-keystore`, `holochain`, `holochain-register`, `telegraf`

## Updating the Cloud-Config

The cloud-config is a Go template at `cloudinit/default/cloud-config.yaml`. It is rendered by Pulumi at deploy time with secrets (e.g. InfluxDB token) injected from Pulumi config.

To update the Holochain or Lair version, or change any provisioning behaviour, edit that file and run `pulumi up`.

# Setup Server

> **Note:** For a complete step-by-step guide including agent setup and app installation, see [Setup an Always-On Node](./setup-always-on-node.md).

Nodes are deployed to DigitalOcean using Pulumi. See the [README](../README.md#pulumi-setup) for configuration options and the [Always-On Node guide](./setup-always-on-node.md) for deployment steps.

## What Gets Provisioned

Each droplet is an Ubuntu 24.04 server provisioned via cloud-init (`cloudinit/cloud-config.yaml`). Cloud-init installs and configures:

- Holochain conductor, Lair Keystore, and the `hc` CLI — all downloaded from the Holochain release pinned by `heart:holochain-version`
- `holo-keyutil` for key operations during registration — pinned separately by `heart:holo-keyutil-version`
- Telegraf for host metrics (CPU, memory, disk, network → InfluxDB)
- Systemd services: `lair-keystore`, `holochain`, `holochain-register`, `telegraf`

## Updating the Cloud-Config

The cloud-config is a Go template at `cloudinit/cloud-config.yaml`. It is rendered by Pulumi at deploy time with per-release values (Holochain/keyutil versions, network endpoints, InfluxDB url/org/bucket/token) injected from Pulumi config.

To bump the Holochain or Lair version for a release, set `heart:holochain-version` (and friends) on that stack and run `pulumi up`. To change provisioning behaviour for all releases, edit the template directly.

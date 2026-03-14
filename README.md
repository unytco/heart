# HEART

> **Notice:** This project is a work in progress and not all features are available yet.
> Please test out the setup if you use this setup

**H**olochain **E**nvironment & **A**gent **R**untime **T**oolkit

A toolkit for quickly setting up and managing Holochain nodes. HEART provides automated setup, configuration, and testing for Holochain environments.

## Overview

HEART is a toolkit for quickly setting up and managing Holochain nodes. It provides automated setup, configuration, and testing for Holochain environments.

## Features

- A Terraform module for deploying Holochain nodes to DigitalOcean
  - This node is a ubuntu 22.04 server running Holonix
  - Pre-configured to run a specified version of Holochain and Lair Keystore
  - use can use a hc to install the apps you want to run

## Documentation

- **[Setup an Always-On Node](./doc/setup-always-on-node.md)** - Complete guide for setting up a production-ready Holochain node
- [Setup A DO that is running a Holochain conductor](./doc/setup-do-holochain-server.md) - Technical details for server setup
- [Setup Progenitor](./doc/setup-progenitor.md) - Setting up progenitor nodes specifically
- [Install Agents](./doc/install-agents.md) - Additional agent installation examples

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and testing instructions.

## Roadmap

- [x] Basic Ubuntu setup with Holochain
- [x] Version-specific Holochain installations
- [x] Automated testing environment
- [x] Comprehensive setup documentation
- [x] Agent key management documentation
- [ ] Piecework app installation automation
- [ ] App version management
- [ ] Monitoring setup
- [ ] Backup procedures
- [ ] Snapshot-based rapid deployment

## Pulumi setup

Configure the digital ocean token using:

```shell
pulumi config set --secret digitalocean:token
```

Set the InfluxDB token using:

```shell
pulumi config set --secret influx-token
```

Configure the project to use on Digital Ocean:

```shell
pulumi config set project-name Holo
```

Configure the number of nodes, of each type:

```shell
pulumi config set heart:heart-always-online-count 2                                                                                                                                                                                               
pulumi config set heart:blockchain-bridging-count 1
pulumi config set heart:unyt-bridging-count 1
```

## Node layout

This section describes where things live on a provisioned droplet. Use it as a reference
when connecting to a node to install or manage apps.

### Binaries

All binaries are on `PATH` at `/usr/local/bin/`:

| Binary | Purpose |
|---|---|
| `holochain` | Holochain conductor |
| `lair-keystore` | Lair keystore |
| `hc` | Holochain CLI — use this to install apps and manage the conductor |
| `holo-keyutil` | Key utilities (`sign`, `extract-pubkey`) used during registration |

### Configuration

| Path | Purpose |
|---|---|
| `/etc/holochain/conductor-config.yaml` | Conductor configuration |

### Data and key files

Everything lives under `/var/lib/holochain/`:

| Path | Purpose |
|---|---|
| `data/` | Conductor databases and state |
| `lair/` | Lair keystore data |
| `lair-passphrase` | Passphrase used to unlock the lair keystore (mode 600). Needed if you ever have to inspect the keystore directly. |
| `agent-pub-key` | The node's agent public key as base64url. **This is the key you need when installing an app** — pass it as `--agent-key` to `hc sandbox call`. |
| `.registered` | Flag file written on successful registration. Its presence prevents re-registration across reboots. |

### Services

| Service | Purpose |
|---|---|
| `telegraf.service` | Collects host metrics (CPU, memory, disk, network) and ships to InfluxDB |
| `lair-keystore.service` | Lair keystore daemon |
| `holochain.service` | Holochain conductor daemon (also ships Holochain metrics directly to InfluxDB) |
| `holochain-register.service` | One-shot registration service — runs once after first boot, polls until an admin approves the node on the auth server |

### Installing an app

Once the node is registered (check `systemctl status holochain-register.service`):

```shell
AGENT_KEY=$(cat /var/lib/holochain/agent-pub-key)
hc sandbox call --running 8000 install-app \
    --app-id "your-app-id" \
    --agent-key "${AGENT_KEY}" \
    /path/to/your-app.happ
```

## Cloud-init binaries

The cloud-config for droplets embeds a pre-built `holo-keyutil` binary as base64.
It provides two subcommands used during node registration:

- `holo-keyutil sign` — signs data via lair IPC
- `holo-keyutil extract-pubkey` — parses a Holochain `AgentPubKey` and extracts the raw ed25519 bytes

The binary is built and published automatically by the `release-holo-keyutil` GitHub
Actions workflow when a tag is pushed. Droplets download it directly from the release
at first boot — nothing needs to be embedded in the cloud-config.

To cut a release and update the cloud-config to point at it:

```shell
git tag v0.1.0 && git push origin v0.1.0
# wait for the Actions workflow to complete, then:
./scripts/package-cloudinit-binaries.sh v0.1.0
```

Commit the resulting `cloudinit/default/cloud-config.yaml` alongside the tag.
Requires `sed`.

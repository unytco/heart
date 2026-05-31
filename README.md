# HEART

> **Notice:** This project is a work in progress and not all features are available yet.
> Please test out the setup if you use this setup

**H**olochain **E**nvironment & **A**gent **R**untime **T**oolkit

A toolkit for quickly setting up and managing Holochain nodes. HEART provides automated setup, configuration, and testing for Holochain environments.

## Overview

HEART is a toolkit for quickly setting up and managing Holochain nodes. It provides automated setup, configuration, and testing for Holochain environments.

## Features

- A Pulumi program for deploying Holochain nodes to DigitalOcean
  - Nodes are Ubuntu 22.04 servers provisioned via cloud-init
  - Pre-configured to run a specified version of Holochain and Lair Keystore
  - Use `hc` to install the apps you want to run

## Documentation

- **[Deploying a New Release Fleet](./doc/deploy-new-release.md)** - Stand up a dedicated set of servers for a new unyt version (one Pulumi stack per release)
- **[Setup an Always-On Node](./doc/setup-always-on-node.md)** - Complete guide for setting up a production-ready Holochain node
- [Setup A DO that is running a Holochain conductor](./doc/setup-do-holochain-server.md) - Technical details for server setup
- [Setup Progenitor](./doc/setup-progenitor.md) - Setting up progenitor nodes specifically
- [Install Agents](./doc/install-agents.md) - Additional agent installation examples

## Development

Work inside the dev shell so `pulumi` and `go` are on `PATH`:

```shell
nix develop
make help      # list build + deploy targets
make build     # compile the Pulumi program
make vet       # go vet
```

See [Deploying a New Release Fleet](./doc/deploy-new-release.md) for the deploy workflow.

## Roadmap

- [x] Basic Ubuntu setup with Holochain
- [x] Version-specific Holochain installations
- [x] Automated testing environment
- [x] Comprehensive setup documentation
- [x] Agent key management documentation
- [x] Monitoring setup (Telegraf host metrics + Holochain metrics → InfluxDB)
- [ ] Piecework app installation automation
- [ ] App version management
- [ ] Backup procedures
- [ ] Snapshot-based rapid deployment

## Pulumi setup

Configure the digital ocean token using:

```shell
pulumi config set --secret digitalocean:token
```

Set the InfluxDB token using:

```shell
pulumi config set --secret heart:influx-token
```

Configure the project to use on Digital Ocean:

```shell
pulumi config set heart:project-name Holo
```

Each release fleet runs in its own stack, identified by `heart:release` (this
namespaces every droplet name and adds a `release:<x>` tag):

```shell
pulumi config set heart:release v0-7-0
```

Configure the number of nodes, of each type:

```shell
pulumi config set heart:always-online-count 4
pulumi config set heart:blockchain-bridging-count 1
pulumi config set heart:unyt-bridging-count 1
```

All other per-release values (Holochain version, network endpoints, droplet
sizes) are optional config keys whose defaults live in
[`defaults.yaml`](./defaults.yaml) — edit that file to change a default for all
releases. One exception: `heart:influx-bucket` defaults to the shared `unyt`
bucket, so set it per release (e.g. `unyt-v0-7-0`, created in InfluxDB first) to
keep each fleet's metrics isolated. See
[`Pulumi.release.yaml.example`](./Pulumi.release.yaml.example) for the full set
of keys.

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

### Services

| Service | Purpose |
|---|---|
| `telegraf.service` | Collects host metrics (CPU, memory, disk, network) and ships to InfluxDB |
| `lair-keystore.service` | Lair keystore daemon |
| `holochain.service` | Holochain conductor daemon (also ships Holochain metrics directly to InfluxDB) |
| `holochain-register.service` | Registration service — runs on every boot to register the node and refresh auth credentials. On first boot it polls until an admin approves the key; on subsequent boots it refreshes credentials directly. |

### Installing an app

Once the node is registered (check `systemctl status holochain-register.service`):

```shell
AGENT_KEY=$(cat /var/lib/holochain/agent-pub-key)
hc sandbox call --running 8800 install-app \
    --app-id "your-app-id" \
    --agent-key "${AGENT_KEY}" \
    /path/to/your-app.happ
```

## holo-keyutil

`holo-keyutil` provides two subcommands used during node registration:

- `holo-keyutil sign` — signs data via lair IPC
- `holo-keyutil extract-pubkey` — parses a Holochain `AgentPubKey` and extracts the raw ed25519 bytes

It is built and published by the `release-holo-keyutil` GitHub Actions workflow when a
tag is pushed. Droplets download the binary directly from that release at first boot;
the version is controlled by the `heart:holo-keyutil-version` config key (default `v0.1.0`).

To use a new build, cut a release and point a stack at it:

```shell
git tag v0.2.0 && git push origin v0.2.0   # wait for the Actions workflow to finish
pulumi config set heart:holo-keyutil-version v0.2.0
```

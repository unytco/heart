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
pulumi config set project-name Holo
```

Configure the number of nodes, of each type:

```shell
pulumi config set heart:heart-always-online-count 4                                                                                                                                                                                               
pulumi config set heart:blockchain-bridging-count 1
pulumi config set heart:unyt-bridging-count 1
pulumi config set heart:heart-always-online-alt-count 4                                                                                                                                                                                               
pulumi config set heart:blockchain-bridging-alt-count 1
pulumi config set heart:unyt-bridging-alt-count 1
```

Configure the Cloudflare tunnel that fronts the explorer's
`hc-http-gw` on each droplet:

```shell
pulumi config set --secret cloudflare:apiToken <token>
pulumi config set heart:cf-account-id <account_id>
pulumi config set heart:cf-zone-name <zone>
pulumi config set heart:gw-hostname <host>
pulumi config set --secret heart:cloudflare-tunnel-secret \
    "$(openssl rand -base64 32)"
```

The tunnel + ingress + DNS are then Pulumi-managed; every
`heart-always-online` droplet runs a `cloudflared` replica with the
same tunnel id and Cloudflare load-balances across healthy replicas.
See [Cloudflare Tunnel Cutover](./doc/tunnel-cutover.md) for the staged
migration from the laptop-hosted legacy tunnel to the Pulumi
fresh-provisioned `unyt-tunnel` tunnel — including how to roll out
droplet connectors before flipping the proxy worker's upstream URL,
and the legacy-tunnel retirement steps once the new path is verified.

Optionally, restrict SSH inbound to a Pulumi-managed allowlist (the
fleet-wide firewall is **skipped entirely** unless this is set, so
nothing locks operators out by accident):

```shell
pulumi config set heart:operator-cidrs "203.0.113.7/32,198.51.100.0/24"
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
| `hc-http-gw` | Holochain HTTP gateway, bound to `127.0.0.1:8090`. May be absent until the [upstream binary release](./doc/upstream-hc-http-gw-release-todo.md) lands; the systemd unit is `ConditionPathExists=`-gated so this is non-fatal. |
| `hc-http-gw-configure` | Helper that writes `/etc/hc-http-gw/env` and restarts `hc-http-gw.service`. Run after installing an `.happ`. |
| `cloudflared` (from apt) | Cloudflare tunnel connector. Authenticates against the shared tunnel id with a token in `/etc/cloudflared/token`. |

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
| `cloudflared.service` | Cloudflare tunnel connector for `unyt-tunnel.unyt.co`. Started automatically; runs as a `DynamicUser` with the token loaded via `systemd-creds`. |
| `hc-http-gw.service` | Holochain HTTP gateway, listens on `127.0.0.1:8090`. **Not started at boot** — `/etc/hc-http-gw/env` ships with no app ids allowlisted; the operator runs `hc-http-gw-configure --app-id <id>` after installing the `.happ`, which writes the env file and starts the service. |

### Installing an app

Once the node is registered (check `systemctl status holochain-register.service`):

```shell
AGENT_KEY=$(cat /var/lib/holochain/agent-pub-key)
hc sandbox call --running 8800 install-app \
    --app-id "your-app-id" \
    --agent-key "${AGENT_KEY}" \
    /path/to/your-app.happ
```

Then point `hc-http-gw` at the installed app and start the service.
`hc-http-gw-configure` is idempotent — re-running it with a new
`--app-id` rewrites `/etc/hc-http-gw/env` and restarts the gateway:

```shell
hc-http-gw-configure --app-id your-app-id --allowed-fns '*'
systemctl status hc-http-gw cloudflared

# Local smoke test
curl -i http://127.0.0.1:8090/health

# End-to-end smoke test (from anywhere; routes worker -> tunnel -> droplet)
source /etc/heart-fleet/metadata
curl -i "https://${HEART_GATEWAY_HOSTNAME}/health"
```

The `--allowed-fns '*'` form opens every zome function on the app; per
the [`hc-http-gw` spec](https://github.com/holochain/hc-http-gw/blob/main/spec.md),
the recommended posture is an explicit comma-separated allowlist (e.g.
`main/list_things,main/get_thing`). The helper takes either.

**Note**: if `hc-http-gw` is missing on disk (because the
[upstream binary release](./doc/upstream-hc-http-gw-release-todo.md)
hasn't landed yet), `hc-http-gw-configure` will write the env file but
warn that the service was not started. Once the upstream binary is
shipped, an `apt-get install` + `systemctl start hc-http-gw` (or
re-running the cloud-init download step inline) brings the gateway up
on existing droplets.

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

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

Set the public hostname that fronts the explorer's `hc-http-gw` on
each droplet. This is the hostname `cloudflared` will route into the
loopback gateway; it's surfaced to operator scripts via
`/etc/heart-fleet/metadata` on every droplet:

```shell
pulumi config set heart:gw-hostname unyt-tunnel.unyt.co
```

The Cloudflare tunnel itself — origin cert, per-tunnel credentials,
ingress config, DNS — is **not** managed by this Pulumi program. It's
operated using Cloudflare's standard locally-managed model:
`/etc/cloudflared/cert.pem` (CF account origin cert) +
`/etc/cloudflared/<tunnel-id>.json` (the per-tunnel credentials) +
`/etc/cloudflared/config.yml` (ingress rules) are streamed onto every
new droplet by [`unytco/automation`](https://github.com/unytco/automation)'s
`setup-tunnel.sh` (Makefile target `heart-always-online-N-tunnel`)
from secrets stored on _this_ Pulumi stack (`heart:cf-cert-pem`,
`heart:unyt-tunnel-credentials-json`). Operators materialize those
onto their laptops via `make pull-secrets` in `automation/`. See
[`automation/docs/hash-explorer-backend.md`](https://github.com/unytco/automation/blob/main/docs/hash-explorer-backend.md)
§ Architecture for the full picture and § Secrets for the rotation
flow.

`cloudflared.service` on the droplet is enabled at first boot but
held inactive by `ConditionPathExists=` until those files exist —
`setup-tunnel.sh` is what eventually starts it.

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
| `hc-http-gw` | Holochain HTTP gateway, bound to `127.0.0.1:8090`. Installed post-boot by the operator via [`unytco/automation`](https://github.com/unytco/automation)'s `setup-gateway.sh` (Makefile target `heart-always-online-N-gateway`); absent until that runs. Currently built from source on the droplet (`cargo build --release` against `holochain/hc-http-gw` at `.gateway.version`) — upstream ships no binary assets yet. The systemd unit's `ConditionPathExists=` gates on all three required files (binary + `hc-http-gw-launcher` + `/etc/hc-http-gw/env`), so a missing binary is non-fatal at boot — see [doc/upstream-hc-http-gw-release-todo.md](./doc/upstream-hc-http-gw-release-todo.md) for the upstream-binary-release plan. |
| `hc-http-gw-launcher` | Bash wrapper installed by cloud-init at `/usr/local/bin/hc-http-gw-launcher`. Reads `/etc/hc-http-gw/env` permissively and execs `hc-http-gw` via `/usr/bin/env` — required because `hc-http-gw` accepts env-var names containing hyphens / dots that systemd's `EnvironmentFile=` parser silently drops. The launcher body is byte-identical with `automation/scripts/setup-gateway.sh`'s runtime install path; verify with `bash automation/scripts/check-launcher-drift.sh`. |
| `hc-http-gw-configure` | Helper that writes `/etc/hc-http-gw/env` and restarts `hc-http-gw.service`. Run after installing an `.happ`. |
| `cloudflared` (from apt) | Cloudflare tunnel connector. Reads `/etc/cloudflared/cert.pem` + `/etc/cloudflared/<tunnel-id>.json` + `/etc/cloudflared/config.yml`, all streamed onto the droplet post-boot by `automation/scripts/setup-tunnel.sh` from Pulumi-canonical secrets. The `cloudflared.service` unit's `ConditionPathExists=` gates on these files, so the service is dormant on a fresh droplet until that script runs. |

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
| `cloudflared.service` | Cloudflare tunnel connector for the public gateway hostname (`HEART_GATEWAY_HOSTNAME` from `heart:gw-hostname`, also in `/etc/heart-fleet/metadata`). Runs as `root` (needs persistent read access to the per-tunnel credentials file at the path baked into `config.yml`). **Not started at boot** — `/etc/cloudflared/` is empty on a fresh droplet, and the unit's `ConditionPathExists=` gate keeps it inactive. Brought online by the post-boot run of `setup-tunnel.sh` once `cert.pem` + `<tunnel-id>.json` + `config.yml` exist. |
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

**Note**: if `hc-http-gw` is missing on disk (because the operator
hasn't yet run `make heart-always-online-N-gateway` from a checkout
of [`unytco/automation`](https://github.com/unytco/automation)),
`hc-http-gw-configure` will write the env file but warn that the
service was not started. Run the make target to build the binary from
upstream `holochain/hc-http-gw` source (cloned at `.gateway.version`
and built on the droplet via `cargo build --release` — slower than a
pre-built binary but currently the only working path since upstream
ships no binary assets). The gateway then starts automatically once
`hc-http-gw-configure` is re-run. See
[doc/upstream-hc-http-gw-release-todo.md](./doc/upstream-hc-http-gw-release-todo.md)
for the upstream-PR plan that will eventually let us pull a pre-built
binary instead of building locally.

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

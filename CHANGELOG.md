# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `domino_cli` crate.
- Pulumi-managed Cloudflare tunnel + remote ingress config + DNS via the new `cloudflare.go`, fronting the always-on `hc-http-gw` fleet behind `unyt-gateway.unyt.co`. Tunnel uses `ConfigSrc=cloudflare` so droplets hold only the connector token, never an ingress config. A fresh tunnel is provisioned by Pulumi rather than importing the legacy hand-created tunnel.
- Cloud-init now installs `cloudflared` from Cloudflare's apt repo and runs it as a `DynamicUser` systemd unit pointed at the shared tunnel id — every always-online droplet contributes to HA replicas automatically.
- Cloud-init downloads `hc-http-gw` (pinned via `HC_HTTP_GW_VERSION` env in `holochain-first-boot`) and installs an idempotent `/usr/local/bin/hc-http-gw-configure` helper for operators to bind app ids / fn allowlists post-install.(The systemd unit is `ConditionPathExists=`-gated so the rest of cloud-init still succeeds while we wait on the upstream binary release pipeline).
- Optional Pulumi `digitalocean.Firewall` (`heart-fleet-firewall`) attached by tag to every droplet flavour. Inbound: SSH from a `heart:operator-cidrs` allowlist + ICMP. No inbound 8090/8800/80/443 — `cloudflared` is outbound-only. Firewall is skipped entirely unless `heart:operator-cidrs` is set, so operators can't lock themselves out by accident.
- `/etc/heart-fleet/metadata` exposes the public gateway hostname on every droplet for operator scripts (the smoke-test helper sources it).
- `scripts/smoke-test.sh <hostname> [...]` exercises the full chain (CF worker → tunnel → gateway → conductor) from anywhere; suitable for manual ops or CI after `pulumi up`.
- `scripts/sync-cloud-configs.sh` enforces byte-identity between `cloudinit/default/` and `cloudinit/alt/`. Run it after any cloud-config edit; `--check` mode is CI-friendly.

### Changed

- bump cloud-init HOLOCHAIN_VERSION and holo-keyutil deps to Holochain 0.6.1.
- conductor admin `allowed_origins` tightened from `*` to `"hc-http-gw,hc-sandbox"`. Admin port stays loopback-only; this is defence-in-depth for the in-process clients we actually use.
- Cloud-init template now consumes `CloudflareToken` and `GatewayHostname` in addition to the existing `InfluxToken`. The rendered `UserData` is a `pulumi.StringOutput` rather than a plain string so the tunnel-derived secret can be inlined without leaking it into Pulumi state previews.

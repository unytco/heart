# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `domino_cli` crate.
- Pulumi-managed Cloudflare tunnel + remote ingress config + DNS via the new `cloudflare.go`, fronting the always-on `hc-http-gw` fleet behind `unyt-tunnel.unyt.co`. Tunnel uses `ConfigSrc=cloudflare` so droplets hold only the connector token, never an ingress config. A fresh tunnel is provisioned by Pulumi rather than importing the legacy hand-created tunnel.
- Cloud-init now installs `cloudflared` from Cloudflare's apt repo and runs it as a `DynamicUser` systemd unit pointed at the shared tunnel id — every always-online droplet contributes to HA replicas automatically.
- Cloud-init installs the `hc-http-gw` systemd unit, the `/etc/hc-http-gw/env` template, and the idempotent `/usr/local/bin/hc-http-gw-configure` helper. The binary itself is installed post-boot by the operator via [`unytco/automation`](https://github.com/unytco/automation)'s `setup-gateway.sh` (Makefile target `heart-always-online-N-gateway`), which builds from a workshop-managed source pin and `scp`s the result to `/usr/local/bin/hc-http-gw`. The systemd unit is `ConditionPathExists=`-gated so cloud-init succeeds while the binary is absent; the gateway comes online once the operator runs `setup-gateway` + `hc-http-gw-configure`.
- Optional Pulumi `digitalocean.Firewall` (`heart-fleet-firewall`) attached by tag to every droplet flavour. Inbound: SSH from a `heart:operator-cidrs` allowlist + ICMP. No inbound 8090/8800/80/443 — `cloudflared` is outbound-only. Firewall is skipped entirely unless `heart:operator-cidrs` is set, so operators can't lock themselves out by accident.
- `/etc/heart-fleet/metadata` exposes the public gateway hostname on every droplet for operator scripts (the smoke-test helper sources it).
- `scripts/smoke-test.sh <hostname> [...]` exercises the full chain (CF worker → tunnel → gateway → conductor) from anywhere; suitable for manual ops or CI after `pulumi up`.
- `scripts/sync-cloud-configs.sh` enforces byte-identity between `cloudinit/default/` and `cloudinit/alt/`. Run it after any cloud-config edit; `--check` mode is CI-friendly.

### Changed

- bump cloud-init HOLOCHAIN_VERSION and holo-keyutil deps to Holochain 0.6.1.
- conductor admin `allowed_origins` tightened from `*` to `"hc-http-gw,hc-sandbox"`. Admin port stays loopback-only; this is defence-in-depth for the in-process clients we actually use.
- Cloud-init template now consumes `CloudflareToken` and `GatewayHostname` in addition to the existing `InfluxToken`. The rendered `UserData` is a `pulumi.StringOutput` rather than a plain string so the tunnel-derived secret can be inlined without leaking it into Pulumi state previews.
- Abandoned a heart-side mirror of upstream `hc-http-gw` releases (previously drafted as `.github/workflows/release-hc-http-gw.yml`). Mirroring a binary we don't author into a private heart release was the wrong supply-chain shape: it would have signed bytes with the wrong key, required GitHub auth on every droplet, and pulled binary-distribution responsibility into a repo that doesn't own the source. The binary now flows operator → droplet via `automation/scripts/setup-gateway.sh`, which builds from a workshop-managed source pin (`workshop/hc-http-gw` submodule) and `scp`s the result — same shape as `lair-sign` and `pricing-oracle`. See [`doc/upstream-hc-http-gw-release-todo.md`](doc/upstream-hc-http-gw-release-todo.md) for the upstream-PR plan that closes the underlying gap.
- Cloud-init now installs `/usr/local/bin/hc-http-gw-launcher` and the `hc-http-gw.service` unit's `ExecStart=` points at the launcher instead of the binary directly; `EnvironmentFile=` is dropped. Required because `hc-http-gw` env var names can include hyphens / dots / version suffixes (e.g. `HC_GW_ALLOWED_FNS_unyt-sandbox-holo-hosting-0.90`) which systemd's `EnvironmentFile=` parser silently drops as "invalid assignment", causing `Missing HC_GW_ALLOWED_FNS_<id> env var` at startup. The launcher reads `/etc/hc-http-gw/env` permissively and execs the binary via `/usr/bin/env`, which uses `execve()` directly and accepts arbitrary key strings. The unit now `ConditionPathExists=`-gates on all three required files (binary, launcher, env). Confirmed live during the `heart-always-online-3` cutover; same fix mirrored into `automation/scripts/setup-gateway.sh`.
- `/etc/hc-http-gw/env` is now written with mode `0644` (was `0640`) in both the initial cloud-init `write_files` entry and the `hc-http-gw-configure` helper. The `DynamicUser` systemd creates for `hc-http-gw.service` isn't in the root group, so `0640 root:root` yields EACCES at startup (confirmed during the cutover — the launcher's `not readable` stderr was the smoking gun). The env file holds no secrets — just a loopback URL, app ids, and function allowlists, which are intentionally HTTPS-exposed posture anyway — so `0644` is correct.

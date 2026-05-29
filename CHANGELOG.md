# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Add `domino_cli` crate.
- Install `cloudflared` apt package and `cloudflared.service` unit via cloud-init (not started at boot).
- Gate `cloudflared.service` on `/etc/cloudflared/cert.pem` and `config.yml` written by `setup-tunnel.sh`.
- Install `hc-http-gw.service`, `/etc/hc-http-gw/env` template, launcher, and `hc-http-gw-configure` via cloud-init.
- Gate `hc-http-gw.service` on binary, launcher, and env file (binary installed post-boot by automation).
- Add optional Pulumi `digitalocean.Firewall` when `heart:operator-cidrs` is set (pilot tag only until `heart:firewall-tags` set).
- Allow outbound TCP/22 on fleet firewall for git-over-SSH during on-droplet `cargo build`.
- Expose `HEART_GATEWAY_HOSTNAME` in `/etc/heart-fleet/metadata` from `heart:gw-hostname` Pulumi config.
- Add `scripts/smoke-test.sh` for end-to-end CF worker → tunnel → gateway → conductor checks.
- Add `scripts/sync-cloud-configs.sh` to keep `cloudinit/default/` and `cloudinit/alt/` byte-identical.
- Add Pulumi secrets `heart:cf-cert-pem` and `heart:unyt-tunnel-credentials-json` for tunnel bootstrap.

### Changed

- Bump cloud-init Holochain and holo-keyutil deps to 0.6.1.
- Tighten conductor admin `allowed_origins` from `*` to `hc-http-gw,hc-sandbox`.
- Retire `cloudflare.go` and `pulumi-cloudflare`; use locally-managed tunnel + `heart:gw-hostname`.
- Simplify `renderCloudInit` to plain string return (no `pulumi.StringOutput` tunnel plumbing).
- Abandon heart-side `hc-http-gw` binary mirror; install via `automation` `setup-gateway.sh` instead.
- Point `hc-http-gw.service` `ExecStart` at launcher; drop `EnvironmentFile=` (hyphen-safe env keys).
- Write `/etc/hc-http-gw/env` mode `0644` so `DynamicUser` can read it at startup.

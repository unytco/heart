# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `make test` target and a Go CI workflow (build / vet / gofmt / test on push + PR), with actions pinned to commit SHAs and no persisted credentials.
- `lair` group on release fleets: the lair passphrase is now `root:lair` mode `640` (was root-only `600`) so co-located non-root services can read it to sign via lair.
- `doc/deploy-new-release.md` § Migration window — the version-retention policy for a migration release (3–4 versions coexist, no auto-decommission).
- `notary` node type to release fleets (one always-on droplet per migration notary).
- `domino_cli` crate.
- `hf-swapper` and `hash-explorer` node types to release fleets.
- Server types are defined in a `nodeTypes` registry in `main.go`; adding one is an entry there plus its `<name>-count` / `<name>-size` keys in `defaults.yaml`.

### Changed

- Provision **Holochain 0.7.0** (was 0.6.2-rc.0); drops the removed `signal_url`, uses the 0.7 `db_sync_level` default. **Rendered user-data changes.**
- The rendered conductor config is schema-checked in tests (unknown keys, enum discriminators, required keys).
- Those key allow-lists are re-derived from `holochain --config-schema` when a binary is on `PATH`/`HEART_HOLOCHAIN_BIN` (skipped in CI).
- The 0.7 bump lands on the next release's fresh stack, not a running fleet — live 0.6 fleets keep running from their 0.6-era commit. See `doc/deploy-new-release.md` § Migration window.
- `holo-keyutil` deploy pin moves to `v0.2.0` (0.7-built; `v0.1.0` is 0.6-era). **The `v0.2.0` release must exist before any `pulumi up`.**
- Bump `holo-keyutil` Rust deps to the 0.7 line (`lair_keystore_api = "=0.7.1"`, `holo_hash = "0.7.0"`); the `v0.2.0` release carries this build to the fleet.
- `make new-release` reads `DIGITALOCEAN_TOKEN`/`INFLUX_TOKEN` from a gitignored `heart/.env` and sets `digitalocean:token` + `heart:influx-token` + `heart:project-name` on the stack.
- The cloud-init base (systemd units + first-boot core) moves to `cloudinit/base/`, shared with the local-testnet fleet image; the InfluxDB metrics env moves to a prod-only `holochain.service.d/10-metrics.conf` drop-in. Rendered user-data changed.
- Default fleet shrinks to 4 droplets per release: `always-online-count` and `unyt-bridging-count` default to `0`, `notary-count` to `1` (single-notary).
- Deploy model is one Pulumi stack per unyt release, namespaced by `heart:release`; versions, endpoints, sizes and counts are per-release config keys with defaults in `defaults.yaml`.
- Makefile reworked around the Pulumi per-release workflow (`make new-release`, `preview`, `up`, `destroy`); dropped the dead Terraform/Vagrant targets.

### Removed

- The `heart:signal-url` config key (and `cloudInitData.SignalURL`) — 0.7 dropped `NetworkConfig::signal_url`. A stack that still sets it is harmless.
- The `alt` second-fleet hack: `createAlt` and the `*-alt-count` config keys. Use a separate release stack instead.
- Dead Terraform/Vagrant-era files: `CONTRIBUTING.md`, `services/`, `scripts/test.sh`, and the obsolete `Pulumi.heart.yaml` stack.

### Fixed

- `holo-keyutil extract-pubkey` errors on a wrong-type hash instead of panicking; added golden + malformed-input tests.
- Doc fixes: `install-app --agent-key` wants the `uhCAk…` `AgentPubKey`, not the raw `agent-pub-key` file; plus `enable-app`'s positional app-id and a bad install path in `doc/install-agents.md`.
- The conductor's `bootstrap_url` / `relay_url` are quoted in the cloud-init template, and `make preview` screens config values for quote-breaking characters and empty values. **Rendered user-data changes.**
- Fleet nodes booted bare — non-ASCII in `cloudinit/cloud-config.yaml` made cloud-init discard the whole config. The `cloudinit/` tree is printable-ASCII only and `renderCloudInit` refuses user-data cloud-init would reject. **Rendered user-data changes.**

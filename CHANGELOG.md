# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `make test` target and a Go CI workflow (build / vet / gofmt / test on push + PR), with actions pinned to commit SHAs and no persisted credentials.
- `lair` group on release fleets: the lair passphrase is now `root:lair` mode `640` (was root-only `600`) so co-located non-root services can read it to sign via lair.
- `doc/deploy-new-release.md` § Migration window: the version-retention policy for a migration release (3–4 versions coexist, no auto-decommission), pointing at version-migration `notary-host.md` § Lifecycle.
- `notary` node type to release fleets (one always-on droplet per migration notary).
- `domino_cli` crate.
- `hf-swapper` and `hash-explorer` node types to release fleets.
- Server types are defined in a `nodeTypes` registry in `main.go`; adding one is an entry there plus its `<name>-count` / `<name>-size` keys in `defaults.yaml`.

### Changed

- Release fleets now provision **Holochain 0.7.0** (was `holochain-0.6.2-rc.0`): the rendered cloud-init drops the removed `network.signal_url` (fatal under `deny_unknown_fields`), replaces `db_sync_strategy: Fast` with 0.7's `db_sync_level` default, and creates the first-boot agent key via `hc client call --port` (`call` moved to the new `hc_client` crate). **Rendered user-data changes.**
- The rendered conductor config is schema-checked in tests (unknown keys, both enum discriminators, required keys, and the values it shares with the register script), because every way it can be malformed fails silently on the droplet.
- Those key allow-lists are re-derived from `holochain --config-schema` whenever a binary is on `PATH`/`HEART_HOLOCHAIN_BIN` (skipped in CI), so they stay authoritative rather than hand-maintained.
- The 0.7 bump lands on the next release's fresh stack, not a running fleet; live 0.6 fleets keep running from their 0.6-era commit until the accounts on them migrate (agent keys are recorded in the automation repo's deploy results). See `doc/deploy-new-release.md` § Migration window.
- `holo-keyutil` deploy pin moves to `v0.2.0` (a 0.7-built binary; `v0.1.0` is 0.6-era). **The `v0.2.0` release must exist before any `pulumi up`** — first boot `curl -fsSL`s it under `set -eo pipefail`.
- Bump `holo-keyutil` Rust deps to the 0.7 line (`lair_keystore_api = "=0.7.1"`, `holo_hash = "0.7.0"`); the `v0.2.0` release carries this build to the fleet.
- `make new-release` reads `DIGITALOCEAN_TOKEN`/`INFLUX_TOKEN` from a gitignored `heart/.env` and sets `digitalocean:token` + `heart:influx-token` + `heart:project-name` on the stack.
- The cloud-init base (systemd units + first-boot core) is factored into `cloudinit/base/` as the single shared source, so the local-testnet fleet image consumes the same bytes and cannot drift from prod; the InfluxDB metrics env moved to a prod-only `holochain.service.d/10-metrics.conf` drop-in. Rendered user-data changed.
- Default fleet shrinks to 4 droplets per release: `always-online-count` and `unyt-bridging-count` default to `0`, `notary-count` to `1` (single-notary). Re-enabling a role is a count bump plus restoring its config from `automation/config/disabled/`.- Deploy model is one Pulumi stack per unyt release, namespaced by `heart:release`; versions, network endpoints, InfluxDB config and droplet sizes/counts are per-release config keys with defaults in `defaults.yaml`. See `Pulumi.release.yaml.example` and `doc/deploy-new-release.md`.
- Makefile reworked around the Pulumi per-release workflow (`make new-release`, `preview`, `up`, `destroy`); dropped the dead Terraform/Vagrant targets.

### Removed

- The `heart:signal-url` config key (and `cloudInitData.SignalURL`): 0.7 dropped `NetworkConfig::signal_url` and the value was already an unused placeholder. A stack that still sets it is harmless.
- The `alt` second-fleet hack: `createAlt` and the `*-alt-count` config keys. Use a separate release stack instead.
- Dead Terraform/Vagrant-era files: `CONTRIBUTING.md`, `services/`, `scripts/test.sh`, and the obsolete `Pulumi.heart.yaml` stack.

### Fixed

- `holo-keyutil extract-pubkey` now errors on a wrong-type hash instead of panicking (a `DnaHash` has the same 39-byte shape as an `AgentPubKey`), and gained golden + malformed-input tests; the crate had none.
- The README and always-on-node guide no longer tell you to pass the raw `agent-pub-key` file to `install-app --agent-key`: that file holds the raw ed25519 key, while `--agent-key` wants the `uhCAk…` `AgentPubKey`, so the command could not have worked. Also fixes `enable-app`'s positional app-id and a bad install path in `doc/install-agents.md`.
- The conductor's `bootstrap_url` and `relay_url` are now quoted in the cloud-init template, and config values are screened at `make preview` for characters that break the quoting (`"`, `\`, `$`, `%`, a backtick), leading/trailing whitespace, and empty values — an unquoted `:` or `#` in a URL previously made the conductor config unparseable or nulled it. **Rendered user-data changes.**
- Fleet nodes booted bare: non-ASCII (em-dashes) in the raw-injected `cloudinit/cloud-config.yaml` made cloud-init discard the whole config, so droplets came up with no services. The `cloudinit/` tree is now printable-ASCII only and `renderCloudInit` refuses to emit user-data cloud-init would reject (naming the offending byte), with config values screened and secrets masked. **Rendered user-data changes.**

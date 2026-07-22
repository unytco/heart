# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `lair` group on release fleets: the lair passphrase is written `root:lair` mode `640` (was root-only `600`) so co-located non-root services (pricing-oracle, bridge-orchestrator) can read it to sign zome calls via lair without running as root.
- `doc/deploy-new-release.md` § Migration window — the version-retention policy for a migration release (released versions stay up by deliberate choice so 3–4 coexist; no auto-decommission; a fleet comes down only on a deliberate org end-of-support decision — remove its registry entry, then tear down manually; single-step climb), pointing at the version-migration `notary-host.md` § Lifecycle as the authoritative home.
- `notary` node type to release fleets (one always-on droplet per migration notary).
- `domino_cli` crate.
- `hf-swapper` and `hash-explorer` node types to release fleets.
- Server types are now defined in a `nodeTypes` registry in `main.go`; adding a type is one entry there plus its `<name>-count` / `<name>-size` keys in `defaults.yaml`.

### Changed

- The cloud-init base-image contract — the `lair-keystore.service` + `holochain.service` systemd units and the first-boot core (lair passphrase, lair init, connection-URL discovery, conductor-config patch) — is factored into `cloudinit/base/` as the single shared source, so the local-testnet fleet image (`automation/local/fleet/`) consumes the SAME bytes and cannot drift from prod. `main.go` reads those files and base64-injects them into the rendered cloud-config via `encoding: b64` write_files (`renderCloudInit` / `readBaseFileB64`); the fleet Dockerfile COPYs them verbatim from a `heart` build context. The InfluxDB metrics env moved out of the inlined `holochain.service` into a prod-only `holochain.service.d/10-metrics.conf` drop-in — that split is exactly what lets the metrics-less local fleet reuse the base unit unchanged. New `TestRenderCloudInit` asserts every template action resolves, the output is valid cloud-init YAML, the three base files are injected byte-identical to `cloudinit/base/`, and the InfluxDB env lives in the drop-in (not the base unit). Rendered user-data is unchanged in content (the units + core moved from inline heredocs to b64-injected write_files); run a `pulumi preview` sanity check before shipping per the deploy DoD.
- Default fleet shape shrinks to 4 droplets per release: `always-online-count` and `unyt-bridging-count` default to `0` (the service nodes each run a full conductor + agent, so dedicated always-on peers add nothing; unyt-bridging never took on its intended bridging duty) and `notary-count` defaults to `1` (single-notary policy for now — raise per release when a window needs resilience). The role types stay defined in `nodeTypes`; re-enabling one is a count bump plus restoring its parked config from `automation/config/disabled/`. Docs (`README`, `deploy-new-release`, `setup-progenitor`, `Pulumi.release.yaml.example`) updated to the 4-role fleet, including the watchtower observer now living on the hash-explorer node.
- bump cloud-init HOLOCHAIN_VERSION and holo-keyutil deps to Holochain 0.6.2-rc.0
- Deploy model is now one Pulumi stack per unyt release, namespaced by the `heart:release` config value. Holochain/keyutil versions, network endpoints (bootstrap/signal/relay/auth), InfluxDB url/org/bucket, droplet sizes and counts are all per-release config keys; their defaults live in `defaults.yaml` (edit it to change a default for all releases). See `Pulumi.release.yaml.example` and `doc/deploy-new-release.md`.
- Makefile reworked around the Pulumi per-release workflow (`make new-release RELEASE=…`, `preview`, `up`, `destroy`); dropped the dead Terraform/Vagrant targets.

### Removed

- The `alt` second-fleet hack: `createAlt` and the `*-alt-count` config keys. Use a separate release stack instead of running a parallel `alt` set.
- Dead Terraform/Vagrant-era files: `CONTRIBUTING.md` (documented a workflow that no longer exists), `services/` (conductor config now inlined in cloud-config), `scripts/test.sh` (stale, unreferenced on-node smoke test), and the obsolete `Pulumi.heart.yaml` stack.

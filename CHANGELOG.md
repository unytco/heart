# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `domino_cli` crate.

### Changed

- bump cloud-init HOLOCHAIN_VERSION and holo-keyutil deps to Holochain 0.6.1
- Deploy model is now one Pulumi stack per unyt release, namespaced by the `heart:release` config value. Holochain/keyutil versions, network endpoints (bootstrap/signal/relay/auth), InfluxDB url/org/bucket, droplet sizes and counts are all per-release config keys with defaults; see `Pulumi.release.yaml.example` and `doc/deploy-new-release.md`.
- Makefile reworked around the Pulumi per-release workflow (`make new-release RELEASE=…`, `preview`, `up`, `destroy`); dropped the dead Terraform/Vagrant targets.

### Removed

- The `alt` second-fleet hack: `createAlt` and the `*-alt-count` config keys. Use a separate release stack instead of running a parallel `alt` set.
- Dead Terraform/Vagrant-era files: `CONTRIBUTING.md` (documented a workflow that no longer exists), `services/` (conductor config now inlined in cloud-config), `scripts/test.sh` (stale, unreferenced on-node smoke test), and the obsolete `Pulumi.heart.yaml` stack.

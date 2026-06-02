# heart — Agent Instructions

## Purpose

**HEART** (Holochain Environment & Agent Runtime Toolkit) is a Pulumi (Go) program that provisions and manages Holochain nodes on DigitalOcean via cloud-init. Each node is an Ubuntu 24.04 droplet that boots a pinned Holochain + Lair Keystore, ships metrics to InfluxDB via Telegraf, and self-registers against the auth server on first boot. Every platform release runs on its own dedicated fleet, managed as **one Pulumi stack per release**, so release fleets coexist without name collisions or downtime. The repo also ships `holo-keyutil`, a small Rust helper (agent-key utilities) pulled onto droplets at first boot.

## Classification

`service` — provisions and runs infrastructure. Orchestrated alongside [`automation/`](../automation/), which consumes its output.

## Stack

- **Go** `1.25.6` (`go.mod`) + **Pulumi** Go SDK (`Pulumi.yaml`, `main.go`) — the provisioning program.
- **Rust** crate `holo-keyutil/` (edition 2021; pins `lair_keystore_api = "=0.6.3"`, `holo_hash = "0.6.1"`).
- **`flake.nix`** bundles `pulumi`, `pulumi-go`, `go`, `jq` — **`nix develop -c` is required** for the go / pulumi commands below.

## Build

```bash
nix develop -c go build ./...                  # Pulumi provisioning program
( cd holo-keyutil && cargo build --release )    # key helper (released to GitHub, fetched at first boot)
```

## Format

Apply, then verify:

```bash
nix develop -c go fmt ./...
nix develop -c gofmt -l .                       # check: prints files needing formatting (empty = clean)
( cd holo-keyutil && cargo fmt && cargo fmt --check )
```

## Test

```bash
nix develop -c go test ./...
```

`main_test.go` covers release-name validation, IP-key generation, and defaults loading — **parsing/validation logic only, no live DigitalOcean**. Verify provisioning changes with `nix develop -c pulumi preview` (dry-run) and Pulumi mocks, **not** by SSHing into prod and pasting logs.

## Deploy

One Pulumi stack per release:

```bash
nix develop                         # enter the dev shell first
make new-release RELEASE=<name>     # init the per-release stack
pulumi config set ...               # DO token, influx-token (secrets), versions
make preview                        # review the plan
make up                             # create droplets; writes releases/<release>/ips.json
```

Droplets boot from the `cloudinit/cloud-config.yaml` template (rendered by Pulumi). The Holochain binary version is the stack key `heart:holochain-version` (default in `defaults.yaml`), rendered into cloud-init as `HOLOCHAIN_VERSION` — this is the pin the [`upgrade-holochain-version`](../.claude/skills/upgrade-holochain-version/SKILL.md) skill bumps.

## Related repos in workshop

- [`automation/`](../automation/) reads heart's `releases/<release>/ips.json` (keyed by `server.heart_node`); `make pull-secrets` there materializes CF tunnel secrets from this stack.
- `holo-keyutil` pins `lair_keystore_api` / `holo_hash` — bump them in lockstep with the rest of the workshop during a Holochain upgrade.
- See workshop [`AGENTS.md`](../AGENTS.md) for the full map.

## Changelog

File: [`./CHANGELOG.md`](./CHANGELOG.md). Format: [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), `## [Unreleased]` at the top, standard subsections (Added/Changed/Deprecated/Removed/Fixed/Security). One bullet per agent change, ≤120 chars, present-tense imperative; branch-type → section mapping per [workshop `docs/WORKSHOP_WORKFLOW.md § Changelog conventions`](../docs/WORKSHOP_WORKFLOW.md). Call out **operator-impacting** changes (new stack config keys, changed droplet sizing/counts, cloud-init changes) under `### Changed`.

## Repo-specific rules

- **One Pulumi stack per release.** Every DO resource is namespaced by `heart:release` (droplet names, `release:<release>` tags). Never reuse a stack across releases — that namespacing is exactly what lets fleets coexist.
- **The InfluxDB token is intentionally plaintext in rendered cloud-init UserData.** It's a Pulumi secret (encrypted in the stack file) but must be readable at first boot, before systemd secret management exists, so Telegraf / the holochain service can start. This is by design — do **not** "fix" it by secret-tainting the cloud-init path.
- **Node types are defined in `main.go`.** The five types (`heart-always-online`, `blockchain-bridging`, `unyt-bridging`, `hf-swapper`, `hash-explorer`) plus their sizing/count keys in `defaults.yaml`; adding a type means editing both.
- **Required per-stack config** (`heart:release`, `heart:project-name`, `digitalocean:token`, `heart:influx-token`) has no default — Pulumi errors at preview if missing. Everything else falls back to `defaults.yaml`.
- **Secrets are gitignored** (`*.pem`, `*.key`, `id_rsa*`, `.env`, `credentials.json`) and blocked from agent reads. Don't add them to the repo or echo them.

## Lessons learned

_Append entries here whenever an agent (or human) loses time to something a guardrail would have prevented. Keep each entry: date, short symptom, concrete fix._

# heart — Agent Instructions

> **This repo follows the workshop root's patterns — it does not define its own.** Development workflow, process, changelog conventions, and spec/feature-doc discipline live in the workshop: [`CLAUDE.md`](../CLAUDE.md), [`AGENTS.md`](../AGENTS.md), [`documentation/DEVELOPMENT_WORKFLOW.md`](../documentation/DEVELOPMENT_WORKFLOW.md). Below is only what's specific to THIS repo.

## Purpose

`service` — **HEART** (Holochain Environment & Agent Runtime Toolkit) is a Pulumi (Go) program that provisions and manages Holochain nodes on DigitalOcean via cloud-init. Each node is an Ubuntu 24.04 droplet that boots a pinned Holochain + Lair Keystore, ships metrics to InfluxDB via Telegraf, and self-registers against the auth server on first boot. Every platform release runs on its own dedicated fleet, managed as **one Pulumi stack per release**, so release fleets coexist without name collisions or downtime. The repo also ships `holo-keyutil`, a small Rust helper (agent-key utilities) pulled onto droplets at first boot.

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

`main_test.go` covers release-name validation, IP-key generation, defaults loading, cloud-init rendering and the ASCII guard on the rendered user-data — **parsing/validation logic only, no live DigitalOcean**. Verify provisioning changes with `nix develop -c pulumi preview` (dry-run) and Pulumi mocks, **not** by SSHing into prod and pasting logs.

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

## Repo-specific rules

- **One Pulumi stack per release.** Every DO resource is namespaced by `heart:release` (droplet names, `release:<release>` tags). Never reuse a stack across releases — that namespacing is exactly what lets fleets coexist.
- **`cloudinit/` is user-data: any byte change replaces every droplet in the stack.** DigitalOcean treats `userData` as replace-on-change, and `cloudinit/base/*` is base64-injected into it, so even a comment typo fix destroys and recreates the fleet — losing each node's agent key, source chain and auth-server registration. Run `make preview` before landing any `cloudinit/` edit, however cosmetic, and expect a live fleet to need a migration window rather than a `make up`.
- **`cloudinit/` is printable-ASCII only.** cloud-init's YAML loader rejects a non-ASCII byte (`unacceptable character #x0080`) or a C0 control (`control characters are not allowed`) by discarding the *entire* config, so the droplet boots bare with no conductor, lair or registration and nothing reports it. `renderCloudInit` blocks such a byte in the rendered user-data and screens the `heart:*` config values interpolated into it; `TestCloudInitTreeIsASCII` blocks it in `cloudinit/base/*`, which is base64-encoded and so invisible to that guard.
- **The InfluxDB token is intentionally plaintext in rendered cloud-init UserData.** It's a Pulumi secret (encrypted in the stack file) but must be readable at first boot, before systemd secret management exists, so Telegraf / the holochain service can start. This is by design — do **not** "fix" it by secret-tainting the cloud-init path.
- **Node types are defined in `main.go`.** The six types (`heart-always-online`, `blockchain-bridging`, `unyt-bridging`, `hf-swapper`, `hash-explorer`, `notary`) plus their sizing/count keys in `defaults.yaml`; adding a type means editing both.
- **Required per-stack config** (`heart:release`, `heart:project-name`, `digitalocean:token`, `heart:influx-token`) has no default — Pulumi errors at preview if missing. Everything else falls back to `defaults.yaml`.
- **Secrets are gitignored** (`*.pem`, `*.key`, `id_rsa*`, `.env`, `credentials.json`) and blocked from agent reads. Don't add them to the repo or echo them.

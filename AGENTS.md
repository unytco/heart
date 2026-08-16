# heart — Agent Instructions

> **This repo follows the workshop root's patterns — it does not define its own.** The workflow, the per-repo definition of done and the submodule ship order live in [`documentation/DEVELOPMENT_WORKFLOW.md`](../documentation/DEVELOPMENT_WORKFLOW.md). Below is only what's specific to THIS repo.

## Purpose

`service` — **HEART** (Holochain Environment & Agent Runtime Toolkit) is a Pulumi (Go) program that provisions and manages Holochain nodes on DigitalOcean via cloud-init. Each node is an Ubuntu 24.04 droplet that boots a pinned Holochain + Lair Keystore, ships metrics to InfluxDB via Telegraf, and self-registers against the auth server on first boot. Every platform release runs on its own dedicated fleet, managed as **one Pulumi stack per release**, so release fleets coexist without name collisions or downtime. The repo also ships `holo-keyutil`, a small Rust helper (agent-key utilities) pulled onto droplets at first boot.

## Stack

- **Go** `1.25.6` (`go.mod`) + **Pulumi** Go SDK (`Pulumi.yaml`, `main.go`) — the provisioning program.
- **Rust** crate `holo-keyutil/` (edition 2021; pins `lair_keystore_api = "=0.7.1"`, `holo_hash = "0.7.0"`).
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
( cd holo-keyutil && cargo test )                          # key helper
HEART_HOLOCHAIN_BIN=$(which holochain) nix develop -c go test ./...   # see below
```

`main_test.go` covers release-name validation, IP-key generation, defaults loading, cloud-init rendering and the ASCII guard on the rendered user-data — **parsing/validation logic only, no live DigitalOcean**. Verify provisioning changes with `nix develop -c pulumi preview` (dry-run) and Pulumi mocks, **not** by SSHing into prod and pasting logs.

`TestConductorConfigKeysMatchHolochainSchema` is the exception that wants a binary: it re-derives the conductor-config allow-lists from `holochain --config-schema`, so a hand edit that drifts from the real serde contract fails instead of shipping. It **skips** when there is no `holochain` on `PATH` (which is CI's case), so run it with `HEART_HOLOCHAIN_BIN` pointing at the pinned release whenever you touch `cloudinit/`'s conductor config or bump `heart:holochain-version`. Setting that variable makes every reason it could not run a failure rather than a skip — **the dev shell ships no holochain**, so `$(which holochain)` from inside it would otherwise expand to nothing and the run would look clean having checked none of it.

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
- **`cloudinit/` is user-data: any byte change re-renders it, and DigitalOcean replaces a droplet whose `userData` changed.** `cloudinit/base/*` is base64-injected into it, so even a comment typo fix means new droplets. That is the normal path rather than a hazard — a fleet is provisioned from the commit its release ships and stays on it, so a `cloudinit/` edit reaches production on the next release's stack. Run `make preview` before landing one anyway: rendering is where the ASCII, quoting and config-value guards run, and a droplet that boots on bad user-data reports nothing.
- **`cloudinit/` is printable-ASCII only.** cloud-init's YAML loader rejects a non-ASCII byte (`unacceptable character #x0080`) or a C0 control (`control characters are not allowed`) by discarding the *entire* config, so the droplet boots bare with no conductor, lair or registration and nothing reports it. `renderCloudInit` blocks such a byte in the rendered user-data and screens the `heart:*` config values interpolated into it; `TestCloudInitTreeIsASCII` blocks it in `cloudinit/base/*`, which is base64-encoded and so invisible to that guard.
- **Every `{{ . }}` in the template is quoted, no config value may be empty, and none may carry `"`, `\`, `$`, `%` or a backtick.** The three conductor URLs land in `/etc/holochain/conductor-config.yaml` — a second YAML document nested in the user-data as a literal block scalar and parsed by *Holochain*, not cloud-init — where an unquoted `#` or `: ` corrupts a file the render guard structurally cannot see, and the node boots with no bootstrap peer. Quoting carries those bytes safely; the screen is what keeps the quoting itself intact. **Being quoted is not one guarantee** — the template interpolates into four embedded languages, each keeping its own bytes live inside the quotes: `"` closes the scalar at all four, `\` escapes at all four (bash included, before `$` `` ` `` `"` `\` or a newline), `$` expands in the first-boot shell **and** in `telegraf.conf` (Telegraf substitutes the environment before parsing), a backtick substitutes in the shell only, and `%` introduces a systemd specifier in `Environment=` — an *unknown* one (`%20` from a percent-encoded URL) drops the whole assignment and at least logs, a *known* one (`%H`, `%a`) verifies clean and rewrites the value saying nothing at all. The shell sites run **as root**, so a `$(…)` in `heart:auth-server` would execute at first boot. `TestCloudInitTemplateQuotesEveryAction` enforces the quoting, but a *new* interpolation site means re-deriving that byte list for its language — a quote count cannot tell a YAML scalar from a `sed` replacement.
- **The InfluxDB token is intentionally plaintext in rendered cloud-init UserData.** It's a Pulumi secret (encrypted in the stack file) but must be readable at first boot, before systemd secret management exists, so Telegraf / the holochain service can start. This is by design — do **not** "fix" it by secret-tainting the cloud-init path.
- **Node types are defined in `main.go`.** The six types (`heart-always-online`, `blockchain-bridging`, `unyt-bridging`, `hf-swapper`, `hash-explorer`, `notary`) plus their sizing/count keys in `defaults.yaml`; adding a type means editing both.
- **Required per-stack config** (`heart:release`, `heart:project-name`, `digitalocean:token`, `heart:influx-token`) has no default — Pulumi errors at preview if missing. Everything else falls back to `defaults.yaml`.
- **Secrets are gitignored** (`*.pem`, `*.key`, `id_rsa*`, `.env`, `credentials.json`) and blocked from agent reads. Don't add them to the repo or echo them.

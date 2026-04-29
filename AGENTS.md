# heart — Agent Instructions

## Purpose

**H**olochain **E**nvironment & **A**gent **R**untime **T**oolkit:
Pulumi-driven provisioner that brings up Holochain-conductor-ready
nodes on DigitalOcean via cloud-init. Owns the cloud-init templates
under [`cloudinit/`](cloudinit/), the `holo-keyutil` Go binary for
key handling, and ancillary services under [`services/`](services/).

## Classification

`service` — the provisioner that creates servers
[`automation/`](../automation/) then deploys onto.

## Stack

- **Go** root binary (`main.go`, `go.mod`, `go.sum`) — Pulumi program.
- **Pulumi** — `Pulumi.yaml`, `Pulumi.heart.yaml` (per-stack config).
  Runtime: `go`.
- Cloud-init under [`cloudinit/`](cloudinit/).
- Helper tool: [`holo-keyutil/`](holo-keyutil/) (Go).
- **Requires `nix develop -c …`** — see
  [`flake.nix`](flake.nix). The workshop's
  [Nix discipline section](../AGENTS.md#nix-discipline) lists this
  repo.

## Build

```bash
nix develop -c go build ./...           # build all Go packages
nix develop -c make build               # if Makefile target exists
```

## Format

Apply, then verify:

```bash
nix develop -c gofmt -w .
nix develop -c bash -c 'gofmt -l . | (! grep .)'   # exits non-zero on any unformatted file
nix develop -c go vet ./...                         # lint pass
```

## Test

```bash
nix develop -c go test ./...
```

## Deploy

`heart` itself is the deployer for DigitalOcean nodes. Typical use:

```bash
nix develop -c pulumi up --stack heart   # creates / updates servers
```

`automation/` then deploys conductors + apps onto the resulting
servers. See workshop
[Deployment hub](../AGENTS.md#deployment-hub-automation).

## Related repos in workshop

- Used by [`automation/`](../automation/) — `automation` deploys
  workloads onto the servers `heart` provisions.
- Cloud-init produces servers that host
  [`unyt-sandbox/unyt`](../unyt-sandbox/unyt/) conductors and the
  always-online `heart-always-online-N` fleet.

## Changelog

File: [`./CHANGELOG.md`](./CHANGELOG.md). Format: [Keep a Changelog
1.1.0](https://keepachangelog.com/en/1.1.0/) with `## [Unreleased]`
at the top and standard subsections. One bullet per agent change,
≤120 chars, present-tense imperative. Branch-type → section mapping
per workshop
[`branch-and-pr-workflow.mdc`](../.cursor/rules/branch-and-pr-workflow.mdc).

Cloud-init template changes ARE operator-impacting (the next server
brought up will use the new template). Note them under `### Changed`.
Holochain / lair version bumps in cloud-init are particularly
sensitive — be explicit about old → new.

## Repo-specific rules

- **Cloud-init scripts must be idempotent.** Re-running on an
  already-provisioned server should be a no-op, not a reset.
- **Secrets stay in Pulumi config** (encrypted), never in the repo.
  `holo-keyutil` is the only sanctioned signing-key handler.
- **Pulumi state lives in Pulumi Cloud / S3** depending on stack —
  do not commit `.pulumi/` or stack outputs that leak addresses.

## Lessons learned

_Append entries here whenever an agent (or human) loses time to
something a guardrail would have prevented. Keep each entry: date,
short symptom, concrete fix._

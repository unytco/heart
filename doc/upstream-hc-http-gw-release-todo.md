# TODO: Upstream `hc-http-gw` release-binary workflow

Heart's cloud-init pulls every other binary it ships to droplets
(holochain, lair-keystore, hc, holo-keyutil) from a GitHub release with
a `<binary>-<target-triple>` asset naming convention. The
[holochain/hc-http-gw](https://github.com/holochain/hc-http-gw) repo
currently publishes _no_ binary assets — every release (`v0.1.0`
through `v0.4.0-dev.1`) has zero attached files; only the
auto-generated source tarball/zip is present. Verified via
`curl https://api.github.com/repos/holochain/hc-http-gw/releases | jq '.[].assets | length'`.

## Current status: heart-side mirror in place (stopgap)

The
[`release-hc-http-gw.yml`](../.github/workflows/release-hc-http-gw.yml)
workflow in this repo builds upstream tags on demand and uploads the
binary to a heart release named `hc-http-gw-<upstream-tag>`.
Cloud-init now pulls from
`https://github.com/unytco/heart/releases/download/hc-http-gw-${HC_HTTP_GW_VERSION}/hc-http-gw-x86_64-unknown-linux-gnu`,
so droplet boots are unblocked.

The mirror is a stopgap. The upstream PR below is still the right
long-term home for the binary because:

- Heart's CI builds upstream code with no review hook between upstream
  tag and our published binary — moving the build to upstream's CI
  closes that supply-chain gap.
- Every other Holochain consumer (Kangaroo, Holochain Launcher, etc.)
  also has to write a mirror until upstream ships one. Upstream-side
  fix is once; everyone-else-mirroring is N times.
- Heart owns one binary distribution today (`holo-keyutil`, which we
  also author). Mirroring `hc-http-gw` doubles that responsibility for
  a project we don't own.

This doc is the executable spec for the PR that closes that gap.

## Goal

When a tag matching `v*` is pushed to `holochain/hc-http-gw`, build a
release binary for at minimum `x86_64-unknown-linux-gnu` and attach it
to the release as `hc-http-gw-x86_64-unknown-linux-gnu`. Same naming
convention as `holochain/holochain` (verified on release
`holochain-0.6.1`: `hc-x86_64-unknown-linux-gnu`,
`holochain-x86_64-unknown-linux-gnu`, etc.).

Once that lands, the cloud-init line at
[cloudinit/default/cloud-config.yaml](../cloudinit/default/cloud-config.yaml)
in `holochain-first-boot`'s hc-http-gw block becomes a working
download, no other changes required on the heart side beyond bumping
`HC_HTTP_GW_VERSION`.

## PR target

- Repo: `holochain/hc-http-gw`
- Branch: `main`
- License: Apache-2.0, OK to contribute.
- Recent activity: contributors `@ThetaSinner` and `@cdunster` (plus
  the `holochain-release-automation2` bot) merged work as recently as
  May 12 2026 — the project is actively maintained.

## Existing release plumbing in that repo

`.github/workflows/`:

- `prepare-release.yml` — opens a release-prep PR (version bump,
  changelog) via `holochain/actions/.github/workflows/prepare-release.yml@v1.3.0`.
- `publish-release.yml` — runs on push to `main`/`main-*`; calls
  `holochain/actions/.github/workflows/publish-release.yml@v1.3.0`,
  which only publishes to crates.io. **It does not produce or upload
  binaries today.**
- `test.yaml` — CI build/test.
- `cargo_update.yaml`, `flake_update.yaml` — scheduled maintenance.

There is no `release.yml` that triggers on tag push. That's the file
the PR adds.

## Workflow file to add

Path: `.github/workflows/release-binaries.yml`

Triggers: push of any tag matching `v*` (the publish-release
workflow's reusable callee creates these tags, so this file is purely
reactive — no human has to push tags manually). Also supports
`workflow_dispatch` with a manual `tag` input for re-runs against an
existing release that's missing assets.

Build matrix (start minimum, scale up as upstream wants):

| Target triple | Runner | Notes |
| --- | --- | --- |
| `x86_64-unknown-linux-gnu` | `ubuntu-22.04` | **Required for heart fleet.** Use 22.04 (not 24.04) so the binary's glibc requirement is broad — droplet base is ubuntu-24-04 today but downstreams running 22.04 must still be able to consume it. |
| `aarch64-unknown-linux-gnu` | `ubuntu-22.04-arm` _or_ `ubuntu-22.04` + cross | Useful future-proofing; ARM is increasingly common on Hetzner / OCI / DO arm pools. Not blocking. |
| `x86_64-apple-darwin` | `macos-13` | Parity with `holochain/holochain` release matrix. Useful for dev-on-Mac workflows. Not blocking. |
| `aarch64-apple-darwin` | `macos-14` | Apple Silicon. Not blocking. |

Skeleton (illustrative; reviewer should fold into upstream's existing
conventions for indentation, action versions, etc.):

```yaml
name: Release binaries

on:
  push:
    tags:
      - "v*"
  workflow_dispatch:
    inputs:
      tag:
        description: "Existing tag to (re-)build binaries for"
        required: true
        type: string

jobs:
  build:
    strategy:
      fail-fast: false
      matrix:
        include:
          - target: x86_64-unknown-linux-gnu
            runner: ubuntu-22.04
          # Uncomment to extend the matrix:
          # - target: aarch64-unknown-linux-gnu
          #   runner: ubuntu-22.04-arm
          # - target: x86_64-apple-darwin
          #   runner: macos-13
          # - target: aarch64-apple-darwin
          #   runner: macos-14
    runs-on: ${{ matrix.runner }}
    steps:
      - uses: actions/checkout@v5
        with:
          ref: ${{ github.event.inputs.tag || github.ref }}

      - uses: dtolnay/rust-toolchain@stable
        with:
          # Honour the pinned channel from rust-toolchain.toml; this
          # action reads it automatically when `toolchain:` is omitted.
          targets: ${{ matrix.target }}

      - name: Build
        run: cargo build --release --target ${{ matrix.target }} --bin hc-http-gw

      - name: Rename artifact
        shell: bash
        run: |
          set -eu
          src="target/${{ matrix.target }}/release/hc-http-gw"
          if [[ "${{ matrix.target }}" == *"windows"* ]]; then
              src="${src}.exe"
          fi
          mkdir -p out
          cp "$src" "out/hc-http-gw-${{ matrix.target }}"

      - name: Upload to release
        uses: softprops/action-gh-release@v2
        with:
          tag_name: ${{ github.event.inputs.tag || github.ref_name }}
          files: out/hc-http-gw-${{ matrix.target }}
          fail_on_unmatched_files: true
```

## Toolchain pin

`hc-http-gw/rust-toolchain.toml` currently pins channel `1.88.0` with
`rustfmt`, `clippy`, and the `wasm32-unknown-unknown` target. The PR
above respects that automatically because `dtolnay/rust-toolchain`
reads `rust-toolchain.toml` when the `toolchain:` input is omitted.

If upstream prefers to pin the action's `toolchain:` input explicitly,
mirror whatever channel `rust-toolchain.toml` declares at the time of
the PR.

## Tests in CI

`hc-http-gw`'s `Cargo.toml` already declares Holochain-flavoured
dev-deps that compile in CI (`test.yaml`); adding a `release-binaries`
workflow has no overlap with the test workflow's responsibilities.

The new workflow should _not_ re-run unit/integration tests — that's
`test.yaml`'s job and the release pipeline assumes tests have already
passed on the source branch. Coupling the two would slow tag releases
needlessly.

## Stretch: SBOM + checksums

Optional improvements for the same PR or a follow-up:

- Generate a SHA-256 checksum file per artifact (`sha256sum
  hc-http-gw-${TARGET} > hc-http-gw-${TARGET}.sha256`) and attach it.
  Heart's cloud-init can then verify the download in a follow-up.
- Sign the artifacts with cosign / sigstore. The `holochain/holochain`
  releases don't do this today, so leaving it out keeps parity, but
  it's a reasonable ask if reviewers raise supply-chain concerns.

## Coordination

When opening the PR:

1. Title: `feat: add release-binaries workflow for tagged builds`.
2. Body: link to this doc and the heart cloud-init that consumes the
   asset, so reviewers can see the deployment use case is concrete and
   in production.
3. CC `@ThetaSinner` and `@cdunster` (most recent contributors).
4. Post a note in the Holochain Discord `#dev` channel after the PR is
   open, referencing the cloud-deployment use case (heart on DO
   droplets) — that's a clear, narrow, reasonable motivation that
   shouldn't be controversial.

## When this lands

1. Bump `HC_HTTP_GW_VERSION` in both
   [cloudinit/default/cloud-config.yaml](../cloudinit/default/cloud-config.yaml)
   and (via `./scripts/sync-cloud-configs.sh`)
   [cloudinit/alt/cloud-config.yaml](../cloudinit/alt/cloud-config.yaml)
   to the first tag that has the binary attached.
2. Push the heart change through the usual flow; new droplets will
   pull the binary at first boot, `hc-http-gw.service` will start once
   the operator runs `hc-http-gw-configure --app-id <id>`, and the
   `ConditionPathExists=` gate will lift automatically.
3. Existing droplets (if any have been provisioned in the gap) can be
   retrofitted by:

   ```bash
   ssh root@<droplet-ip>
   HC_HTTP_GW_VERSION=v0.X.Y
   curl -fsSL -o /usr/local/bin/hc-http-gw \
       "https://github.com/holochain/hc-http-gw/releases/download/${HC_HTTP_GW_VERSION}/hc-http-gw-x86_64-unknown-linux-gnu"
   chmod 755 /usr/local/bin/hc-http-gw
   systemctl daemon-reload
   systemctl start hc-http-gw.service
   ```

4. Update [CHANGELOG.md](../CHANGELOG.md) under `### Changed` noting
   the upstream binary is now available and which version is pinned.

## Alternatives considered (and why not)

- **Mirror the release in `heart/` itself.** A `release-hc-http-gw.yml`
  workflow next to `release-holo-keyutil.yml` could clone
  `hc-http-gw@<rev>`, build the binary, and upload it to a heart
  release tag. This works and is fully under our control but pulls a
  responsibility into this repo that doesn't belong here (we'd own
  binary distribution for a project we don't author). Acceptable as a
  short-term unblocker if the upstream PR stalls beyond ~2 weeks.

- **Cargo-install at first boot.** Adds ~5 minutes to first boot,
  ~1.5 GB rust toolchain on every droplet, and a runtime dependency
  on crates.io being reachable. Rejected.

- **Build locally + commit the binary into heart.** No. We don't commit
  binaries to source control, and bumping the version becomes a
  high-friction binary diff PR.

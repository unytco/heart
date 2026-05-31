# Deploying a New Release Fleet

Each unyt release runs on its own dedicated set of servers, managed as a **single Pulumi stack per release**. Standing up a new release means creating a new stack — the existing release fleets are never touched, so there is no downtime and no risk of disturbing a live deployment. When a release is retired you tear down just its stack.

This is the recommended path for "deploy a new set of servers for a new version of unyt". For what gets installed on each node and how to install a `.happ`, see [Setup an Always-On Node](./setup-always-on-node.md).

## How a release maps to infrastructure

A stack's `heart:release` value (e.g. `v0-7-0`) namespaces every resource it creates:

- Droplets are named `heart-always-online-<release>-N`, `blockchain-bridging-<release>-N`, `unyt-bridging-<release>-N`.
- Every droplet is tagged `release:<release>` (plus its role tag), so you can filter a release's fleet in the DigitalOcean console or API.

Two release fleets therefore coexist cleanly in the same DigitalOcean project without name collisions.

## Prerequisites

- The dev shell, so `pulumi` and `go` are on `PATH`:
  ```bash
  nix develop
  ```
- A DigitalOcean API token and an SSH key already added to the DigitalOcean account.
- The InfluxDB token for metrics shipping.
- **The InfluxDB bucket for this release must already exist.** This program does not create it. A per-release bucket (e.g. `unyt-v0-7-0`) keeps each fleet's metrics isolated; create it in InfluxDB before deploying.

## Steps

### 1. Create the stack

```bash
make new-release RELEASE=v0-7-0
```

This runs `pulumi stack init`, selects the stack, sets `heart:release`, and prints the remaining values to set.

### 2. Set the required config

```bash
pulumi config set --secret digitalocean:token   # paste DO token
pulumi config set --secret heart:influx-token    # paste InfluxDB token
pulumi config set heart:project-name  Holo
pulumi config set heart:influx-bucket unyt-v0-7-0
```

Secrets are encrypted per-stack and cannot be copied from another stack's config file — set them fresh for each release.

### 3. (Optional) Override anything else

Everything else has a default baked into `main.go`. Override only what differs for this release. The full list with defaults is in [`Pulumi.release.yaml.example`](../Pulumi.release.yaml.example). Common ones:

```bash
# Pin a different Holochain version for this release
pulumi config set heart:holochain-version holochain-0.6.2

# Give the release its own network endpoints (see the caveat below)
pulumi config set heart:bootstrap-url https://hc-auth-iroh-unyt-v0-7-0.holochain.org/
pulumi config set heart:relay-url     https://iroh-relay-unyt-v0-7-0.holochain.org
pulumi config set heart:auth-server   https://hc-auth-iroh-unyt-v0-7-0.holochain.org

# Adjust the fleet shape (bridging counts are capped at 1)
pulumi config set heart:always-online-count 4
pulumi config set heart:always-online-size  s-2vcpu-4gb
```

### 4. Preview and deploy

```bash
make preview      # review the plan
make up           # create the droplets
```

Cloud-init runs on first boot to install Holochain, Lair, Telegraf, and the registration service. Then follow [Setup an Always-On Node](./setup-always-on-node.md) from Part 2 (agent keys) onward to bring each node into service and install the `.happ`.

## Network isolation caveat

Setting `heart:bootstrap-url` / `heart:relay-url` / `heart:auth-server` controls which network *endpoints* the conductors use. It does **not** by itself put a release on a separate Holochain network: peers only segregate by DNA, which is determined by the **network seed applied when the `.happ` is installed** (the manual install step), not by this Pulumi config. If two releases share the same bootstrap/relay endpoints *and* the same network seed, their nodes will still gossip. Decide per release whether it needs its own endpoints, its own seed, or both.

## Switching between releases

Pulumi commands act on the currently selected stack. Either select it once:

```bash
pulumi stack select v0-7-0
make preview
```

or target a stack per-command without switching:

```bash
make preview STACK=v0-7-0
make config  STACK=v0-6-0
```

`make stacks` lists every release stack.

## Decommissioning a release

When a release is retired, tear down only its stack:

```bash
make destroy STACK=v0-6-0
pulumi stack rm v0-6-0   # optional: also remove the (now empty) stack
```

Other release fleets are unaffected.

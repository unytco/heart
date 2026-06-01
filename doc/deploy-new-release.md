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
- **Cloudflare credentials for the automation deploy** (the joining-service redeploy and other wrangler steps need them). Copy `automation/.env.example` to `automation/.env` and set `CLOUDFLARE_API_TOKEN` + `CLOUDFLARE_ACCOUNT_ID`; the automation Makefile auto-loads `.env`. The deploy fails fast up front if these are missing, so set them before running any `make <role>`.
- **Metrics default to the shared `unyt` InfluxDB bucket** — no setup needed. Optionally, to isolate a release's metrics, override `heart:influx-bucket` with a bucket that already exists in InfluxDB (this program does not create it).

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
pulumi config set heart:project-name  unyt
```

Metrics ship to the shared `unyt` InfluxDB bucket by default. To isolate this release's metrics instead, also `pulumi config set heart:influx-bucket <existing-bucket>`.

Secrets are encrypted per-stack and cannot be copied from another stack's config file — set them fresh for each release.

### 3. (Optional) Override anything else

Everything else has a default in [`defaults.yaml`](../defaults.yaml) (edit that file to change a default for all releases). Override only what differs for this release with `pulumi config set`. The full list of keys is in [`Pulumi.release.yaml.example`](../Pulumi.release.yaml.example). Common ones:

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

`make up` also writes the provisioned IPv4 addresses to `releases/<release>/ips.json`, keyed by server name (e.g. `heart-always-online-1`, `hf-swapper-1`, `blockchain-bridging-1`). The automation repo references this file by key, so you don't paste IPs by hand — commit it alongside the release. Inspect it with `jq . releases/<release>/ips.json`.

Cloud-init runs on first boot to install Holochain, Lair, Telegraf, and the registration service.

### 5. Authorize the new nodes on the auth server

On first boot each node's `holochain-register` service creates an agent key, submits a `/request-auth` to the auth server (`heart:auth-server`, default `hc-auth-iroh-unyt.holochain.org`), and then **polls `/authenticate` until an admin approves that key**. Until you approve them, the nodes are up but not yet on the network — the conductor won't get its bootstrap credentials.

So after `make up`, an admin must approve each new node's request:

1. Sign in to the auth ops console: <https://hc-auth-iroh-unyt.holochain.org/ops/auth>
2. Click **Approve** on each pending request — one per new node (all six for the default fleet shape).

Once approved, each node's `holochain-register` picks up the credentials on its next poll and Holochain restarts automatically.

### 6. Set up the progenitor first

Before deploying the app to any service node, the **progenitor must be set up**, because it produces two values every other node needs:

- the **progenitor agent key** → goes into `properties.progenitor_pubkey`
- the **network seed** used to create the network → goes into `network_seed`

In the automation repo these two values live in `config/release.json` (release-wide, shared by every node). You cannot deploy the services until they're filled in with the real progenitor values.

Progenitor setup is currently **manual**: install the `.happ`/`.deb` build by hand and bring up a running agent, then read back its agent key and the network seed. See [Setup Progenitor](./setup-progenitor.md).

### 7. Bring nodes into service

> **`config/release.json` is the one file you edit per release.** It holds the release-wide values shared by every node, and the automation scripts derive everything else from it:
> - `release_version` — the unyt-sandbox release tag (e.g. `v0.91.0`). Drives the `.happ` download URL, the heart IP-file path (`releases/<release>/ips.json`, dots→dashes), and the `{version}` substitution in gateway DNA labels.
> - `network_seed` + `properties.progenitor_pubkey` — from the progenitor setup (Step 6).
> - `happ_url_template` / `happ_dest` — usually unchanged.
>
> The per-node `deploy.json` files carry only what's node-specific (`heart_node`, agent `app_id`s, gateway/tunnel blocks). They do **not** carry a hardcoded release label — bumping `release.json` repoints the whole fleet. The one per-release value that lives outside `release.json` is the bridge **lane definition** (Step 7.3 below), because it isn't known until the agreement is created.

Once `config/release.json` has the real `network_seed` + `progenitor_pubkey`, deploy the nodes from the automation repo. Each `make <role>` installs the `.happ` and initialises the agent. For the manual per-node steps, see [Setup an Always-On Node](./setup-always-on-node.md) from Part 2 (agent keys) onward.

**Deploy in dependency order, not all at once:**

1. **`make blockchain-bridging` then `make hf-swapper`** — deploy these two first. Their agent keys (printed in each deploy's results, `config/<role>/results/deploy-result.json`) are needed to **create the agreements in the progenitor account**. Nothing downstream works until these agents exist.
2. **Create the agreements** in the progenitor account using those two agent keys (manual progenitor step, in the progenitor's unyt app).
3. **Update the lane definition in the bridge config.** Creating the agreement yields a **lane definition hash**, shown in the **UI of the progenitor's unyt app**. Copy it into `config/blockchain-bridging/services.json` → `bridge_orchestrator.holochain_lane_definition`. This is **per-release** — a fresh agreement each release means a new lane id, so this field must be updated every time (it's not carried in `release.json`). `setup-blockchain-bridge-services.sh` reads this value and the run will fail if it's stale/placeholder.
4. **Run the bridge + swapper services:** `make blockchain-bridging-services` + `make blockchain-bridging-pricing-oracle`, then `make hf-swapper-hf-swap`. The `unyt-bridging` node also needs its bridging cron: `make unyt-bridging-setup-cron` (it cross-references the blockchain-bridge's results, so run it after both bridges are deployed).
5. **Test** the core bridge/swap flow end-to-end before going further — this is the part that needs real validation.
6. **Then the basic nodes** (`make always-online-1`, `make always-online-2`, `make hash-explorer`). The base `make <role>` is a plain `.happ` install with no cross-node dependency, so these go last and in any order. A node that also exposes a gateway/tunnel (today only `hash-explorer`, identified by a `.tunnel` + `.gateway` block in its `deploy.json`) needs the extra steps in **§ Hash-explorer / gateway nodes** below.

Why this order: the bridge + swapper agent keys feed the progenitor agreements, and that flow is what needs initial testing; the always-online / hash-explorer nodes are basic and independent, so they're safe to leave for the end.

### Hash-explorer / gateway nodes (tunnel + gateway)

Nodes with a `.tunnel` + `.gateway` block in their `config/<role>/deploy.json` need two extra steps after the base `make <role>`. They depend on Cloudflare tunnel secrets being available locally.

**One-time per release stack — seed the tunnel secret.** The CF cert + per-tunnel credentials are stored as Pulumi secrets on the heart stack and pulled onto the operator's machine. Seed them from your raw files using the heart Makefile (run in `heart/`, in `nix develop`):

```bash
make set-tunnel-secret TUNNEL=unyt-tunnel CERT=/path/to/cert.pem CREDS=/path/to/credentials.json STACK=<release>
```

`TUNNEL` must match `.tunnel.tunnel_name` in the automation config. `STACK` is the release stack (e.g. `v0-91-0`) — heart is one stack per release, so the secret lives on that stack.

**Pull the secret onto this machine**, then deploy:

```bash
cd ../automation
make pull-secrets                 # auto-selects heart's stack if there's only one; else PULUMI_STACK=<release>
make hash-explorer                # base .happ deploy (also refreshes the release's DNA hash in results)
make hash-explorer-backend        # tunnel + gateway in one step
```

`make hash-explorer-backend` runs the tunnel then the gateway. The individual `make hash-explorer-tunnel` / `make hash-explorer-gateway` targets still exist for when you need to rotate one without the other (e.g. refresh tunnel creds without rebuilding the gateway). Only nodes whose `deploy.json` carries a `.tunnel` + `.gateway` block get these targets — today that's just `hash-explorer`.

The gateway's DNA registration **auto-resolves** the live DNA hash from the node's `results/deploy-result.json` (falling back to `.gateway.apps[].dnas[].hash` if no result exists), and substitutes `{version}` in the DNA `display_name`/`description` from `release.json` — so you don't hand-edit the DNA hash or labels per release.

**Note on per-release config:** the per-node IP path is also derived from `release.json` — `resolve_server_ip` builds `heart/releases/<release>/ips.json` from `release_version` (dots→dashes), so the configs carry no hardcoded release label. Bumping `release.json` is the single edit that repoints the whole fleet at the new release. (`server.heart_ips_file` can still be set on a config to override the derived path.)

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

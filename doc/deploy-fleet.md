# Deploying the heart fleet (Pulumi-orchestrated)

This is the entry-point guide for the **service-named, Pulumi-orchestrated** deploy flow. `heart/` (Pulumi) provisions the fleet, owns the Cloudflare tunnel + DNS, and exports a `fleet` map; `automation/` consumes that map and runs the per-service deploy steps. Start here.

> New flow — first run notes are flagged inline. If you're migrating existing DigitalOcean droplets into a fresh Pulumi account, read **§6 Migrating existing infra** before any `pulumi up`.

## TL;DR — three commands

From inside `nix develop`:

```bash
# 1. Create or update a release's fleet + provision all servers (asks for anything missing):
./scripts/fleet-up.sh

# 2. Walk through the per-server app deploy (install .happ, gateway, tunnel, …), pausing for key approval:
( cd ../automation && PULUMI_STACK=<org>/heart/<stack> ./scripts/deploy-all.sh )   # or: make deploy-all PULUMI_STACK=…

# 3. Tear it all down to start clean if something broke:
./scripts/fleet-down.sh <stack>
```

The sections below explain what each does and the manual equivalents.

## 0. What gets deployed

The fleet is declared in [`heart/fleet.go`](../fleet.go) — one entry per droplet, each named for its service:

| Server | Service | Notes |
| --- | --- | --- |
| `always-online-1` | always-online | runs a watchtower observer |
| `always-online-2` | always-online | runs a watchtower observer |
| `hf-swapping` | hf-swap | dedicated HF-swap box (conductor + always-on-node + the swap cron) |
| `hash-explorer-1` | hash-explorer backend | conductor + hc-http-gw + cloudflared tunnel |
| `hot-2-mhot-bridge` | HOT↔mHOT bridge | + pricing-oracle + UI |
| `hf-2-infra-bridge` | HF↔infra bridge | + bridge cron |

Principle: each service runs on its own server (e.g. hf-swap is *not* co-located on an always-online node — it has its own `hf-swapping` box).

Each app-version release gets its **own Pulumi stack** (`unyt-heart-vX`); see §7.

## 1. Prerequisites (one-time)

```bash
cd heart
nix develop                     # puts pulumi, go, doctl, cloudflared on PATH
pulumi login                    # your Pulumi Cloud account
```

In your **DigitalOcean** account you need: a project named to match `heart:project-name` (e.g. `Holo`), and your **SSH key added** (every droplet gets every account SSH key, applied only at create — add it first).

## 2. Configure the stack

Pick or create your stack (one per release line), then set config. Replace `unyt-heart` with your stack name:

```bash
pulumi stack select unyt-heart      # or: pulumi stack init unyt-heart

# --- required ---
pulumi config set --secret digitalocean:token        # DO API token
pulumi config set --secret heart:influx-token        # InfluxDB write token
pulumi config set heart:project-name Holo            # must already exist in DO

# --- Cloudflare (tunnel + DNS now owned by Pulumi) ---
pulumi config set --secret cloudflare:apiToken       # scope: Tunnel:Edit, Zone.DNS:Edit, Zone:Read
pulumi config set heart:cf-account-id 42bb42af90168b2108ab750004e4a0dd
pulumi config set heart:cf-zone-name unyt.co
pulumi config set --secret heart:cloudflare-tunnel-secret "$(openssl rand -base64 32)"

# --- post-boot orchestration (optional, see §4b) ---
pulumi config set --secret heart:ssh-private-key -- < ~/.ssh/id_ed25519   # key that can SSH root@droplet
# pulumi config set heart:release-version v0.90.0    # tags + .happ URL; per-version stacks
# pulumi config set heart:orchestrate-postboot true  # end-to-end (install/gateway/tunnel) inside `pulumi up`
```

If you leave `heart:cf-account-id` unset, Cloudflare resources are skipped (hybrid mode) and you manage the tunnel out-of-band as before.

## 3. Preview (always do this first)

```bash
nix develop -c pulumi preview
```

Confirm:
- the `fleet` export and the five droplets plan as expected;
- the Cloudflare tunnel + a proxied CNAME for `unyt-tunnel.unyt.co` appear;
- **on a migration**, renamed droplets show as **update/alias, not replace** (a replace destroys the lair keystore + agent key — stop and see §6 if you see `replace`).

## 4a. Provision (infra only — recommended first)

With `heart:orchestrate-postboot` unset, `pulumi up` only provisions infra (droplets, project, firewall, CF tunnel + DNS) and exports `fleet`:

```bash
nix develop -c pulumi up
```

Then cache the fleet output so the automation scripts can resolve IPs by name:

```bash
cd ../automation
make pull-fleet PULUMI_STACK=<org>/heart/unyt-heart      # writes ~/.config/unyt-deploy/fleet.json
make pull-secrets PULUMI_STACK=<org>/heart/unyt-heart    # CF cert/creds cache (if using locally-managed tunnel)
```

Now deploy per service from `automation/` — these resolve the droplet IP from the Pulumi `fleet` output automatically (no hand-edited IPs):

```bash
make hash-explorer-1                 # reset + install released .happ + agents
# (approve the node's agent key — see §5)
make hash-explorer-1-gateway         # build + start hc-http-gw
make hash-explorer-1-tunnel          # bring up the cloudflared connector

make always-online-1 && make always-online-1-watchtower   # conductor + its watchtower observer
make always-online-2 && make always-online-2-watchtower
make hf-swapping     && make hf-swapping-hf-swap           # dedicated hf-swap box + its swap cron
make hot-2-mhot-bridge ; make hf-2-infra-bridge            # bridges
# …etc; `make help` lists every target
```

## 4b. End-to-end (optional)

Set `heart:orchestrate-postboot true` and `heart:ssh-private-key`, then `pulumi up` drives the whole chain per server (deploy → **await key approval** → gateway → tunnel → DNA capture) via the command provider. The build still invokes the same `automation/scripts/*.sh`. Re-running `pulumi up` with no changes is a no-op. Use this once the per-service path (4a) is proven.

## 5. The one human gate: agent-key approval

On first boot each node generates an agent key and **polls `hc-auth-iroh-unyt.holochain.org` until an admin approves it**. Nothing downstream (gateway/tunnel) works until then. In orchestrated mode (4b) `pulumi up` *blocks* at this step (60-min resumable timeout) — approve the key, and the next `pulumi up` sails through. This step is irreducible; Pulumi can only wait.

## 6. Migrating existing infra (fresh Pulumi account)

Your new account's state is empty but the DO droplets / CF tunnel may already exist:
- A bare `pulumi up` would **create duplicates** — `pulumi import` each existing droplet (by DO id), the tunnel, and the DNS record first.
- Renaming a droplet's logical name is destroy+recreate unless aliased. [`main.go`](../main.go) already maps old→new names via `pulumi.Aliases` for the three renamed boxes (`always-online-1/2`, `hash-explorer-1`). Verify the preview shows rename, not replace.

For a **clean review without touching production**, the simplest path is a throwaway stack provisioning a *single* box: temporarily trim `Fleet` in `fleet.go` to one entry (e.g. just `hash-explorer-1`), `pulumi up` on a `unyt-heart-test` stack, validate, then `pulumi destroy`.

## 7. Per-version fleets

A new release = a new stack off the same program:

```bash
pulumi stack init unyt-heart-v0.91
pulumi config set heart:release-version v0.91.0
# (set the other config as in §2) then:
nix develop -c pulumi up
```

Old versions keep running on their own stacks; retire one with `pulumi destroy --stack unyt-heart-vW`.

## 8. Verify

```bash
# from heart/ — end-to-end probe through the tunnel:
./scripts/smoke-test.sh unyt-tunnel.unyt.co
# launcher source-of-truth still in sync across heart + automation:
( cd ../automation && bash scripts/check-launcher-drift.sh )
# on a node:
ssh root@$(cd ../automation && jq -r '.["hash-explorer-1"].ipv4' ~/.config/unyt-deploy/fleet.json) \
  'systemctl status holochain lair-keystore holochain-register cloudflared hc-http-gw --no-pager'
```

## 9. Teardown

```bash
nix develop -c pulumi destroy --stack <stack>
```

## See also
- [`automation/docs/hash-explorer-backend.md`](../../automation/docs/hash-explorer-backend.md) — deep dive on the tunnel + gateway + registry runbook and secrets/rotation.
- [`setup-always-on-node.md`](setup-always-on-node.md) — older single-node walkthrough (pre-orchestration); still useful for the on-droplet layout and agent-key options.

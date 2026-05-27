# Cutover from `lisa-tunnel` (laptop-hosted) to `unyt-tunnel` (Pulumi-managed)

`heart/cloudflare.go` provisions a **fresh** Cloudflare tunnel named
`unyt-tunnel` in CF account `42bb42af90168b2108ab750004e4a0dd`, with a
CNAME at `unyt-tunnel.unyt.co` and an ingress rule pointing to
`http://127.0.0.1:8090` on each connected droplet.

Until this PR landed, the explorer's traffic was served by a tunnel
named `lisa-tunnel` (id `8b05da1a-a8f8-4367-8574-16ba77295c9e`) that
was created by hand from a developer laptop via
`cloudflared tunnel create lisa-tunnel`. The laptop is the **only**
connector for that legacy tunnel, and `hash-explorer-worker.unyt.co`
proxies to it via `LOCAL_SERVER_URL = https://lisa-tunnel.unyt.co`
(see [`hash-explorer/workers/wrangler.proxy.toml`](../../hash-explorer/workers/wrangler.proxy.toml)).

We deliberately **do not import** the legacy tunnel into Pulumi state:

- The fresh-tunnel path has no `pulumi import` failure modes (URN
  matching, attribute drift, `ConfigSrc=local → cloud` flip during
  `pulumi up`).
- The legacy tunnel keeps serving production from the laptop the whole
  time the new tunnel is being rolled out to droplets. There's never a
  window where no connector exists for the active hostname.
- Once the proxy worker's `LOCAL_SERVER_URL` is updated to the new
  hostname and verified end-to-end, the legacy tunnel is decommissioned
  by stopping the laptop's `cloudflared` and deleting the CF tunnel.

## Sequence

```text
T0 (today): proxy-worker → lisa-tunnel.unyt.co → laptop cloudflared → hc-http-gw on laptop
T1 (after `pulumi up`): unyt-tunnel tunnel exists but has 0 connectors;
                        unyt-tunnel.unyt.co returns 530. Legacy path unchanged.
T2 (after first droplet's cloudflared comes up):
                        unyt-tunnel.unyt.co → droplet cloudflared → droplet hc-http-gw.
                        Production traffic still uses lisa-tunnel.unyt.co.
T3 (after proxy-worker LOCAL_SERVER_URL change):
                        Production traffic now flows through unyt-tunnel. Legacy still up.
T4 (after bake-in, e.g. 24-48h):
                        Stop laptop cloudflared, delete legacy tunnel.
```

## One-time procedure

```bash
cd heart
nix develop -c bash

# 1. Pulumi config (run once per stack)
export CF_ACCOUNT=42bb42af90168b2108ab750004e4a0dd
pulumi config set --secret cloudflare:apiToken <token-with-zone:dns-edit-and-account:tunnel-write>
pulumi config set heart:cf-account-id "$CF_ACCOUNT"
pulumi config set heart:cf-zone-name unyt.co
pulumi config set heart:gw-hostname unyt-tunnel.unyt.co

# Generate a fresh tunnel secret for the new tunnel. At least 32 bytes,
# base64-encoded. Stash it in the team Bitwarden vault under the entry
# "Cloudflare — heart unyt-tunnel tunnel secret" *before* pasting it
# into Pulumi config so the secret is recoverable if the Pulumi backend
# is ever lost. Pulumi config stores it encrypted on the cloud backend,
# but only authorised stack readers can decrypt it — Bitwarden is the
# break-glass copy.
pulumi config set --secret heart:cloudflare-tunnel-secret \
    "$(openssl rand -base64 32)"

# Optional: operator allowlist for the DO firewall (defer until after
# fleet rollout is verified to avoid lockout risk on a typo).
# pulumi config set heart:operator-cidrs "203.0.113.7/32,198.51.100.0/24"

# 2. Preview — there should be no `import` rows, just three additions:
#    + cloudflare:zeroTrustTunnelCloudflared::unyt-tunnel
#    + cloudflare:zeroTrustTunnelCloudflaredConfig::unyt-tunnel-ingress
#    + cloudflare:dnsRecord::unyt-tunnel-dns
#    (plus any firewall + droplet additions/changes).
pulumi preview

# 3. Apply.
pulumi up
```

After `pulumi up`:

```bash
# Verify CF state.
TUNNEL_ID=$(pulumi stack output tunnelId)
echo "New tunnel id: ${TUNNEL_ID}"

# Verify DNS.
dig +short unyt-tunnel.unyt.co | head -3
# Expected: <tunnel-id>.cfargotunnel.com

# Verify the tunnel exists but has 0 connectors (no droplet has cloudflared yet).
curl -sI https://unyt-tunnel.unyt.co/health
# Expected: HTTP 530 (no connector — that's correct at this stage).

# Legacy path still healthy.
curl -sI https://lisa-tunnel.unyt.co/health
# Expected: HTTP 200.
```

## Droplet rollout

Once Pulumi has provisioned the CF-side resources, individual droplets
roll out their `cloudflared` connector. The first droplet typically takes ~2 minutes from
SSH to "Registered tunnel connection" in `journalctl -u cloudflared`.

After the first droplet's connector is registered:

```bash
# Bypass DNS to confirm the droplet is actually serving the tunnel.
curl -sI -H 'Host: unyt-tunnel.unyt.co' \
    --resolve unyt-tunnel.unyt.co:443:<droplet-IP> \
    https://unyt-tunnel.unyt.co/health
# Expected: HTTP 200.

# And via the public path (which goes through CF edge → tunnel pool).
curl -sI https://unyt-tunnel.unyt.co/health
# Expected: HTTP 200.
```

## Proxy worker cutover

When `unyt-tunnel.unyt.co/health` returns 200 from the droplet
connector(s), it's safe to flip the proxy worker:

```bash
cd hash-explorer/workers
# Edit wrangler.proxy.toml: LOCAL_SERVER_URL = "https://unyt-tunnel.unyt.co"
# (the legacy value is left as a `#`-prefixed comment for one release
# cycle so the rollback path is obvious.)
bunx wrangler deploy --config wrangler.proxy.toml
```

Smoke test from a fresh terminal (no cached resolution):

```bash
./heart/scripts/smoke-test.sh unyt-tunnel.unyt.co
./heart/scripts/smoke-test.sh unyt-tunnel.unyt.co \
    <dna_hash> <app_id> transactor/get_global_units_details
```

Then verify the SPA at `https://hash-explorer.unyt.co` still loads and
executes a zome call (check the Network tab for any 5xx).

## Legacy `lisa-tunnel` retirement

After 24-48 hours of clean traffic through `unyt-tunnel`:

```bash
# 1. Stop the laptop connector.
pkill -f 'cloudflared tunnel run lisa-tunnel'

# 2. Verify the proxy worker still serves traffic.
curl -sI https://hash-explorer-worker.unyt.co/health
# Expected: HTTP 200 (now via unyt-tunnel tunnel).

# 3. Wait ~1h to confirm nothing else (cron, a forgotten dev client) is
#    hitting lisa-tunnel.unyt.co. CF Analytics dashboard should show
#    zero requests on the legacy tunnel's hostname.

# 4. Delete the legacy tunnel from CF (after archiving its credentials
#    to the team Bitwarden vault as a rollback parachute — the original
#    secret is in ~/.cloudflared/lisa-tunnel.json on the operator
#    laptop that ran `cloudflared tunnel create lisa-tunnel`):
cloudflared tunnel delete lisa-tunnel

# 5. Remove the legacy DNS record (still pointing at the
#    now-deleted tunnel id):
#    Dashboard: Cloudflare → unyt.co zone → DNS → delete the
#    `lisa-tunnel` CNAME (or via API).
```

## Rollback

If the new tunnel misbehaves and we need to revert:

```bash
# 1. Roll back hash-explorer/workers/wrangler.proxy.toml's
#    LOCAL_SERVER_URL to https://lisa-tunnel.unyt.co.
# 2. Re-deploy:
cd hash-explorer/workers
bunx wrangler deploy --config wrangler.proxy.toml

# 3. Confirm the laptop's cloudflared is still running (it should be
#    until the explicit retirement step above):
pgrep -af 'cloudflared tunnel run lisa-tunnel'

# 4. Investigate the new tunnel without time pressure.
```

The DigitalOcean firewall + droplet additions are independent of
Cloudflare, so even in this rollback the conductor stays up.

## Phase 2 cleanup — future enhancements

Items to consider once the public-hostname cutover above has been
boring-and-stable for ~30 days. None of these are required for
correctness — they're follow-on hardening / consolidation work.

### Migrate the worker → tunnel hop to Workers VPC

[Cloudflare Workers VPC](https://developers.cloudflare.com/workers-vpc/)
lets a CF Worker bind directly to a CF Tunnel without going through a
public hostname. For our chain that means
`hash-explorer-worker` could call the `unyt-tunnel` tunnel via a
private binding (`env.GATEWAY_VPC.fetch(...)`) instead of
`fetch("https://unyt-tunnel.unyt.co/...")`. The tunnel itself,
`cloudflared` on each droplet, and `hc-http-gw` all stay the same —
only the worker-to-tunnel hop changes.

Pros worth migrating for:

- Removes `unyt-tunnel.unyt.co` from public DNS once nothing else
  depends on it (only `hash-explorer-worker` does today). Smaller
  public attack surface — the gateway becomes Worker-reachable only.
- Drops one TLS handshake from the request chain. Microsecond-scale,
  not user-visible, but it's free latency.
- Tunnel ingress lives next to the worker config
  (`hash-explorer/workers/wrangler.proxy.toml`) rather than in CF DNS
  state. Slightly cleaner ownership story.

Reasons to wait — and what to track before adopting:

- **Beta API stability.** Workers VPC is currently in Beta
  ([as of 2026-05](https://developers.cloudflare.com/workers-vpc/));
  CF can change binding format or wire compat before GA. Wait for
  the GA announcement before pinning a load-bearing prod path on it.
- **Lose laptop-side debuggability.** Once the public CNAME is
  removed, `curl https://unyt-tunnel.unyt.co/health` no longer works
  from an operator terminal — `scripts/smoke-test.sh` becomes a
  worker-only path. Keep the CNAME for at least 30 days post-migration
  so smoke tests still run, then optionally remove it for the
  attack-surface win.
- **Pricing at GA is unknown.** During Beta, Workers VPC is free on
  all Workers plans (Free + Paid). CF's pattern is to add
  usage-based pricing at GA — budget for some line-item once it
  leaves Beta. Check the
  [Workers VPC pricing page](https://developers.cloudflare.com/workers-vpc/)
  before committing.
- **Workers-only access.** Only Workers / Pages Functions can call a
  VPC service binding. If a future non-Worker caller (CLI, partner
  integration, webhook source) is in scope, keep the public hostname.

Implementation sketch (don't action yet — capture for the future):

1. CF dashboard → **Workers VPC → Services** → **Create VPC Service**.
   Pick the `unyt-tunnel` tunnel, set host `127.0.0.1`, port `8090`,
   service type HTTP.
2. Add a binding to
   [hash-explorer/workers/wrangler.proxy.toml](../../hash-explorer/workers/wrangler.proxy.toml):
   ```toml
   [[vpc_services]]
   binding = "GATEWAY_VPC"
   service_id = "<id-from-dashboard>"
   ```
3. Refactor [hash-explorer/workers/proxy-worker.js](../../hash-explorer/workers/proxy-worker.js)'s
   single `fetch(env.LOCAL_SERVER_URL + path)` site to
   `env.GATEWAY_VPC.fetch(path)`. Keep `LOCAL_SERVER_URL` as a fallback
   for ~one release cycle in case rollback is needed.
4. `bunx wrangler deploy --config wrangler.proxy.toml`.
5. Smoke-test via the SPA (browser network panel) — `curl` from
   laptop will still work as long as the public CNAME exists, but it
   no longer exercises the worker → tunnel hop.
6. After 30 days of clean traffic, optionally remove the
   `unyt-tunnel` CNAME and the `LOCAL_SERVER_URL` env var, leaving
   the VPC binding as the only path.

Rollback is a one-line revert in
`hash-explorer/workers/proxy-worker.js` + `bunx wrangler deploy` —
trivial as long as the public CNAME hasn't been deleted yet.

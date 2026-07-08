# Setup Progenitor

The progenitor is the first agent in a release's network — it **creates** the network rather than joining one. It must be set up **before any service node**, because two of its outputs are required by every other node in the release, both living in the automation repo's `config/release.json`:

- the **network seed** the network is created with → `network_seed`
- the **progenitor agent key** → `properties.progenitor_pubkey`

Until both are filled in with the real progenitor values, the service-node deploys (`make hf-swapper`, etc.) cannot run — a joining agent installs the DNA with that seed and that progenitor key baked into its properties.

## Where the progenitor runs

The progenitor is its **own droplet**, a first-class `progenitor` node type in the fleet (heart `main.go` / `defaults.yaml`, count 1, weekly backup), deployed with the **same agent machinery as every other node** (`automation`'s `deploy.sh`). It is operated headlessly over its server conductor's admin websocket — which every standalone conductor exposes, unlike the packaged desktop app (the direct-mode `android-service-runtime` runs the conductor in-process with no admin socket, so `unyt_cli` cannot drive it; a server conductor is the supported path). The droplet persists for the release's life: the progenitor account is the network's only GlobalDefinition write surface, so later GD work (and any future migration window that makes this release a source) reuses it.

## How "create" differs from "join"

A service node joins the progenitor's network: `deploy.sh` gets a membrane proof from the joining service and installs the `alliance` DNA with `properties.progenitor_pubkey` set to the progenitor's key. The progenitor installs with **no membrane proof, but the same DNA property** — it designates **itself**. `config/progenitor/deploy.json` marks its agent `"progenitor": true`, so the deploy CLI sets `properties.progenitor_pubkey` to the progenitor's own freshly-generated key and applies it to the `alliance` role even on the no-proof path (`packages/unyt-deploy/src/{cli,admin-api}.ts`).

This identical-property requirement is the crux: a cell's DNA hash is derived from its modifiers (network seed **and** properties), so the progenitor and every joiner must install with the **same** `progenitor_pubkey` or they land on different DNAs and never share a network or GlobalDefinition. Installing the progenitor with no property (letting it fall back to the "no designation ⇒ default progenitor" path in `unyt/src-tauri/src/runtime/boot/progenitor.rs`) is **not** safe for the release DNA — that relaxed behaviour is testing-only, and it produces a different hash from the joiners. The empty `joining_service.url` in the config is what selects the no-membrane-proof create path.

## Procedure

Run in order. Steps 1–5 stand up the progenitor and record its outputs; step 6 configures the Holo Hosting network once the metered agents exist.

1. **Prepare `release.json`** (automation repo) for the new release **before** running the progenitor:
   - `release_version` → the dotted label, e.g. `v0.93.0`
   - `happ_url_template` → the release's `unyt.happ` download URL
   - `network_seed` → a **fresh** value (membrane proofs are seed-scoped per release; never reuse a prior release's seed)
   - `predecessor_release` → `""` for a plain release (no migration window)
   - `properties.progenitor_pubkey` → its current value doesn't affect the progenitor's own install (the `"progenitor": true` create path overrides it with the progenitor's freshly-generated key); it is **set to that reported key at step 4** so the joiners install the identical property.
2. **Provision the fleet** (heart): `make up` creates every droplet including `progenitor-<release>-1`, and writes its IP into `releases/<release>/ips.json` under the key `progenitor-1`.
3. **Create the network** (automation): `make progenitor`. This resets the droplet, brings up lair, generates the progenitor agent key, and installs the `alliance` DNA with the fresh `network_seed` and `properties.progenitor_pubkey` set to **that generated key** (no membrane proof) — creating the network with the agent as its progenitor. The deploy result (`config/progenitor/results/deploy-result.json`) reports the agent key.
4. **Record the progenitor key for the joiners**: set `release.json` `properties.progenitor_pubkey` to the agent key from step 3. Every joining node then installs the `alliance` DNA with this same property, so its cell hash matches the progenitor's — the equality that lets it see the progenitor's network and GlobalDefinition. A mismatch here is the classic silent failure: joiners land on a different DNA and never converge.
5. **Deploy the service nodes** in dependency order (see [`deploy-new-release.md`](./deploy-new-release.md) § Bring nodes into service): `make blockchain-bridging` and `make hf-swapper` first (their agent keys feed the agreements), then the rest.
6. **Configure the Holo Hosting network**: once blockchain-bridging + hf-swapper are up, run `unyt_cli progenitor holo-hosting setup` against the progenitor node (its conductor admin/app ports; `app_id` defaults to `unyt-progenitor`, matching `config/progenitor/deploy.json`). Setup builds the GlobalDefinition, two lanes, six units, and five agreements in one shot, and refuses to run if the network is already configured (`crates/unyt_cli/src/actions/holo_hosting.rs` — not resumable). It is parameterised by the connected progenitor plus six role keys supplied as flags (hf-swapper, HOT bridging agent, WindTunnel admin, pricing oracle, fee collector, oracle) — run `unyt_cli progenitor holo-hosting setup --help` for the exact flag names, and pass the keys gathered from the deployed agents' `deploy-result.json` files. Verify with `unyt_cli progenitor global get`.

## Infrastructure

The progenitor droplet is provisioned like any other node — see the [README](../README.md) and the [Always-On Node guide](./setup-always-on-node.md). The only difference is the create-not-join install above.

# Setup Progenitor

The progenitor is the first agent in a release's network — it **creates** the network rather than joining one. It must be set up **before any service node**, because two of its outputs are required by every other node in the release, both living in the automation repo's `config/release.json`:

- the **network seed** the network is created with → `network_seed`
- the **progenitor agent key** → `properties.progenitor_pubkey`

Until both are filled in with the real progenitor values, the service-node deploys (`make hf-swapper`, etc.) cannot run — a joining agent installs the DNA with that seed and that progenitor key baked into its properties.

## Where the progenitor runs

The progenitor is its **own droplet**, a first-class `progenitor` node type in the fleet (heart `main.go` / `defaults.yaml`, count 1, weekly backup), deployed with the **same agent machinery as every other node** (`automation`'s `deploy.sh`). It is operated headlessly over its server conductor's admin websocket — which every standalone conductor exposes, unlike the packaged desktop app (the direct-mode `android-service-runtime` runs the conductor in-process with no admin socket, so `unyt_cli` cannot drive it; a server conductor is the supported path). The droplet persists for the release's life: the progenitor account is the network's only GlobalDefinition write surface, so later GD work (and any future migration window that makes this release a source) reuses it.

## How "create" differs from "join"

A service node joins the progenitor's network: `deploy.sh` gets a membrane proof from the joining service and installs the DNA with `properties.progenitor_pubkey` set to the progenitor's key. The progenitor instead installs with **no membrane proof and no `progenitor_pubkey`** — the deploy CLI skips the join whenever the config's `joining_service.url` is empty (`packages/unyt-deploy/src/cli.ts`), and an agent that installs a fresh network with no `progenitor_pubkey` designated **is** the progenitor by default (`unyt/src-tauri/src/runtime/boot/progenitor.rs` — no designation ⇒ true). `config/progenitor/deploy.json` carries the empty `joining_service.url` that selects this path.

## Procedure

Run in order. Steps 1–5 stand up the progenitor and record its outputs; step 6 configures the Holo Hosting network once the metered agents exist.

1. **Prepare `release.json`** (automation repo) for the new release **before** running the progenitor:
   - `release_version` → the dotted label, e.g. `v0.93.0`
   - `happ_url_template` → the release's `unyt.happ` download URL
   - `network_seed` → a **fresh** value (membrane proofs are seed-scoped per release; never reuse a prior release's seed)
   - `predecessor_release` → `""` for a plain release (no migration window)
   - `properties.progenitor_pubkey` → **remove / leave empty.** This is critical: if a stale key is present, the progenitor installs with a designation that does not match its own key and is *not* recognised as progenitor. It is filled in at step 4.
2. **Provision the fleet** (heart): `make up` creates every droplet including `progenitor-<release>-1`, and writes its IP into `releases/<release>/ips.json` under the key `progenitor-1`.
3. **Create the network** (automation): `make progenitor`. This resets the droplet, brings up lair, generates the progenitor agent key, and installs the happ with the fresh `network_seed` and no membrane proof — creating the network with that agent as its default progenitor. The deploy result (`config/progenitor/results/deploy-result.json`) reports the agent key.
4. **Record the progenitor key**: set `release.json` `properties.progenitor_pubkey` to the agent key from step 3. Every joining node now installs against this progenitor.
5. **Deploy the service nodes** in dependency order (see [`deploy-new-release.md`](./deploy-new-release.md) § Bring nodes into service): `make blockchain-bridging` and `make hf-swapper` first (their agent keys feed the agreements), then the rest.
6. **Configure the Holo Hosting network**: once blockchain-bridging + hf-swapper are up, run `unyt_cli progenitor holo-hosting setup` against the progenitor node (its conductor admin/app ports; `app_id` defaults to `unyt-progenitor`, matching `config/progenitor/deploy.json`). Setup builds the GlobalDefinition, two lanes, six units, and five agreements in one shot, and refuses to run if the network is already configured (`crates/unyt_cli/src/actions/holo_hosting.rs` — not resumable). It is parameterised by the connected progenitor plus six role keys supplied as flags (hf-swapper, HOT bridging agent, WindTunnel admin, pricing oracle, fee collector, oracle) — run `unyt_cli progenitor holo-hosting setup --help` for the exact flag names, and pass the keys gathered from the deployed agents' `deploy-result.json` files. Verify with `unyt_cli progenitor global get`.

## Infrastructure

The progenitor droplet is provisioned like any other node — see the [README](../README.md) and the [Always-On Node guide](./setup-always-on-node.md). The only difference is the create-not-join install above.

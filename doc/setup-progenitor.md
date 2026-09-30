# Setup Progenitor

The progenitor is the first agent in a release's network — it **creates** the network rather than joining one. It must be set up **before any service node**, because two of its outputs are required by every other node in the release, both living in the automation repo's `config/release.json`:

- the **network seed** the network is created with → `network_seed`
- the **progenitor agent key** → `properties.progenitor_pubkey`

Until both are filled in with the real progenitor values, the service-node installs (`make install ROLE=hf-swapper`, etc.) cannot run: a joining agent installs the DNA with that seed and that progenitor key baked into its properties.

## Where the progenitor runs

The progenitor is its **own droplet**, a first-class `progenitor` node type in the fleet (heart `main.go` / `defaults.yaml`, count 1, weekly backup), deployed with the **same agent machinery as every other node** (`automation`'s `deploy.sh`). It is operated headlessly over its server conductor's admin websocket — which every standalone conductor exposes, unlike the packaged desktop app (the direct-mode `android-service-runtime` runs the conductor in-process with no admin socket, so `unyt_cli` cannot drive it; a server conductor is the supported path). The droplet persists for the release's life: the progenitor account is the network's only GlobalDefinition write surface, so later GD work (and any future migration window that makes this release a source) reuses it.

## How "create" differs from "join"

A service node joins the progenitor's network: `deploy.sh` gets a membrane proof from the joining service and installs the `alliance` DNA with `properties.progenitor_pubkey` set to the progenitor's key. The progenitor installs with **no membrane proof, but the same DNA property** — it designates **itself**. `config/progenitor/deploy.json` marks its agent `"progenitor": true`, so the deploy CLI sets `properties.progenitor_pubkey` to the progenitor's own freshly-generated key and applies it to the `alliance` role even on the no-proof path (`packages/unyt-deploy/src/{cli,admin-api}.ts`).

This identical-property requirement is the crux: a cell's DNA hash is derived from its modifiers (network seed **and** properties), so the progenitor and every joiner must install with the **same** `progenitor_pubkey` or they land on different DNAs and never share a network or GlobalDefinition. Installing the progenitor with no property (letting it fall back to the "no designation ⇒ default progenitor" path in `unyt/src-tauri/src/runtime/boot/progenitor.rs`) is **not** safe for the release DNA — that relaxed behaviour is testing-only, and it produces a different hash from the joiners. The empty `joining_service.url` in the config is what selects the no-membrane-proof create path.

## Procedure

Run in order. Steps 1 to 5 stand up the progenitor and record its outputs; step 6 configures the Holo Hosting network once the metered agents' keys are minted, before they install, and step 7 extends its settings.

1. **Prepare `release.json`** (automation repo) for the new release **before** running the progenitor:
   - `release_version` → the dotted label, e.g. `v0.93.0`
   - `happ_url_template` → the release's `unyt.happ` download URL
   - `network_seed` → a **fresh** value (membrane proofs are seed-scoped per release; never reuse a prior release's seed)
   - `predecessor_release` → `""` for a plain release (no migration window)
   - `properties.progenitor_pubkey` → **set at step 4** to the key step 3 mints, before `make progenitor` installs anything, so the progenitor and every joiner install the identical property.
2. **Provision the fleet** (heart): `make up` creates every droplet including `progenitor-<release>-1`, and writes its IP into `releases/<release>/ips.json` under the key `progenitor-1`.
3. **Mint the progenitor key** (automation): `make progenitor-genkey` resets the droplet, generates the progenitor agent key, saves it to `config/progenitor/results/agent-keys.txt` and prints it, then stops. `make standup` runs the automation targets of steps 3 to 6 in one batch with the service nodes, in the order [`deploy-new-release.md`](./deploy-new-release.md) § Bring nodes into service lists. In production step 4's edit and step 5's `make redeploy-joining-service` fall outside the batch, so there it runs stage by stage.
4. **Record the progenitor key for the joiners**: set `release.json` `properties.progenitor_pubkey` to the agent key from step 3. Every joining node then installs the `alliance` DNA with this same property, so its cell hash matches the progenitor's: the equality that lets it see the progenitor's network and GlobalDefinition. `make progenitor` checks it and stops with `Progenitor key mismatch` while `release.json` names a different key.
5. **Create the network** (automation): in production, `make redeploy-joining-service` first points `joining.unyt.dev`'s default network at this release's DNA. Then `make progenitor` installs the `alliance` DNA with the step 3 key and the fresh `network_seed`, creating the network with that agent as its progenitor. The deploy result (`config/progenitor/results/deploy-result.json`) reports the agent key.
6. **Configure the Holo Hosting network**: `make holo-hosting-setup` runs `unyt_cli progenitor holo-hosting setup` on the progenitor, before any service node installs. Setup builds the GlobalDefinition, two lanes, six units, and five agreements in one shot, and refuses to run if the network is already configured (`crates/unyt_cli/src/actions/holo_hosting.rs`; not resumable). It resolves the six role keys (hf-swapper, HOT bridging agent, WindTunnel admin, pricing oracle, fee collector, oracle) per `config/progenitor/holo-hosting.json`, taking the service nodes' keys from their `make genkey` output, then reads the result back with `unyt_cli progenitor global get`.
7. **Extend the network's settings, the standup's last step**, once the rest of the batch has installed the service nodes and the notary, and `make gd-migration-config` has applied the chain root's migration config: `unyt_cli progenitor extend-windows` on the progenitor node, as [`deploy-new-release.md`](./deploy-new-release.md) § Bring nodes into service, step 4, gives it.

## Infrastructure

The progenitor droplet is provisioned like any other node — see the [README](../README.md) and the [Always-On Node guide](./setup-always-on-node.md). The only difference is the create-not-join install above.

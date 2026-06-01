# Setup Progenitor

The progenitor is the first node in the network — it creates the network rather than joining an existing one. **It must be set up before any service node**, because two of its outputs are required by every other node in the release:

- the **progenitor agent key** — becomes `properties.progenitor_pubkey` in the automation repo's `config/release.json`
- the **network seed** used to create the network — becomes `network_seed` in `config/release.json`

Until both are filled into `release.json` with the real progenitor values, the per-node service deploys (`make always-online-1`, etc.) cannot run.

## Current process (manual)

Progenitor setup is not yet automated. Today it is done by hand:

1. Install the progenitor build manually (the `.deb` package).
2. Bring up a running agent on that node.
3. Read back the **agent key** and the **network seed** it was created with.
4. Put those two values into `automation/config/release.json` (`properties.progenitor_pubkey` and `network_seed`).

> TODO: document the exact `.deb` install command, where the agent key and network seed are printed/stored, and the agent-startup steps. Automating this is future work.

## Infrastructure

The progenitor droplet itself is provisioned like any other node — see the [README](../README.md) and the [Always-On Node guide](./setup-always-on-node.md). The only difference is the post-boot step above: you initialise it as the progenitor instead of joining an existing network.

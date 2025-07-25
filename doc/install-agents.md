# Setup Agents

> **Note:** For a complete guide on setting up an always-on node including agent setup, see [Setup an Always-On Node](./setup-always-on-node.md).

Once you have a server running, we can start talking about installing an agent. This document provides additional examples for specific agent installation scenarios.

## Setting up a basic node

```bash
# /bin/bash

mkdir /var/lib/holochain/apps/

# downloading the app to be installed
curl -L -o /var/lib/holochain/apps/domino.happ <https://url.happ>

hc sandbox call --running 8800 install-app /var/lib/holochain/apps/domino.happ
```

## Setting up an agent with a pre-existing public key

```bash
# /bin/bash

# create a new agent
hc sandbox call --running 8800 new-agent

```

Output:

```bash
root@heart-node:~# hc sandbox call --running 8800 new-agent
hc-sandbox: Added agent uhCAkN5IokFxdryZWUzR6Nb89wjVsiENaXp8uGsKbGJpT1SKxPzEm
```

## Importing an agent key into lair

For importing keys generated with `hc_seed_bundle_cli`, see the [Always-On Node Setup guide](./setup-always-on-node.md#option-3-import-from-hc_seed_bundle_cli-most-control).

## Setting up an agent with a pre-existing public key

```bash
# /bin/bash

# install the app
hc sandbox call --running 8800 install-app --app-id "domino-progenitor" --agent-key uhCAkN5IokFxdryZWUzR6Nb89wjVsiENaXp8uGsKbGJpT1SKxPzEm /var/lib/holochain/apps/domino.happ

# Verify the app is installed
hc sandbox call --running 8800 list-apps
```

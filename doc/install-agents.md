# Setup Agents

Once you have a server running, we can start talking about installing an agent.

- Setting up a basic node

```bash
# /bin/bash

# downloading the app to be installed
curl -L -o /var/lib/holochain/apps/domino.happ <https://url.happ>

hc sandbox call --running 8800 install-app /var/lib/holochain/apps/domino.happ
```

- Setting up an agent with a pre-existing public key

```bash
# /bin/bash

# create a new agent
hc sandbox call --running 8800 new-agent

```

Output:

```bash
root@heart-node:~# hc sandbox call --running 8800 new-agent
hc-sandbox: Added agent uhCAkmwZqe275HZtZ-a0praX8zHKTNwJSzqFKQAd5XSTkUou5d1IT
```

- Setting up an agent with a pre-existing public key

```bash
# /bin/bash

# install the app
hc sandbox call --running 8800 install-app --app-id <progenitor-domino-app> --agent-key uhCAkmwZqe275HZtZ-a0praX8zHKTNwJSzqFKQAd5XSTkUou5d1IT /var/lib/holochain/apps/domino.happ

# Verify the app is installed
hc sandbox call --running 8800 list-apps
```

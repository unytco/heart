# Setup Agents

Once you have a server running, we can start talking about installing an agent.

- Setting up a basic node

```bash
# /bin/bash

# downloading the app to be installed
curl -L -o /var/lib/holochain/apps/piecework.happ <https://url.happ>

hc sandbox call --running 9000 install-app /var/lib/holochain/apps/piecework.happ
```

- Setting up an agent with a pre-existing public key

```bash
# /bin/bash

# create a new agent
hc sandbox call --running 9000 new-agent

```

Output:

```bash
root@heart-node:~# hc sandbox call --running 9000 new-agent
hc-sandbox: Added agent uhCAkmwZqe275HZtZ-a0praX8zHKTNwJSzqFKQAd5XSTkUou5d1IT
```

- Setting up an agent with a pre-existing public key

```bash
# /bin/bash

# install the app
hc sandbox call --running 9000 install-app /var/lib/holochain/apps/piecework.happ --agent-key uhCAkmwZqe275HZtZ-a0praX8zHKTNwJSzqFKQAd5XSTkUou5d1IT
```

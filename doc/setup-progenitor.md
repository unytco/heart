# How to set up a progenitor?

> **Note:** For a complete step-by-step guide, see [Setup an Always-On Node](./setup-always-on-node.md) which covers the full process including progenitor setup.

- Use this repo's Pulumi program to deploy a server

  - This server will be an Ubuntu 22.04 server provisioned via cloud-init.
  - It will be running a version of Holochain and Lair Keystore baked into the cloud-config.
  - Review the [Always-On Node Setup](./setup-always-on-node.md) to see how to deploy the server.

- Then you need to plan out what kind of agent you are planning to run on this node.
  - If it's a progenitor node you will have to generate an agent key first.
  - Build the app with the progenitor node in the DNA properties.
  - Then review the [Always-On Node Setup](./setup-always-on-node.md) or [Install Agents](./install-agents.md) to see how to install the app on the node.
- If you are setting up a child node you will have to install the app on the node first.

  - Review the [Always-On Node Setup](./setup-always-on-node.md) or [Install Agents](./install-agents.md) to see how to install the app on the node.

- Use the domino_cli to interact with the holochain agent.

# How to set up a progenitor?

- Use this repos terraform module to create a server

  - This server will be a ubuntu 22.04 server.
  - It will be running a version of Holochain and Lair Keystore specified in the .env file
  - A peicework_cli will be installed in the ubuntu to interact with the peicework app.
  - Review the readme to see how to deploy the server

- Then you need to plan out what kind of agent you are planning to run on this node.
  - if its a progenitor node you will have to generate a agent key first.
  - build the app with the progenitor node in the dna properties.
  - then review [this doc](./setup-agents.md) to see how to install the app on the node.
- If you are setting up a child node you will have to install the app on the node first.

  - review [this doc](./setup-agents.md) to see how to install the app on the node.

- Use the peicework_cli to interact with the holochain agent.

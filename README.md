# HEART

> **Notice:** This project is a work in progress and not all features are available yet.
> Please test out the setup if you use this setup

**H**olochain **E**nvironment & **A**gent **R**untime **T**oolkit

A toolkit for quickly setting up and managing Holochain nodes. HEART provides automated setup, configuration, and testing for Holochain environments.

## Overview

HEART is a toolkit for quickly setting up and managing Holochain nodes. It provides automated setup, configuration, and testing for Holochain environments.

## Features

- A Terraform module for deploying Holochain nodes to DigitalOcean
  - This node is a ubuntu 22.04 server running Holonix
  - Pre-configured to run a specified version of Holochain and Lair Keystore
  - use can use a hc to install the apps you want to run

## Documentation

- [Setup A DO that is running a Holochain conductor](./doc/setup-do-holochain-server.md)
- [Setup Progenitor](./doc/setup-progenitor.md)
- [Install Agents](./doc/install-agents.md)

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and testing instructions.

## Roadmap

- [x] Basic NixOS setup with Holonix
- [x] Version-specific Holochain installations
- [x] Automated testing environment
- [ ] Piecework app installation
- [ ] Agent key management
- [ ] App version management
- [ ] Monitoring setup
- [ ] Backup procedures

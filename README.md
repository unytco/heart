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

- **[Setup an Always-On Node](./doc/setup-always-on-node.md)** - Complete guide for setting up a production-ready Holochain node
- [Setup A DO that is running a Holochain conductor](./doc/setup-do-holochain-server.md) - Technical details for server setup
- [Setup Progenitor](./doc/setup-progenitor.md) - Setting up progenitor nodes specifically
- [Install Agents](./doc/install-agents.md) - Additional agent installation examples

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and testing instructions.

## Roadmap

- [x] Basic Ubuntu setup with Holochain
- [x] Version-specific Holochain installations
- [x] Automated testing environment
- [x] Comprehensive setup documentation
- [x] Agent key management documentation
- [ ] Piecework app installation automation
- [ ] App version management
- [ ] Monitoring setup
- [ ] Backup procedures
- [ ] Snapshot-based rapid deployment

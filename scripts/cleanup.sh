#!/usr/bin/env bash
set -ex

# Stop any running Vagrant processes
pkill -f "vagrant" || true

# Clean up VirtualBox VMs
VBoxManage list runningvms | cut -d'"' -f2 | xargs -r -I {} VBoxManage controlvm {} poweroff
VBoxManage list vms | cut -d'"' -f2 | xargs -r -I {} VBoxManage unregistervm {} --delete

# Clean up port forwarding
for i in $(seq 2200 2300); do
    netstat -tln | grep ":$i " && fuser -k "$i/tcp" || true
done

# Clean up Vagrant metadata
rm -rf .vagrant/ test/

# Clean up any stale lock files
rm -f test/.vagrant/*.lock 
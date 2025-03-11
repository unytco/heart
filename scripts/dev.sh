#!/bin/bash
set -e

# Get the directory containing this script
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"
TEST_DIR="$PROJECT_ROOT/test"

# Function to sync and rebuild
sync_and_rebuild() {
    echo "Syncing changes..."
    cd "$TEST_DIR" && vagrant rsync
    echo "Rebuilding NixOS..."
    vagrant ssh -c "sudo nixos-rebuild switch"
    echo "Running tests..."
    vagrant ssh -c "bash /tmp/test.sh"
}

# Ensure we're in a vagrant environment or create it
if [ ! -d "$TEST_DIR/.vagrant" ]; then
    echo "No vagrant environment found. Running dev-init..."
    cd "$PROJECT_ROOT"
    make dev-init
else
    # Check if VM is running
    if ! (cd "$TEST_DIR" && vagrant status | grep "running" > /dev/null); then
        echo "VM not running. Starting it..."
        cd "$TEST_DIR" && vagrant up
    fi
fi

# Initial sync and build
sync_and_rebuild

# Watch for changes in nixos directory and tests
if command -v fswatch > /dev/null; then
    echo "Watching for changes in nixos directory and tests..."
    fswatch -o "$PROJECT_ROOT/nixos" "$PROJECT_ROOT/scripts/test.sh" | while read f; do
        sync_and_rebuild
    done
else
    echo "fswatch not found. Install it for automatic rebuilds on changes."
    echo "Available commands:"
    echo "  make dev-rebuild  - Sync changes and rebuild"
    echo "  make dev-test    - Run tests"
    echo "  make dev-shell   - SSH into VM"
    echo "  make dev-init    - Clean and recreate test environment"
fi 
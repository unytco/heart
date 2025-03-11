#!/usr/bin/env bash
set -e

# Get the directory containing this script
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"

# Create test directory
TEST_DIR="$PROJECT_ROOT/test"
echo "📁 Creating test directory at $TEST_DIR"
mkdir -p "$TEST_DIR"

# Copy scripts to test directory
cp "$SCRIPT_DIR/setup.sh" "$TEST_DIR/"
cp "$SCRIPT_DIR/test.sh" "$TEST_DIR/"

# Generate Vagrantfile
echo "📝 Generating Vagrantfile..."
cat > "$TEST_DIR/Vagrantfile" << EOF
Vagrant.configure("2") do |config|
  config.vm.box = "ubuntu/jammy64"
  config.vm.hostname = "hi-test"
  
  config.vm.provider "virtualbox" do |vb|
    vb.memory = "4096"
    vb.cpus = 4
  end

  config.vm.synced_folder "$PROJECT_ROOT", "/vagrant", type: "rsync",
    rsync__args: ["--verbose", "--archive", "--delete", "-z", "--chmod=755"]

  # Run setup
  config.vm.provision "shell", path: "setup.sh", privileged: true

  # Run tests
  config.vm.provision "shell", path: "test.sh", privileged: true
end
EOF

# Start VM and provision
echo "🖥️  Starting VM and provisioning..."
cd "$TEST_DIR"
vagrant up

echo "✅ Testing completed! You can SSH into the VM with 'vagrant ssh'" 
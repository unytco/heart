# Contributing to HEART

Thank you for your interest in contributing to HEART (Holochain Environment & Agent Runtime Toolkit)!

## Development Setup

### Prerequisites

- [VirtualBox](https://www.virtualbox.org/)
  - Host system needs at least:
    - 16GB RAM (VM will use 12GB)
    - 6+ CPU cores
    - 30GB+ available disk space
- [Vagrant](https://www.vagrantup.com/)
- Bash shell
- High-speed internet connection

> **Note:** The test environment requires significant system resources to compile and run Holochain. If your system doesn't meet these requirements, the build process may fail or be very slow.

### Testing Locally

1. Clone the repository:

   ```bash
   git clone https://github.com/your-org/hi.git
   cd hi
   ```

2. Run the local test environment:

   ```bash
   ./scripts/test_locally.sh
   ```

   > **Note:** The first run may take 10-15 minutes while downloading the NixOS image and dependencies. Subsequent runs will be faster as they use cached resources.

   This will:

   - Create a test VM using Vagrant
   - Install NixOS
   - Set up the Holonix environment
   - Run the test suite

3. SSH into the test environment:

   ```bash
   cd test
   vagrant ssh
   ```

4. Clean up after testing:
   ```bash
   vagrant destroy -f
   cd ..
   rm -rf test
   ```

### Running Tests

The test suite (`scripts/test.sh`) verifies:

- Nix environment setup
- Holochain tools availability
- Directory structure
- Configuration files

### Making Changes

1. Create a new branch:

   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes
3. Test locally using the steps above
4. Submit a pull request

## Project Structure

```
.
├── scripts/
│   ├── setup.sh      # Main setup script
│   ├── test.sh       # Test suite
│   └── test_locally.sh # Local testing script
├── terraform/
│   ├── main.tf       # Terraform configuration
│   └── variables.tf  # Terraform variables
├── README.md
└── CONTRIBUTING.md
```

## Code Style

- Use shellcheck for shell scripts
- Follow HashiCorp's style guide for Terraform
- Keep scripts idempotent
- Add comments for complex operations

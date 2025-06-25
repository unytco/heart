# Variables
TEST_DIR := test
TERRAFORM_DIR := terraform

# Load environment variables if .env exists
ifneq (,$(wildcard .env))
    include .env
    export
endif

# Terraform var arguments
TF_VARS = \
	-var="do_token=$(DO_TOKEN)" \
	-var="ssh_key_id=$(SSH_KEY_ID)" \
	-var="node_name=$(NODE_NAME)" \
	-var="holochain_version=$(HOLOCHAIN_VERSION)" \
	-var="lair_version=$(LAIR_VERSION)" \
	-var="droplet_size=$(DROPLET_SIZE)" \
	-var="region=$(REGION)" \
	-var="lair_password=$(LAIR_PASSWORD)" \
	-var="holochain_password=$(HOLOCHAIN_PASSWORD)"

deploy:
	cd $(TERRAFORM_DIR) && terraform apply $(TF_VARS)

redeploy:
	@# First ensure terraform is up-to-date
	cd $(TERRAFORM_DIR) && terraform refresh $(TF_VARS)
	@# Get the IP address
	@IP=$$(cd $(TERRAFORM_DIR) && terraform show -json | jq -r '.values.root_module.resources[] | select(.type == "digitalocean_droplet") | .values.ipv4_address') && \
	if [ -z "$$IP" ]; then \
		echo "Error: Could not get droplet IP address" >&2; \
		exit 1; \
	fi && \
	echo "Redeploying to $$IP..." && \
	rsync -av scripts/ root@$$IP:/tmp/scripts/ && \
	rsync -av services/ root@$$IP:/tmp/services/ && \
	ssh root@$$IP \
		"HOLOCHAIN_VERSION='$(HOLOCHAIN_VERSION)' \
		LAIR_VERSION='$(LAIR_VERSION)' \
		LAIR_PASSWORD='$(LAIR_PASSWORD)' \
		HOLOCHAIN_PASSWORD='$(HOLOCHAIN_PASSWORD)' \
		bash /tmp/scripts/setup.sh"

test:
	./scripts/test_locally.sh

# Development targets
dev-init: clean-test test    # Clean and create new test environment

dev-rebuild:                 # Sync changes and rebuild
	cd $(TEST_DIR) && vagrant rsync
	cd $(TEST_DIR) && vagrant ssh -c "sudo nixos-rebuild switch"

dev-test:                   # Run tests
	cd $(TEST_DIR) && vagrant rsync
	cd $(TEST_DIR) && vagrant ssh -c "bash /vagrant/scripts/test.sh"

dev-shell:                  # SSH into VM
	cd $(TEST_DIR) && vagrant ssh

dev-watch:                  # Watch for changes and auto-rebuild
	./scripts/dev.sh

clean-test:
	@echo "Running full cleanup..."
	sudo ./scripts/cleanup.sh

.PHONY: deploy redeploy test dev-init dev-rebuild dev-test dev-shell dev-watch clean-test

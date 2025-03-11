# Variables
TEST_DIR := test

deploy:
	cd terraform && terraform apply

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

clean-test:                 # Clean up test environment
	@echo "🧹 Cleaning up test environment..."
	-vagrant global-status --prune
	-pkill -f "vagrant"
	-VBoxManage list runningvms | grep hi-test | cut -d'"' -f2 | xargs -r -I {} VBoxManage controlvm {} poweroff
	-VBoxManage list vms | grep hi-test | cut -d'"' -f2 | xargs -r -I {} VBoxManage unregistervm {} --delete
	rm -rf $(TEST_DIR)
	@echo "✨ Cleanup complete"

cleanup:
	@echo "Running full cleanup..."
	sudo ./scripts/cleanup.sh

.PHONY: deploy test dev-init dev-rebuild dev-test dev-shell dev-watch clean-test

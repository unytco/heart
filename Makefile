# heart — deploy helpers (Pulumi + DigitalOcean)
#
# One Pulumi stack per unyt release. Run these inside the dev shell so pulumi
# and go are on PATH:  nix develop
#
# Target a specific stack without `pulumi stack select` by passing STACK=:
#   make preview STACK=v0-7-0
#
# Full walkthrough: doc/deploy-new-release.md

STACK ?=
PULUMI_STACK := $(if $(STACK),--stack $(STACK),)

.DEFAULT_GOAL := help

help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

## --- verify (no cloud calls) ---

build: ## Compile the Pulumi program
	go build ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format the Pulumi program
	go fmt ./...

## --- per-release deploy ---

new-release: ## Init a new release stack: make new-release RELEASE=v0-7-0
	@test -n "$(RELEASE)" || { echo "RELEASE is required, e.g. make new-release RELEASE=v0-7-0" >&2; exit 1; }
	pulumi stack init $(RELEASE)
	pulumi stack select $(RELEASE)
	pulumi config set heart:release $(RELEASE)
	@echo
	@echo "Stack '$(RELEASE)' created and selected. Now set the required values:"
	@echo "  pulumi config set --secret digitalocean:token <token>"
	@echo "  pulumi config set --secret heart:influx-token  <token>"
	@echo "  pulumi config set heart:project-name  Holo"
	@echo "  pulumi config set heart:influx-bucket unyt-$(RELEASE)   # bucket must exist in InfluxDB"
	@echo "Optional overrides (versions, endpoints, sizes, counts): see Pulumi.release.yaml.example"
	@echo "Then:  make preview  &&  make up"

preview: ## Preview changes for the selected stack (or STACK=...)
	pulumi preview $(PULUMI_STACK)

up: ## Deploy the selected stack (or STACK=...)
	pulumi up $(PULUMI_STACK)

refresh: ## Reconcile state with real infrastructure (or STACK=...)
	pulumi refresh $(PULUMI_STACK)

destroy: ## Tear down the selected stack (or STACK=...)
	pulumi destroy $(PULUMI_STACK)

## --- inspect ---

config: ## Show config for the selected stack (or STACK=...)
	pulumi config $(PULUMI_STACK)

stacks: ## List all stacks
	pulumi stack ls

current: ## Show the currently selected stack and its resources
	pulumi stack $(PULUMI_STACK)

.PHONY: help build vet fmt new-release preview up refresh destroy config stacks current

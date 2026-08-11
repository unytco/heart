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

# The Pulumi organization release stacks are created under. Every stack name is
# org-qualified, so a machine whose `pulumi org get-default` is unset or personal cannot
# silently create the fleet in an individual account where the rest of the team can't see
# or manage it. Override only to target a different org: make new-release PULUMI_ORG=...
PULUMI_ORG ?= unyt

.DEFAULT_GOAL := help

help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

## --- verify (no cloud calls) ---

build: ## Compile the Pulumi program
	go build ./...

test: ## Run the tests
	go test ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format the Pulumi program
	go fmt ./...

## --- per-release deploy ---

# Init a per-release Pulumi stack. The whole recipe is one shell that loads heart/.env
# once and validates RELEASE + both tokens BEFORE any Pulumi state is created, so a bad
# input never leaves a half-configured stack. RELEASE flows through the environment
# (RELEASE_ENV) and is only ever quoted, so metacharacters can't reach a command
# boundary. .env is sourced without `set -a`, so the tokens aren't exported to Pulumi's
# child processes; they reach the stack only via `printf '%s'` over stdin (no trailing
# newline — one corrupts the rendered cloud-init) and are never echoed. The stack is
# created and every config write pinned as the org-qualified "$ORG/$REL", so neither an
# inherited PULUMI_STACK nor an unset/personal `pulumi org get-default` can redirect the
# stack off the release — or off the organization.
new-release: export RELEASE_ENV := $(RELEASE)
new-release: ## Init a new release stack, config from heart/.env: make new-release RELEASE=v0-7-0
	@set -e; \
	REL="$$RELEASE_ENV"; \
	test -n "$$REL" || { echo "RELEASE is required, e.g. make new-release RELEASE=v0-7-0" >&2; exit 1; }; \
	case "$$REL" in ''|*[!a-zA-Z0-9_-]*) echo "RELEASE must match ^[a-zA-Z0-9_-]+\$$ (letters, digits, '-', '_') - e.g. v0-7-0, not v0.7.0" >&2; exit 1;; esac; \
	ORG="$(PULUMI_ORG)"; \
	case "$$ORG" in ''|*[!a-zA-Z0-9_-]*) echo "PULUMI_ORG must match ^[a-zA-Z0-9_-]+\$$ - got '$$ORG'" >&2; exit 1;; esac; \
	QUAL="$$ORG/$$REL"; \
	test -f ./.env || { echo "heart/.env not found - copy heart/.env.example to heart/.env and set DIGITALOCEAN_TOKEN and INFLUX_TOKEN" >&2; exit 1; }; \
	. ./.env; \
	: "$${DIGITALOCEAN_TOKEN:?missing from heart/.env (see heart/.env.example)}"; \
	: "$${INFLUX_TOKEN:?missing from heart/.env (see heart/.env.example)}"; \
	case "$$DIGITALOCEAN_TOKEN" in *[[:space:][:cntrl:]]*) echo "DIGITALOCEAN_TOKEN in heart/.env must not contain whitespace or control characters" >&2; exit 1;; esac; \
	case "$$INFLUX_TOKEN" in *[[:space:][:cntrl:]]*) echo "INFLUX_TOKEN in heart/.env must not contain whitespace or control characters" >&2; exit 1;; esac; \
	pulumi stack select --create "$$QUAL"; \
	pulumi config set heart:release "$$REL" --stack "$$QUAL"; \
	pulumi config set heart:project-name unyt --stack "$$QUAL"; \
	printf '%s' "$$DIGITALOCEAN_TOKEN" | pulumi config set --secret digitalocean:token --stack "$$QUAL" --; \
	printf '%s' "$$INFLUX_TOKEN" | pulumi config set --secret heart:influx-token --stack "$$QUAL" --; \
	echo; \
	echo "Stack '$$QUAL' ready: heart:release and heart:project-name (unyt) set,"; \
	echo "digitalocean:token + heart:influx-token read from heart/.env."; \
	echo "Metrics default to the shared 'unyt' InfluxDB bucket. To isolate this"; \
	echo "release's metrics, set heart:influx-bucket to a bucket that already exists."; \
	echo "Optional overrides (versions, endpoints, sizes, counts): see Pulumi.release.yaml.example"; \
	echo "Then:  make preview  &&  make up"

preview: ## Preview changes for the selected stack (or STACK=...)
	pulumi preview $(PULUMI_STACK)

up: ## Deploy the selected stack (or STACK=...) and write its release IP file
	pulumi up $(PULUMI_STACK)
	@rel=$$(pulumi config get heart:release $(PULUMI_STACK)); \
	mkdir -p releases/$$rel; \
	pulumi stack output --json ips $(PULUMI_STACK) > releases/$$rel/ips.json; \
	echo "Wrote releases/$$rel/ips.json"

refresh: ## Reconcile state with real infrastructure (or STACK=...)
	pulumi refresh $(PULUMI_STACK)

destroy: ## Tear down the selected stack (or STACK=...)
	pulumi destroy $(PULUMI_STACK)

## --- cloudflare tunnel secrets ---

set-tunnel-secret: ## Seed CF tunnel cert+creds into a stack from raw files: make set-tunnel-secret TUNNEL=unyt-tunnel CERT=cert.pem CREDS=creds.json [STACK=…]
	@test -n "$(TUNNEL)" || { echo "TUNNEL is required, e.g. TUNNEL=unyt-tunnel (must match .tunnel.tunnel_name in the automation config)" >&2; exit 1; }
	@test -n "$(CERT)"   || { echo "CERT is required: path to the raw cert.pem" >&2; exit 1; }
	@test -n "$(CREDS)"  || { echo "CREDS is required: path to the raw <tunnel>-credentials.json" >&2; exit 1; }
	@test -f "$(CERT)"   || { echo "CERT file not found: $(CERT)" >&2; exit 1; }
	@test -f "$(CREDS)"  || { echo "CREDS file not found: $(CREDS)" >&2; exit 1; }
	@jq -e . "$(CREDS)" >/dev/null 2>&1 || { echo "CREDS is not valid JSON: $(CREDS)" >&2; exit 1; }
	base64 -w0 "$(CERT)"  | pulumi config set --secret heart:cf-cert-pem $(PULUMI_STACK) --
	base64 -w0 "$(CREDS)" | pulumi config set --secret heart:$(TUNNEL)-credentials-json $(PULUMI_STACK) --
	@echo
	@echo "Seeded heart:cf-cert-pem and heart:$(TUNNEL)-credentials-json$(if $(STACK), on stack $(STACK),)."
	@echo "Pull them onto this machine with:  (cd ../automation && make pull-secrets$(if $(STACK), PULUMI_STACK=$(STACK),))"

## --- inspect ---

config: ## Show config for the selected stack (or STACK=...)
	pulumi config $(PULUMI_STACK)

stacks: ## List all stacks
	pulumi stack ls

current: ## Show the currently selected stack and its resources
	pulumi stack $(PULUMI_STACK)

.PHONY: help build test vet fmt new-release preview up refresh destroy set-tunnel-secret config stacks current

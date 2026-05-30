#!/usr/bin/env bash
# fleet-up.sh — create OR update a heart fleet stack and provision every server.
#
# One command that: picks/creates the per-release stack, asks for anything that
# isn't configured yet, and runs `pulumi up`. Re-run any time to update an
# existing fleet (it only prompts for what's missing).
#
# Run inside the dev shell so pulumi + go are on PATH:
#   nix develop -c ./scripts/fleet-up.sh
set -euo pipefail

cd "$(dirname "$0")/.."   # heart/ (the Pulumi program dir)

command -v pulumi >/dev/null || { echo "pulumi not on PATH — run inside 'nix develop'." >&2; exit 1; }
command -v go >/dev/null     || { echo "go not on PATH — run inside 'nix develop'." >&2; exit 1; }

bold() { printf '\033[1m%s\033[0m\n' "$*"; }

# ── 1. Release version → stack ──────────────────────────────────────────
read -rp "Release version (e.g. v0.90.0): " REL
[ -n "$REL" ] || { echo "release version is required" >&2; exit 1; }
slug=$(printf '%s' "$REL" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9-' '-' | sed 's/-\+/-/g; s/-$//')
DEFAULT_STACK="unyt-heart-${slug}"
read -rp "Stack name [${DEFAULT_STACK}]: " STACK
STACK="${STACK:-$DEFAULT_STACK}"

if pulumi stack select "$STACK" 2>/dev/null; then
  bold "Updating existing stack: $STACK"
else
  bold "Creating new stack: $STACK"
  pulumi stack init "$STACK"
fi
pulumi config set heart:release-version "$REL"

# ── 2. Ensure config; prompt only for what's missing ────────────────────
have() { pulumi config get "$1" >/dev/null 2>&1; }
ask() {  # <key> <prompt>
  have "$1" && return 0
  local v; read -rp "  $2: " v; [ -n "$v" ] && pulumi config set "$1" "$v"
}
ask_secret() {  # <key> <prompt>
  have "$1" && return 0
  local v; read -rsp "  $2: " v; echo; [ -n "$v" ] && pulumi config set --secret "$1" "$v"
}

bold "Core config"
ask_secret digitalocean:token "DigitalOcean API token"
ask_secret heart:influx-token "InfluxDB write token"
ask        heart:project-name "DigitalOcean project name (must already exist, e.g. Holo)"

if ! have heart:ssh-private-key; then
  read -rp "  Path to an SSH private key that can root-SSH the droplets [~/.ssh/id_ed25519]: " keypath
  keypath="${keypath:-$HOME/.ssh/id_ed25519}"
  keypath="${keypath/#\~/$HOME}"
  [ -r "$keypath" ] && pulumi config set --secret heart:ssh-private-key -- < "$keypath" \
    || echo "  (skipped ssh-private-key — $keypath not readable; orchestrated deploy needs it)"
fi

# ── 3. Cloudflare (tunnel + DNS owned by Pulumi) — optional ─────────────
if ! have heart:cf-account-id; then
  read -rp "Manage the Cloudflare tunnel + DNS in Pulumi? [Y/n] " cf
  if [[ "${cf:-y}" =~ ^[Yy] ]]; then
    bold "Cloudflare config"
    ask_secret cloudflare:apiToken "Cloudflare API token (Tunnel:Edit, Zone.DNS:Edit, Zone:Read)"
    ask        heart:cf-account-id "Cloudflare account id"
    ask        heart:cf-zone-name  "Cloudflare zone (e.g. unyt.co)"
    if ! have heart:cloudflare-tunnel-secret; then
      pulumi config set --secret heart:cloudflare-tunnel-secret "$(head -c 32 /dev/urandom | base64)"
      echo "  generated heart:cloudflare-tunnel-secret"
    fi
    ask heart:gw-hostname "Public gateway hostname (e.g. unyt-tunnel-${slug}.unyt.co)"
  else
    echo "  Skipping Cloudflare (hybrid mode — manage the tunnel out-of-band)."
  fi
fi

# ── 4. Optional: end-to-end post-boot orchestration ─────────────────────
if ! have heart:orchestrate-postboot; then
  read -rp "Have 'pulumi up' also run the per-server deploy (install .happ, gateway, tunnel)? [y/N] " orch
  [[ "${orch:-n}" =~ ^[Yy] ]] && pulumi config set heart:orchestrate-postboot true \
                              || pulumi config set heart:orchestrate-postboot false
fi

# ── 5. Provision ────────────────────────────────────────────────────────
bold "Previewing + applying $STACK ..."
pulumi up

cat <<DONE

Fleet '$STACK' is up. Next:
  - cache the fleet map for the automation scripts:
      ( cd ../automation && make pull-fleet PULUMI_STACK=$(pulumi whoami)/heart/$STACK )
  - walk through the per-server app deploy:
      ( cd ../automation && PULUMI_STACK=$(pulumi whoami)/heart/$STACK ./scripts/deploy-all.sh )
  - tear it all down if something broke:
      ./scripts/fleet-down.sh $STACK
DONE

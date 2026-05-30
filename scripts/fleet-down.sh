#!/usr/bin/env bash
# fleet-down.sh — tear a heart fleet stack all the way down so you can start
# clean after something breaks. Destroys every resource Pulumi manages for the
# stack (droplets, tunnel, DNS, firewall). Keeps the stack + its config by
# default so `fleet-up.sh` can recreate without re-prompting; pass --rm to also
# delete the stack, or --keep-cache to leave the local fleet cache.
#
#   nix develop -c ./scripts/fleet-down.sh [<stack>] [--rm] [--keep-cache]
set -euo pipefail

cd "$(dirname "$0")/.."   # heart/
command -v pulumi >/dev/null || { echo "pulumi not on PATH — run inside 'nix develop'." >&2; exit 1; }

STACK=""; RM=0; KEEP_CACHE=0
for arg in "$@"; do
  case "$arg" in
    --rm) RM=1 ;;
    --keep-cache) KEEP_CACHE=1 ;;
    -*) echo "unknown flag: $arg" >&2; exit 1 ;;
    *) STACK="$arg" ;;
  esac
done

[ -n "$STACK" ] && pulumi stack select "$STACK"
STACK="$(pulumi stack --show-name)"
[ -n "$STACK" ] || { echo "no stack selected; pass one: fleet-down.sh <stack>" >&2; exit 1; }

echo "This will DESTROY all resources in stack: $STACK"
read -rp "Type the stack name to confirm: " confirm
[ "$confirm" = "$STACK" ] || { echo "mismatch — aborting."; exit 1; }

pulumi destroy --stack "$STACK"

if [ "$KEEP_CACHE" -eq 0 ]; then
  rm -f "${FLEET_JSON:-$HOME/.config/unyt-deploy/fleet.json}"
  echo "cleared local fleet cache"
fi

if [ "$RM" -eq 1 ]; then
  pulumi stack rm "$STACK"
  echo "removed stack $STACK"
else
  echo "stack $STACK kept (config intact). Recreate with ./scripts/fleet-up.sh"
fi

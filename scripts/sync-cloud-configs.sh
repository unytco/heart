#!/usr/bin/env bash
#
# sync-cloud-configs.sh
#
# heart provisions two droplet flavours (default + alt) which currently
# share an identical cloud-config. Rather than templating around the
# minor differences upstream might want later (there are none today),
# we enforce byte-identity by copying default -> alt and failing CI if
# anyone hand-edits alt out of sync.
#
# Usage:
#   ./scripts/sync-cloud-configs.sh           # copy default -> alt
#   ./scripts/sync-cloud-configs.sh --check   # exit non-zero if they differ
#
# This is intentionally a stop-gap. The right long-term fix is to
# template once and emit both flavours from the Pulumi program; flag
# tracked in CHANGELOG / upstream-hc-http-gw-release-todo.md.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
DEFAULT="${REPO_ROOT}/cloudinit/default/cloud-config.yaml"
ALT="${REPO_ROOT}/cloudinit/alt/cloud-config.yaml"

if [[ ! -f "$DEFAULT" ]]; then
    echo "ERROR: $DEFAULT not found" >&2
    exit 1
fi

case "${1:-sync}" in
    --check|check)
        if ! diff -q "$DEFAULT" "$ALT" >/dev/null 2>&1; then
            echo "ERROR: cloudinit/default and cloudinit/alt are out of sync." >&2
            echo "       Run ./scripts/sync-cloud-configs.sh to fix." >&2
            diff -u "$DEFAULT" "$ALT" >&2 || true
            exit 1
        fi
        echo "default/cloud-config.yaml == alt/cloud-config.yaml"
        ;;
    sync|"")
        cp "$DEFAULT" "$ALT"
        echo "Copied default/cloud-config.yaml -> alt/cloud-config.yaml"
        ;;
    -h|--help)
        sed -n '/^# Usage:/,/^$/p' "$0" | sed 's/^# \?//'
        ;;
    *)
        echo "ERROR: unknown argument $1" >&2
        exit 1
        ;;
esac

#!/usr/bin/env bash
#
# smoke-test.sh
#
# End-to-end probe that the explorer's gateway chain is healthy:
#   browser -> CF Pages -> CF Worker -> CF Tunnel -> hc-http-gw -> conductor
#
# Exercises /health (which only requires the tunnel + gateway to be up)
# and optionally a known zome-call URL shape (which additionally
# requires an .happ to be installed and hc-http-gw-configure to have
# been run).
#
# Usage:
#   ./scripts/smoke-test.sh <hostname>
#       Health probe only.
#   ./scripts/smoke-test.sh <hostname> <dna_hash> <app_id> <zome>/<fn>
#       Health + zome-call shape probe.
#
# Examples:
#   ./scripts/smoke-test.sh unyt-tunnel.unyt.co
#   ./scripts/smoke-test.sh unyt-tunnel.unyt.co \
#       uhC0kd6W3cCdJqFSe0SoQN8lr3iwLmGPNkdWWKZlyciLM1bEHpOif \
#       unyt-sandbox-holo-hosting-0.90 \
#       transactor/get_global_units_details
#
# Exit codes:
#   0  all checks passed
#   1  /health failed
#   2  zome-call probe failed (only if requested)
#   3  usage error
#
# Designed to be runnable from anywhere — from an operator laptop after
# `pulumi up`, from CI after a deploy, or from the droplet itself
# (using the loopback url). Reads no environment except the standard
# CURL_OPTS pass-through for proxy/TLS overrides.

set -eo pipefail

usage() {
    sed -n '/^# Usage:/,/^$/p' "$0" | sed 's/^# \?//'
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" || $# -lt 1 ]]; then
    usage
    exit 3
fi

HOSTNAME="$1"
DNA_HASH="${2:-}"
APP_ID="${3:-}"
ZOME_FN="${4:-}"

# Strip protocol if the caller pasted a URL.
HOSTNAME="${HOSTNAME#https://}"
HOSTNAME="${HOSTNAME#http://}"
HOSTNAME="${HOSTNAME%%/*}"

BASE="https://${HOSTNAME}"

# `curl -fsS` makes 4xx/5xx an error, hides progress, but still shows
# stderr on failure. ${CURL_OPTS:-} lets the caller inject e.g.
# --resolve or --insecure when probing a staging environment.
CURL=(curl -fsS --max-time 10 ${CURL_OPTS:-})

step() { printf '\033[1;34m→\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m✓\033[0m %s\n' "$*"; }
err()  { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; }

# ---- /health probe ------------------------------------------------------------
step "GET ${BASE}/health"
if ! body=$("${CURL[@]}" "${BASE}/health"); then
    err "/health failed; gateway or tunnel is down. Inspect with:"
    err "  ssh <droplet> 'journalctl -u cloudflared -u hc-http-gw --no-pager | tail -100'"
    exit 1
fi
case "${body}" in
    Ok*|ok*) ok "/health returned 'Ok'" ;;
    *)       err "/health returned unexpected body: ${body}" ; exit 1 ;;
esac

# ---- zome-call shape probe ---------------------------------------------------
# Optional. We don't validate the response body (it's app-specific), only
# that the URL shape is reachable with a non-5xx status — anything 2xx or
# 4xx (e.g. "no payload" with a 400) confirms the request made it
# through the worker, the tunnel, hc-http-gw's path matcher, and into
# Holochain. A 502/503/504 means we lost something along the way.
if [[ -n "${DNA_HASH}" || -n "${APP_ID}" || -n "${ZOME_FN}" ]]; then
    if [[ -z "${DNA_HASH}" || -z "${APP_ID}" || -z "${ZOME_FN}" ]]; then
        err "if you pass dna_hash you must also pass app_id and zome/fn"
        usage
        exit 3
    fi

    zome_url="${BASE}/${DNA_HASH}/${APP_ID}/${ZOME_FN}"
    step "GET ${zome_url}"
    # We deliberately allow 4xx here (-w '%{http_code}'); only the
    # transport / proxy chain is being verified.
    http_code=$(curl -sS --max-time 15 -o /tmp/heart-smoke-zome-body \
        -w '%{http_code}' "${zome_url}" || echo "000")
    case "${http_code}" in
        2[0-9][0-9]|4[0-9][0-9])
            ok "zome-call URL reachable (HTTP ${http_code})"
            ;;
        000)
            err "zome-call URL: connect failed (no http response)"
            exit 2
            ;;
        5[0-9][0-9])
            err "zome-call URL returned ${http_code} — transport degraded:"
            err "  $(head -c 200 /tmp/heart-smoke-zome-body 2>/dev/null || true)"
            exit 2
            ;;
        *)
            err "zome-call URL returned unexpected ${http_code}"
            exit 2
            ;;
    esac
fi

ok "smoke test passed: ${BASE}"

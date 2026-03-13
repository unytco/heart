#!/usr/bin/env bash
# Sets the holo-keyutil release tag in cloudinit/default/cloud-config.yaml.
#
# The binary is built and published automatically by the release-holo-keyutil
# GitHub Actions workflow when a tag is pushed. Run this after publishing a
# release to point the cloud-config at the new version.
#
# Usage:
#   ./scripts/package-cloudinit-binaries.sh <tag>
#
# Example:
#   ./scripts/package-cloudinit-binaries.sh v0.1.0
#
# Requires: sed
set -eo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CLOUD_CONFIG="${REPO_ROOT}/cloudinit/default/cloud-config.yaml"

TAG="${1:-}"
if [ -z "${TAG}" ]; then
    echo "Usage: $0 <tag>" >&2
    echo "Example: $0 v0.1.0" >&2
    exit 1
fi

echo "==> Setting holo-keyutil version to ${TAG} in cloud-config.yaml..."
sed -i "s|HOLO_KEYUTIL_TAG_PLACEHOLDER|${TAG}|" "${CLOUD_CONFIG}"

echo "==> Done. Commit cloudinit/default/cloud-config.yaml with the updated version."

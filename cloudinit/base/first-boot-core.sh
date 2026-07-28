#!/usr/bin/env bash
# SHARED base-image first-boot CORE - single source of truth for the invariant
# middle both heart's cloud-init and the local-testnet fleet image run verbatim:
# generate the lair passphrase, initialise lair, discover its connection URL,
# and patch the conductor config with it. Self-contained (defines its own paths)
# so each consumer can call it directly.
#
# The DIVERGENT wrappers around this core live with each consumer and are NOT
# shared: heart's wrapper (cloudinit/cloud-config.yaml -> holochain-first-boot)
# adds the journald fix, Telegraf install, binary downloads, and service start;
# the fleet's wrapper (automation/emulation/heart/first-boot/holochain-first-boot)
# adds ssh host-key generation and nothing else (binaries are baked at build,
# systemd oneshot ordering starts the services). Consumed by:
#   - heart:  cloudinit/cloud-config.yaml (base64-injected as
#             /usr/local/bin/holochain-first-boot-core, called by the wrapper)
#   - fleet:  automation/emulation/heart/Dockerfile (COPYed to the same path)
set -eo pipefail

HOLOCHAIN_DIR=/var/lib/holochain
LAIR_DIR=${HOLOCHAIN_DIR}/lair
CONFIG_FILE=/etc/holochain/conductor-config.yaml

echo "==> Generating lair passphrase..."
mkdir -p "${HOLOCHAIN_DIR}" "${LAIR_DIR}"
# The `lair` group lets co-located non-root services (pricing-oracle,
# bridge-orchestrator) read the passphrase to sign via lair without running as
# root; root (lair-keystore / holochain) still owns the file.
groupadd -f lair
if [ ! -f "${HOLOCHAIN_DIR}/lair-passphrase" ]; then
    openssl rand -hex 32 > "${HOLOCHAIN_DIR}/lair-passphrase"
    echo "    Passphrase written to ${HOLOCHAIN_DIR}/lair-passphrase"
else
    echo "    Passphrase already exists, skipping"
fi
chown root:lair "${HOLOCHAIN_DIR}/lair-passphrase"
chmod 640 "${HOLOCHAIN_DIR}/lair-passphrase"

echo "==> Initialising lair keystore..."
printf '%s' "$(cat "${HOLOCHAIN_DIR}/lair-passphrase")" | \
    /usr/local/bin/lair-keystore --lair-root "${LAIR_DIR}" init --piped \
    || true  # non-zero if already initialised

echo "==> Discovering lair connection URL..."
LAIR_URL=$(/usr/local/bin/lair-keystore --lair-root "${LAIR_DIR}" url)
echo "    Lair URL: ${LAIR_URL}"

echo "==> Patching conductor config with lair URL..."
sed -i "s|connection_url: LAIR_CONNECTION_URL_PLACEHOLDER|connection_url: \"${LAIR_URL}\"|" \
    "${CONFIG_FILE}"

#!/usr/bin/env bash
# Build the three static binaries (openvpn, squid, dropbear) for one target arch
# and copy them into ../internal/assets/bin/ so the next `go build` picks them up.
#
# Run on Linux (or a Linux VM) with Docker installed.
# Cross-arch: pass --platform linux/arm64 etc. Requires binfmt qemu setup
#   docker run --privileged --rm tonistiigi/binfmt --install all
#
# Output:
#   ../internal/assets/bin/openvpn-<arch>
#   ../internal/assets/bin/dropbearmulti-<arch>
#   ../internal/assets/bin/squid-<arch>

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD="${ROOT}/build"
OUT="${ROOT}/internal/assets/bin"
mkdir -p "${OUT}"

PLATFORM="${PLATFORM:-linux/amd64}"
ARCH="${PLATFORM##*/}"

build_one() {
    local name="$1"
    local dockerfile="${BUILD}/Dockerfile.${name}"
    local artifact="$2"

    echo
    echo "=== building ${name} for ${PLATFORM} ==="
    if ! docker buildx build \
        --platform "${PLATFORM}" \
        --file "${dockerfile}" \
        --output "type=local,dest=${OUT}/_${name}-${ARCH}" \
        --target export \
        "${BUILD}"; then
        echo
        echo "!!! ${name} build FAILED. Skipping copy. !!!"
        return 1
    fi

    cp "${OUT}/_${name}-${ARCH}/${artifact}" "${OUT}/${artifact}-${ARCH}"
    rm -rf "${OUT}/_${name}-${ARCH}"
    ls -lh "${OUT}/${artifact}-${ARCH}"
}

# Order: easy ones first so a failure on squid doesn't waste the openvpn/dropbear run.
build_one openvpn openvpn || OPENVPN_FAILED=1
build_one dropbear dropbearmulti || DROPBEAR_FAILED=1
build_one squid squid || SQUID_FAILED=1

echo
echo "=== summary ==="
echo "openvpn   : ${OPENVPN_FAILED:+FAILED}${OPENVPN_FAILED:-OK}"
echo "dropbear  : ${DROPBEAR_FAILED:+FAILED}${DROPBEAR_FAILED:-OK}"
echo "squid     : ${SQUID_FAILED:+FAILED}${SQUID_FAILED:-OK}"
echo
echo "artifacts in: ${OUT}/"
ls -lh "${OUT}/" 2>/dev/null || true

# Exit non-zero only if the critical one (openvpn) failed.
[[ -z "${OPENVPN_FAILED:-}" ]]

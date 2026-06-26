#!/usr/bin/env bash
# Build hexplus-linux-<arch> for every supported architecture.
#
# Pipeline per arch:
#   1. docker buildx build --platform linux/<arch> the three static binaries
#      (openvpn, dropbearmulti, squid) into internal/assets/bin/_<arch>/.
#   2. stage them at internal/assets/bin/{openvpn,squid,dropbearmulti}
#      so //go:embed picks them up.
#   3. GOOS=linux GOARCH=<arch> go build -> dist/hexplus-linux-<arch>.
#
# Requires Docker Desktop with binfmt installed:
#   docker run --privileged --rm tonistiigi/binfmt --install all
#
# Override which archs to build with ARCHS=amd64 ./build-release.sh.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD="${ROOT}/build"
BIN="${ROOT}/internal/assets/bin"
DIST="${ROOT}/dist"

ARCHS="${ARCHS:-amd64 arm64}"

# Map buildx arch names to GOARCH (1:1 today, but explicit is cheaper
# than a future surprise).
goarch_for() {
    case "$1" in
        amd64) echo "amd64" ;;
        arm64) echo "arm64" ;;
        arm)   echo "arm" ;;
        *) echo "unsupported arch: $1" >&2; exit 1 ;;
    esac
}

# stage_arch copies the per-arch tree into the top-level bin/ dir so
# the next `go build` embeds the right binaries.
stage_arch() {
    local arch="$1"
    cp "${BIN}/_openvpn-${arch}/openvpn"               "${BIN}/openvpn"
    cp "${BIN}/_dropbear-${arch}/dropbearmulti"        "${BIN}/dropbearmulti"
    cp "${BIN}/_squid-${arch}/squid"                   "${BIN}/squid"
}

# build_statics_for runs the three Dockerfiles for one arch and leaves
# the artifacts in internal/assets/bin/_<arch>/.
build_statics_for() {
    local arch="$1"
    local plat="linux/${arch}"
    echo
    echo "=== building static deps for ${plat} ==="
    docker buildx build --platform "${plat}" --file "${BUILD}/Dockerfile.dropbear" \
        --output "type=local,dest=${BIN}/_dropbear-${arch}" --target export "${BUILD}"
    docker buildx build --platform "${plat}" --file "${BUILD}/Dockerfile.openvpn" \
        --output "type=local,dest=${BIN}/_openvpn-${arch}" --target export "${BUILD}"
    docker buildx build --platform "${plat}" --file "${BUILD}/Dockerfile.squid" \
        --output "type=local,dest=${BIN}/_squid-${arch}" --target export "${BUILD}"
}

# build_go_for stages the matching statics and cross-compiles the
# hexplus binary for one arch.
build_go_for() {
    local arch="$1"
    local goarch
    goarch="$(goarch_for "${arch}")"
    stage_arch "${arch}"
    mkdir -p "${DIST}"
    local out="${DIST}/hexplus-linux-${arch}"
    echo
    echo "=== go build ${out} ==="
    (
        cd "${ROOT}"
        CGO_ENABLED=0 GOOS=linux GOARCH="${goarch}" \
            go build -ldflags "-s -w" -o "${out}" ./cmd/hexplus
    )
    ls -lh "${out}"
}

mkdir -p "${BIN}" "${DIST}"

for arch in ${ARCHS}; do
    build_statics_for "${arch}"
done

# Statics done; now do the Go builds. Done in a second pass so a failure
# in the slow Docker phase doesn't waste the staging time.
for arch in ${ARCHS}; do
    build_go_for "${arch}"
done

echo
echo "=== release artifacts ==="
ls -lh "${DIST}/"hexplus-linux-* 2>/dev/null || true

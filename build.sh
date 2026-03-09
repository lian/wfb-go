#!/bin/bash
# Build wfb-go binaries for multiple architectures

set -e

VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
GIT_COMMIT=${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}
BUILD_DATE=${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
OUTPUT_DIR=${OUTPUT_DIR:-"dist"}

# ldflags for version injection
VERSION_PKG="github.com/lian/wfb-go/pkg/version"
LDFLAGS="-s -w -X ${VERSION_PKG}.Version=${VERSION} -X ${VERSION_PKG}.GitCommit=${GIT_COMMIT} -X ${VERSION_PKG}.BuildDate=${BUILD_DATE}"

# Binaries to build
BINS=(
    "cmd/wfb_server:wfb_server"
    "cmd/wfb_keygen:wfb_keygen"
    "cmd/wfb_tx:wfb_tx"
    "cmd/wfb_rx:wfb_rx"
    "cmd/wfb_cli:wfb_cli"
    "cmd/wfb_tx_cmd:wfb_tx_cmd"
    "cmd/wfb_tun:wfb_tun"
)

# Target architectures: GOOS/GOARCH/GOARM
TARGETS=(
    "linux/amd64/"
    "linux/arm64/"
    "linux/arm/7"
)

build() {
    local goos=$1
    local goarch=$2
    local goarm=$3
    local src=$4
    local name=$5

    local outdir="${OUTPUT_DIR}/${goos}_${goarch}"
    [[ -n "$goarm" ]] && outdir="${OUTPUT_DIR}/${goos}_${goarch}v${goarm}"

    mkdir -p "$outdir"

    local output="${outdir}/${name}"

    echo "Building ${name} for ${goos}/${goarch}${goarm:+v$goarm}..."

    GOOS=$goos GOARCH=$goarch GOARM=$goarm CGO_ENABLED=0 \
        go build -ldflags="${LDFLAGS}" -o "$output" "./${src}"
}

# Parse arguments
SELECTED_TARGETS=()
SELECTED_BINS=()

while [[ $# -gt 0 ]]; do
    case $1 in
        --target)
            SELECTED_TARGETS+=("$2")
            shift 2
            ;;
        --bin)
            SELECTED_BINS+=("$2")
            shift 2
            ;;
        --help|-h)
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  --target <os/arch>   Build only for specific target (can be repeated)"
            echo "  --bin <name>         Build only specific binary (can be repeated)"
            echo ""
            echo "Available targets:"
            for t in "${TARGETS[@]}"; do
                IFS='/' read -r os arch arm <<< "$t"
                echo "  ${os}/${arch}${arm:+v$arm}"
            done
            echo ""
            echo "Available binaries:"
            for b in "${BINS[@]}"; do
                IFS=':' read -r src name <<< "$b"
                echo "  ${name}"
            done
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Use all targets/bins if none selected
[[ ${#SELECTED_TARGETS[@]} -eq 0 ]] && SELECTED_TARGETS=("all")
[[ ${#SELECTED_BINS[@]} -eq 0 ]] && SELECTED_BINS=("all")

echo "wfb-go build script"
echo "Version: ${VERSION}"
echo "Output:  ${OUTPUT_DIR}/"
echo ""

for target in "${TARGETS[@]}"; do
    IFS='/' read -r goos goarch goarm <<< "$target"

    # Check if target is selected
    if [[ "${SELECTED_TARGETS[0]}" != "all" ]]; then
        match=0
        for sel in "${SELECTED_TARGETS[@]}"; do
            if [[ "$sel" == "${goos}/${goarch}"* ]]; then
                match=1
                break
            fi
        done
        [[ $match -eq 0 ]] && continue
    fi

    for bin in "${BINS[@]}"; do
        IFS=':' read -r src name <<< "$bin"

        # Check if binary is selected
        if [[ "${SELECTED_BINS[0]}" != "all" ]]; then
            match=0
            for sel in "${SELECTED_BINS[@]}"; do
                [[ "$sel" == "$name" ]] && match=1 && break
            done
            [[ $match -eq 0 ]] && continue
        fi

        build "$goos" "$goarch" "$goarm" "$src" "$name"
    done
done

echo ""
echo "Build complete. Output in ${OUTPUT_DIR}/"
ls -la "${OUTPUT_DIR}"/*/ 2>/dev/null || true

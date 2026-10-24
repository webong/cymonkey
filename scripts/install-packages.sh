#!/usr/bin/env bash
# Builds the guest packages the cymy host runs and installs them as ext plugin
# packages. Re-running upgrades what is already installed.
#
#   scripts/install-packages.sh [--cymy PATH] [role...]
#
# Roles (default: all but providers):
#   display    display engines playwright, puppeteer and cymonkey (one program, three names)
#   device     the Board device provider
#   content    the content service (cymy content)
#   console    the console service (cymy console)
#   gateway    the display gateway (cymy device gateway serve)
#   mobile     the mobile-target package (POST /v1/device-targets)
#   providers  the ONNX and WebLLM content providers (need their own native runtimes)
#
# The packages go to $CYMONKEY_PLUGIN_DIR, or the default plugin directory.
# `cymy plugins check` reports what is installed.
set -euo pipefail

cd "$(dirname "$0")/.."
root=$(pwd)

cymy=${CYMY:-}
roles=()
while [ $# -gt 0 ]; do
  case "$1" in
    --cymy) cymy=${2:?--cymy needs a path}; shift 2 ;;
    -h|--help) sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*) echo "unknown option $1" >&2; exit 2 ;;
    *) roles+=("$1"); shift ;;
  esac
done
[ ${#roles[@]} -gt 0 ] || roles=(display device content console gateway mobile)

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

if [ -z "$cymy" ]; then
  if command -v cymy >/dev/null 2>&1; then
    cymy=$(command -v cymy)
  else
    echo "building cymy"
    go build -o "$stage/cymy" ./src
    cymy=$stage/cymy
  fi
fi

installed() { "$cymy" plugins list | grep -q "\"id\":\"$1\""; }

# package TYPE NAME MODULE PACKAGE: build one program from its module and install it.
package() {
  local type=$1 name=$2 module=$3 target=$4 dir="$stage/$2"
  mkdir -p "$dir"
  echo "building $name ($module $target)"
  go -C "$root/$module" build -o "$dir/$name" "$target"
  install_dir "$type" "$name" "$dir" "$name"
}

# install_dir TYPE NAME DIR EXECUTABLE: write the manifest and install or upgrade.
install_dir() {
  local type=$1 name=$2 dir=$3 executable=$4
  "$cymy" plugins manifest --type "$type" --name "$name" --version 1.0.0 --command "$dir/$executable" >/dev/null
  if installed "$name"; then
    # An upgrade needs new content; identical content is already installed.
    local output
    if output=$("$cymy" plugins upgrade --manifest "$dir" 2>&1 >/dev/null); then
      echo "upgraded $name"
    elif [[ $output == *"requires a new revision"* ]]; then
      echo "unchanged $name"
    else
      echo "$output" >&2
      return 1
    fi
  else
    "$cymy" plugins install --manifest "$dir" >/dev/null
    echo "installed $name"
  fi
}

for role in "${roles[@]}"; do
  case "$role" in
    display)
      # One program installed once for each engine name it should answer to.
      echo "building display engine"
      mkdir -p "$stage/display-build"
      go -C "$root/pkg/board" build -o "$stage/display-build/display" ./cmd/display
      for engine in playwright puppeteer cymonkey; do
        mkdir -p "$stage/$engine"
        cp "$stage/display-build/display" "$stage/$engine/display"
        install_dir display "$engine" "$stage/$engine" display
      done ;;
    device)  package device board pkg/board ./cmd/provider ;;
    content) package content-service content pkg/console ./cmd/content ;;
    console) package console-service console pkg/console ./cmd/console ;;
    gateway) package gateway-service gateway pkg/board ./cmd/gateway ;;
    mobile)  package mobile-target mobile pkg/board ./cmd/mobile ;;
    providers)
      package content onnx pkg/content-onnx .
      package content webllm pkg/content-webllm . ;;
    *) echo "unknown role $role" >&2; exit 2 ;;
  esac
done

echo
"$cymy" plugins check

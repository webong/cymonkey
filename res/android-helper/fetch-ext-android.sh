#!/usr/bin/env bash
set -euo pipefail

version=0.2.0
expected=9efb738057bd6c88ff3297b57e8a9dc62f6dfb774e1bad852844e1c08dcacf30
helper_dir="$(cd "$(dirname "$0")" && pwd)"
dependency_dir="$helper_dir/.dependencies"
artifact="$dependency_dir/ext-android-$version.aar"
mkdir -p "$dependency_dir"

if [[ -f "$artifact" ]]; then
  actual="$(shasum -a 256 "$artifact" | cut -d ' ' -f 1)"
  if [[ "$actual" == "$expected" ]]; then
    echo "Verified $artifact"
    exit 0
  fi
  echo "Refusing mismatched existing artifact: $artifact" >&2
  exit 1
fi

download="$(mktemp "$dependency_dir/.ext-android.XXXXXX")"
trap 'rm -f "$download"' EXIT
curl --fail --location --show-error --silent \
  "https://github.com/webong/ext/releases/download/ext-android-v$version/ext-android-$version.aar" \
  --output "$download"
actual="$(shasum -a 256 "$download" | cut -d ' ' -f 1)"
if [[ "$actual" != "$expected" ]]; then
  echo "Ext Android checksum mismatch: $actual" >&2
  exit 1
fi
mv "$download" "$artifact"
echo "Verified $artifact"

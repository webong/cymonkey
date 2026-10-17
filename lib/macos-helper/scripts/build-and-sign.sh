#!/bin/sh
set -eu

if [ -z "${CODESIGN_IDENTITY:-}" ] || [ "${CODESIGN_IDENTITY}" = "-" ]; then
  echo "CODESIGN_IDENTITY must name the target owner's signing identity; ad-hoc signing is not accepted" >&2
  exit 2
fi

package_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
output_path=${1:-"${package_dir}/dist/cymonkey-macos-helper"}
build_path=${CYMONKEY_SWIFT_BUILD_PATH:-"${package_dir}/.build"}

/usr/bin/swift build --package-path "${package_dir}" --scratch-path "${build_path}" -c release
/bin/mkdir -p "$(dirname -- "${output_path}")"
/usr/bin/install -m 0755 "${build_path}/release/cymonkey-macos-helper" "${output_path}"

if [ -n "${CYMONKEY_ENTITLEMENTS:-}" ]; then
  /usr/bin/codesign --force --options runtime --timestamp --sign "${CODESIGN_IDENTITY}" --entitlements "${CYMONKEY_ENTITLEMENTS}" "${output_path}"
else
  /usr/bin/codesign --force --options runtime --timestamp --sign "${CODESIGN_IDENTITY}" "${output_path}"
fi

/usr/bin/codesign --verify --strict --verbose=2 "${output_path}"
echo "signed Cymonkey macOS helper at ${output_path}"

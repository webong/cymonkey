#!/usr/bin/env bash
set -euo pipefail

fixture_executable="${UNREAL_FIXTURE_EXECUTABLE:-/opt/unreal-cymonkey-fixture/Linux/UnrealPacmanFixture/Binaries/Linux/UnrealPacmanFixture-Linux-Shipping}"
artifact_dir="${UNREAL_ARTIFACT_DIR:-/workspace/artifacts}"
screen_width="${UNREAL_SCREEN_WIDTH:-1280}"
screen_height="${UNREAL_SCREEN_HEIGHT:-720}"

if [[ ! -x "${fixture_executable}" ]]; then
  echo "Unreal Pacman GPU fixture executable is missing: ${fixture_executable}" >&2
  exit 1
fi

mkdir -p "${artifact_dir}"
if command -v nvidia-smi >/dev/null 2>&1; then
  nvidia-smi >"${artifact_dir}/nvidia-smi.txt" 2>&1 || true
fi

launcher=()
if [[ "${UNREAL_USE_XVFB:-1}" == "1" && -z "${DISPLAY:-}" ]]; then
  if ! command -v xvfb-run >/dev/null 2>&1; then
    echo "UNREAL_USE_XVFB=1 but xvfb-run is unavailable; provide DISPLAY or install Xvfb." >&2
    exit 1
  fi
  launcher=(xvfb-run -a --server-args="-screen 0 ${screen_width}x${screen_height}x24")
fi

render_args=(
  -unattended
  -RenderOffscreen
  -NoSplash
  -stdout
  -FullStdOutLogOutput
  -Windowed
  "-ResX=${screen_width}"
  "-ResY=${screen_height}"
)
if [[ -n "${UNREAL_RENDER_API:-}" ]]; then
  render_args+=("-${UNREAL_RENDER_API}")
fi

exec "${launcher[@]}" "${fixture_executable}" "${render_args[@]}" "$@"

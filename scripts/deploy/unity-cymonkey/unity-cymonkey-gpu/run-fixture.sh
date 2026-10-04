#!/usr/bin/env bash
set -euo pipefail

unity_editor="${UNITY_EDITOR_PATH:-/opt/unity/Editor/Unity}"
project_path="${UNITY_PROJECT_PATH:-/workspace/tests/unity-cymonkey-fixture}"
artifact_dir="${UNITY_ARTIFACT_DIR:-/workspace/artifacts}"
log_path="${UNITY_FIXTURE_LOG_PATH:-${artifact_dir}/unity-cymonkey-fixture.log}"
screen_width="${UNITY_SCREEN_WIDTH:-1280}"
screen_height="${UNITY_SCREEN_HEIGHT:-720}"

if [[ ! -x "${unity_editor}" ]]; then
  echo "Unity Editor executable is missing at UNITY_EDITOR_PATH." >&2
  exit 1
fi
if [[ ! -f "${project_path}/Packages/manifest.json" ]]; then
  echo "Unity Cymonkey fixture project is missing." >&2
  exit 1
fi

mkdir -p "${artifact_dir}"
if command -v nvidia-smi >/dev/null 2>&1; then
  nvidia-smi >"${artifact_dir}/nvidia-smi.txt" 2>&1 || true
fi

launcher=()
if [[ "${UNITY_USE_XVFB:-1}" == "1" && -z "${DISPLAY:-}" ]]; then
  if ! command -v xvfb-run >/dev/null 2>&1; then
    echo "UNITY_USE_XVFB=1 but xvfb-run is unavailable; provide DISPLAY or install Xvfb." >&2
    exit 1
  fi
  launcher=(xvfb-run -a --server-args="-screen 0 ${screen_width}x${screen_height}x24")
fi

render_args=()
if [[ -n "${UNITY_RENDER_API:-}" ]]; then
  render_args+=("-force-${UNITY_RENDER_API}")
fi

set +e
"${launcher[@]}" "${unity_editor}" \
  -batchmode \
  -quit \
  -accept-apiupdate \
  -projectPath "${project_path}" \
  -screen-width "${screen_width}" \
  -screen-height "${screen_height}" \
  -screen-fullscreen 0 \
  "${render_args[@]}" \
  -executeMethod Jangolova.CymonkeyFixture.HeadlessPacmanFixture.Run \
  -logFile "${log_path}" \
  "$@"
fixture_status=$?
set -e

if [[ ${fixture_status} -ne 0 ]]; then
  echo "Unity Cymonkey GPU fixture failed; inspect the container log artifact." >&2
  exit "${fixture_status}"
fi
echo "Unity Cymonkey GPU fixture passed."

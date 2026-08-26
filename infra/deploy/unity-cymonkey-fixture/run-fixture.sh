#!/usr/bin/env bash
set -euo pipefail

unity_editor="${UNITY_EDITOR_PATH:-/opt/unity/Editor/Unity}"
project_path="${UNITY_PROJECT_PATH:-/workspace/tests/unity-cymonkey-fixture}"
log_path="${UNITY_FIXTURE_LOG_PATH:-/tmp/unity-cymonkey-fixture.log}"

if [[ ! -x "${unity_editor}" ]]; then
  echo "Unity Editor executable is missing at UNITY_EDITOR_PATH." >&2
  exit 1
fi
if [[ ! -f "${project_path}/Packages/manifest.json" ]]; then
  echo "Unity Pacman fixture project is missing." >&2
  exit 1
fi

set +e
"${unity_editor}" \
  -batchmode \
  -nographics \
  -quit \
  -accept-apiupdate \
  -projectPath "${project_path}" \
  -executeMethod Jangolova.CymonkeyFixture.HeadlessPacmanFixture.Run \
  -logFile "${log_path}"
fixture_status=$?
set -e

if [[ ${fixture_status} -ne 0 ]]; then
  echo "Unity Pacman headless fixture failed; inspect the container log artifact." >&2
  exit "${fixture_status}"
fi
echo "Unity Pacman headless fixture passed."

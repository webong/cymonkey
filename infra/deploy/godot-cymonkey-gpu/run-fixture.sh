#!/usr/bin/env bash
set -euo pipefail

project_path="${GODOT_PROJECT_PATH:-/workspace/tests/godot-cymonkey-fixture}"
artifact_dir="${GODOT_ARTIFACT_DIR:-${JANGOLOVA_ARTIFACT_DIR:-/workspace/artifacts}}"
display_driver="${GODOT_DISPLAY_DRIVER:-x11}"
rendering_method="${GODOT_RENDERING_METHOD:-gl_compatibility}"
rendering_driver="${GODOT_RENDERING_DRIVER:-opengl3}"
screen_width="${GODOT_SCREEN_WIDTH:-1280}"
screen_height="${GODOT_SCREEN_HEIGHT:-720}"

if [[ ! -f "${project_path}/project.godot" ]]; then
  echo "Godot Cymonkey fixture project is missing: ${project_path}" >&2
  exit 1
fi
if [[ -z "${JANGOLOVA_CYMONKEY_TOKEN:-}" ]]; then
  echo "JANGOLOVA_CYMONKEY_TOKEN must be supplied at runtime." >&2
  exit 1
fi

mkdir -p "${artifact_dir}"
if command -v nvidia-smi >/dev/null 2>&1; then
  nvidia-smi >"${artifact_dir}/nvidia-smi.txt" 2>&1 || true
fi

godot_args=(
  --path "${project_path}"
  --display-driver "${display_driver}"
  --rendering-method "${rendering_method}"
  --rendering-driver "${rendering_driver}"
  --log-file "${artifact_dir}/godot.log"
)
if [[ -n "${GODOT_GPU_INDEX:-}" ]]; then
  godot_args+=(--gpu-index "${GODOT_GPU_INDEX}")
fi

if [[ "${GODOT_USE_XVFB:-1}" == "1" && -z "${DISPLAY:-}" ]]; then
  if ! command -v xvfb-run >/dev/null 2>&1; then
    echo "GODOT_USE_XVFB=1 but xvfb-run is unavailable; provide DISPLAY or install Xvfb." >&2
    exit 1
  fi
  exec xvfb-run -a \
    --server-args="-screen 0 ${screen_width}x${screen_height}x24" \
    godot "${godot_args[@]}" "$@"
fi

exec godot "${godot_args[@]}" "$@"

#!/usr/bin/env bash
set -euo pipefail

fixture_executable="${UNREAL_FIXTURE_EXECUTABLE:-/opt/unreal-cymonkey-fixture/Linux/UnrealPacmanFixture/Binaries/Linux/UnrealPacmanFixture-Linux-Shipping}"
if [[ ! -x "${fixture_executable}" ]]; then
  echo "Unreal Cymonkey fixture executable is missing: ${fixture_executable}" >&2
  exit 1
fi

exec "${fixture_executable}" -nullrhi -unattended -NoSplash "$@"

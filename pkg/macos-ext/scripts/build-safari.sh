#!/bin/sh
set -eu

package_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
repo_dir=$(CDPATH= cd -- "${package_dir}/../.." && pwd)
web_extension_dir="${repo_dir}/pkg/browser-ext/.output/safari-mv3"
project_dir="${package_dir}/Safari"
project_path="${project_dir}/Jangolova/Jangolova.xcodeproj"
resource_dir="${project_dir}/Jangolova/Jangolova Extension/Resources"

npm --prefix "${repo_dir}/pkg/browser-ext" run build:safari

if [ -d "${project_path}" ]; then
  # The converter is the one-time project generator. Re-running it after the
  # containing app gains local packages and menu-bar code can rewrite or abort
  # on those customizations, so subsequent builds update only web resources.
  /usr/bin/rsync -a --delete "${web_extension_dir}/" "${resource_dir}/"
else
  /bin/mkdir -p "${project_dir}"
  /usr/bin/xcrun safari-web-extension-converter "${web_extension_dir}" \
    --project-location "${project_dir}" \
    --app-name "Cymonkey" \
    --bundle-identifier "dev.jangolova.macos" \
    --swift --macos-only --copy-resources --no-open --no-prompt
fi

echo "Safari containing app is ready at ${project_path}"

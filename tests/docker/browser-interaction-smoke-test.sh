#!/usr/bin/env bash
set -euo pipefail

# Chromium and Xvfb are target fixtures owned by this test, not by Jangolova.
display_number="${DISPLAY_NUM:-99}"
export DISPLAY=":${display_number}"
profile_path="/tmp/jangolova-target-profile"
provider_log="/tmp/jangolova-provider.log"
token="test-only-browser-interaction-token"

Xvfb "${DISPLAY}" -screen 0 1280x720x24 -ac +extension GLX +render -noreset >/tmp/jangolova-xvfb.log 2>&1 &
xvfb_pid=$!
chromium \
  --no-first-run \
  --no-default-browser-check \
  --no-sandbox \
  --disable-dev-shm-usage \
  --remote-debugging-address=127.0.0.1 \
  --remote-debugging-port=9222 \
  --user-data-dir="${profile_path}" \
  about:blank >/tmp/jangolova-target.log 2>&1 &
target_pid=$!
JANGOLOVA_PROVIDER_TOKEN="${token}" bin/cymonkey provider serve-engine-provider --bind 127.0.0.1:7391 >"${provider_log}" 2>&1 &
provider_pid=$!

cleanup() {
  kill -TERM "${provider_pid}" "${target_pid}" "${xvfb_pid}" 2>/dev/null || true
}
trap cleanup EXIT
trap 'cat "${provider_log}" >&2; cat /tmp/jangolova-target.log >&2' ERR

for _ in $(seq 1 150); do
  if curl -fsS http://127.0.0.1:9222/json/version >/dev/null 2>&1 && \
     curl -fsS http://127.0.0.1:7391/healthz >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done
curl -fsS http://127.0.0.1:9222/json/version >/dev/null
curl -fsS http://127.0.0.1:7391/healthz >/dev/null

for adapter in playwright puppeteer; do
  curl --fail-with-body -sS \
    -H "Authorization: Bearer ${token}" \
    -H "Content-Type: application/json" \
    -d "{\"apiVersion\":\"interaction.engine/v1alpha1\",\"instanceId\":\"${adapter}-one\",\"engine\":{\"adapter\":\"${adapter}\",\"source\":\"file:///app/tests/fixture.html\"},\"target\":{\"kind\":\"browser\",\"endpoints\":[{\"name\":\"cdp\",\"protocol\":\"cdp\",\"url\":\"http://127.0.0.1:9222\"}]}}" \
    http://127.0.0.1:7391/v1/instances >/tmp/"${adapter}"-connect.json

  curl -fsS \
    -H "Authorization: Bearer ${token}" \
    -H "Content-Type: application/json" \
    -d '{"method":"act","params":{"name":"window.evaluate","input":{"expression":"document.title"}}}' \
    http://127.0.0.1:7391/v1/instances/"${adapter}"-one/call >/tmp/"${adapter}"-call.json

  node -e '
    const fs = require("fs");
    const connected = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    const called = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
    if (connected.status !== "connected" || !connected.capabilities.includes("window.evaluate")) {
      throw new Error(`bad connection: ${JSON.stringify(connected)}`);
    }
    if (called.result?.value !== "Jangolova Fixture") {
      throw new Error(`bad interaction result: ${JSON.stringify(called)}`);
    }
  ' /tmp/"${adapter}"-connect.json /tmp/"${adapter}"-call.json

  curl -fsS -X DELETE -H "Authorization: Bearer ${token}" \
    http://127.0.0.1:7391/v1/instances/"${adapter}"-one

  # Disconnecting Jangolova must not terminate the caller-owned Chromium.
  curl -fsS http://127.0.0.1:9222/json/version >/dev/null
done

curl -fsS \
  -H "Authorization: Bearer ${token}" \
  -H "Content-Type: application/json" \
  -d '{"apiVersion":"interaction.engine/v1alpha1","instanceId":"cymonkey-cdp","engine":{"adapter":"cymonkey","requiredCapabilities":["augmentation.install","document.query","storage.set"],"options":{"driver":"cdp","extension":{"mode":"disabled"}}},"target":{"kind":"browser","endpoints":[{"name":"cdp","protocol":"cdp","url":"http://127.0.0.1:9222"}]}}' \
  http://127.0.0.1:7391/v1/instances >/tmp/cymonkey-cdp-connect.json

JANGOLOVA_PROVIDER_TOKEN="${token}" node tests/cymonkey-live-client.mjs \
  --provider http://127.0.0.1:7391 \
  --instance cymonkey-cdp \
  --expect-backend cdp

curl -fsS -X DELETE -H "Authorization: Bearer ${token}" \
  http://127.0.0.1:7391/v1/instances/cymonkey-cdp
curl -fsS http://127.0.0.1:9222/json/version >/dev/null

echo "Playwright, Puppeteer, and Cymonkey CDP smoke tests passed against a caller-owned browser"

#!/usr/bin/env bash
set -euo pipefail

# Firefox and its display are caller-owned test targets. Jangolova receives
# only the WebDriver BiDi endpoint.
export DISPLAY=:98
profile_path=/tmp/jangolova-firefox-profile
provider_log=/tmp/jangolova-firefox-provider.log
target_log=/tmp/jangolova-firefox-target.log
token=test-only-firefox-bidi-token

mkdir -p "${profile_path}"
Xvfb "${DISPLAY}" -screen 0 1280x720x24 -ac >/tmp/jangolova-firefox-xvfb.log 2>&1 &
xvfb_pid=$!
firefox-esr --no-remote --profile "${profile_path}" --remote-debugging-port 9223 about:blank >"${target_log}" 2>&1 &
target_pid=$!
JANGOLOVA_PROVIDER_TOKEN="${token}" bin/cymonkey provider serve-engine-provider --bind 127.0.0.1:7391 >"${provider_log}" 2>&1 &
provider_pid=$!

cleanup() {
  kill -TERM "${provider_pid}" "${target_pid}" "${xvfb_pid}" 2>/dev/null || true
}
trap cleanup EXIT
trap 'cat "${provider_log}" >&2; cat "${target_log}" >&2' ERR

for _ in $(seq 1 200); do
  if curl -sS --max-time 1 http://127.0.0.1:9223/session >/dev/null 2>&1 && \
     curl -fsS http://127.0.0.1:7391/healthz >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

curl -fsS \
  -H "Authorization: Bearer ${token}" \
  -H "Content-Type: application/json" \
  -d '{"apiVersion":"interaction.engine/v1alpha1","instanceId":"firefox-one","engine":{"adapter":"puppeteer","source":"file:///app/tests/fixture.html"},"target":{"kind":"browser","endpoints":[{"name":"bidi","protocol":"webdriver-bidi","url":"ws://127.0.0.1:9223/session"}]}}' \
  http://127.0.0.1:7391/v1/instances >/tmp/firefox-connect.json

curl -fsS \
  -H "Authorization: Bearer ${token}" \
  -H "Content-Type: application/json" \
  -d '{"method":"act","params":{"name":"window.evaluate","input":{"expression":"document.title"}}}' \
  http://127.0.0.1:7391/v1/instances/firefox-one/call >/tmp/firefox-call.json

node -e '
const fs = require("fs");
const connected = JSON.parse(fs.readFileSync("/tmp/firefox-connect.json", "utf8"));
const called = JSON.parse(fs.readFileSync("/tmp/firefox-call.json", "utf8"));
if (connected.status !== "connected" || called.result?.value !== "Jangolova Fixture") {
  throw new Error(`Firefox BiDi interaction failed: ${JSON.stringify({connected, called})}`);
}
'

curl -fsS -X DELETE -H "Authorization: Bearer ${token}" \
  http://127.0.0.1:7391/v1/instances/firefox-one

curl -fsS \
  -H "Authorization: Bearer ${token}" \
  -H "Content-Type: application/json" \
  -d '{"apiVersion":"interaction.engine/v1alpha1","instanceId":"cymonkey-bidi","engine":{"adapter":"cymonkey","requiredCapabilities":["augmentation.install","document.query","storage.set"],"options":{"backend":"bidi","extension":{"mode":"disabled"}}},"target":{"kind":"browser","endpoints":[{"name":"bidi","protocol":"webdriver-bidi","url":"ws://127.0.0.1:9223/session"}]}}' \
  http://127.0.0.1:7391/v1/instances >/tmp/cymonkey-bidi-connect.json

JANGOLOVA_PROVIDER_TOKEN="${token}" node tests/cymonkey-live-client.mjs \
  --provider http://127.0.0.1:7391 \
  --instance cymonkey-bidi \
  --expect-backend bidi

curl -fsS -X DELETE -H "Authorization: Bearer ${token}" \
  http://127.0.0.1:7391/v1/instances/cymonkey-bidi
kill -0 "${target_pid}"

echo "Puppeteer and Cymonkey WebDriver BiDi smoke tests passed against caller-owned Firefox"

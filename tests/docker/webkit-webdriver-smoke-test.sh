#!/usr/bin/env bash
set -euo pipefail

# WebKitGTK, WebKitWebDriver, its browser session, and Xvfb are caller-owned
# test targets. Jangolova receives only the existing session coordinates.
export DISPLAY=:97
driver_log=/tmp/jangolova-webkit-driver.log
provider_log=/tmp/jangolova-webkit-provider.log
token=test-only-webkit-webdriver-token

Xvfb "${DISPLAY}" -screen 0 1280x720x24 -ac >/tmp/jangolova-webkit-xvfb.log 2>&1 &
xvfb_pid=$!
WebKitWebDriver --host=127.0.0.1 --port=4445 >"${driver_log}" 2>&1 &
driver_pid=$!
JANGOLOVA_PROVIDER_TOKEN="${token}" bin/jangolova serve-engine-provider --bind 127.0.0.1:7391 >"${provider_log}" 2>&1 &
provider_pid=$!
session_id=

cleanup() {
  if [[ -n "${session_id}" ]]; then
    curl -sS -X DELETE "http://127.0.0.1:4445/session/${session_id}" >/dev/null 2>&1 || true
  fi
  kill -TERM "${provider_pid}" "${driver_pid}" "${xvfb_pid}" 2>/dev/null || true
}
trap cleanup EXIT
trap 'cat "${provider_log}" >&2; cat "${driver_log}" >&2' ERR

for _ in $(seq 1 200); do
  if curl -fsS http://127.0.0.1:4445/status >/dev/null 2>&1 && \
     curl -fsS http://127.0.0.1:7391/healthz >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

curl -fsS \
  -H "Content-Type: application/json" \
  -d '{"capabilities":{"alwaysMatch":{"browserName":"MiniBrowser"}}}' \
  http://127.0.0.1:4445/session >/tmp/webkit-session.json
session_id=$(node -e '
const value = JSON.parse(require("fs").readFileSync("/tmp/webkit-session.json", "utf8"));
if (!value.value?.sessionId) throw new Error(`no WebKit session: ${JSON.stringify(value)}`);
process.stdout.write(value.value.sessionId);
')

node - "${session_id}" <<'NODE' >/tmp/webkit-connect-request.json
const sessionId = process.argv[2];
process.stdout.write(JSON.stringify({
  apiVersion: "interaction.engine/v1alpha1",
  instanceId: "webkit-one",
  engine: {adapter: "webkit-webdriver", source: "file:///app/tests/fixture.html"},
  target: {
    kind: "browser",
    endpoints: [{name: "webdriver", protocol: "webdriver", url: "http://127.0.0.1:4445"}],
    handles: {"webdriver.sessionId": sessionId}
  }
}));
NODE

curl -fsS \
  -H "Authorization: Bearer ${token}" \
  -H "Content-Type: application/json" \
  --data-binary @/tmp/webkit-connect-request.json \
  http://127.0.0.1:7391/v1/instances >/tmp/webkit-connect.json

curl -fsS \
  -H "Authorization: Bearer ${token}" \
  -H "Content-Type: application/json" \
  -d '{"method":"act","params":{"name":"window.evaluate","input":{"expression":"document.title"}}}' \
  http://127.0.0.1:7391/v1/instances/webkit-one/call >/tmp/webkit-call.json

node -e '
const fs = require("fs");
const connected = JSON.parse(fs.readFileSync("/tmp/webkit-connect.json", "utf8"));
const called = JSON.parse(fs.readFileSync("/tmp/webkit-call.json", "utf8"));
if (connected.status !== "connected" || called.result !== "Jangolova Fixture") {
  throw new Error(`WebKit interaction failed: ${JSON.stringify({connected, called})}`);
}
'

curl -fsS -X DELETE -H "Authorization: Bearer ${token}" \
  http://127.0.0.1:7391/v1/instances/webkit-one

# Jangolova disconnect must leave both the caller-owned driver and session alive.
kill -0 "${driver_pid}"
curl -fsS "http://127.0.0.1:4445/session/${session_id}/url" >/dev/null

echo "WebKit WebDriver smoke test passed against caller-owned WebKitGTK session"

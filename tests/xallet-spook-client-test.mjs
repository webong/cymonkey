import assert from "node:assert/strict";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import test from "node:test";
import ts from "../pkg/browser-ext/node_modules/typescript/lib/typescript.js";

const sourceURL = new URL("../pkg/browser-ext/src/xallet-spook.ts", import.meta.url);

test("Xallet Spook discovery is optional, authenticated, and re-registers a same-ID hub", async () => {
  const { XalletSpookClient } = await loadClient();
  let extensions = [];
  let rejectMessages = false;
  const messages = [];
  const statuses = [];
  const api = {
    management: { getAll: async () => extensions },
    runtime: {
      sendMessage: async (extensionId, message) => {
        if (rejectMessages) throw new Error("hub unavailable");
        messages.push({ extensionId, message });
      },
    },
  };
  const state = {
    status: "ready",
    xalletSpook: "discovering",
    browser: "chrome",
    capabilities: [],
    extensionId: "jangolova-extension",
  };
  const client = new XalletSpookClient("Jangolova Browser Extension", state, "popup.html", (status) => statuses.push(status), api);

  await client.probe();
  assert.equal(statuses.at(-1), "unavailable");
  assert.equal(client.acceptsExternalSender("untrusted"), false);

  extensions = [{ id: "xallet-hub", name: "Xallet", enabled: true }];
  await client.probe();
  assert.equal(statuses.at(-1), "connected");
  assert.equal(client.acceptsExternalSender("xallet-hub"), true);
  assert.equal(client.acceptsExternalSender("untrusted"), false);
  assert.equal(messages.at(-1).message.type, "REGISTER_SPOKE");
  assert.equal(messages.at(-1).message.payload.initialState.xalletSpook, "connected");

  await client.probe();
  assert.equal(messages.filter(({ message }) => message.type === "REGISTER_SPOKE").length, 2, "same-ID hub must be re-registered after a restart");

  rejectMessages = true;
  await client.probe();
  assert.equal(statuses.at(-1), "unavailable");
  assert.equal(client.acceptsExternalSender("xallet-hub"), false);
});

async function loadClient() {
  const source = await readFile(sourceURL, "utf8");
  const output = ts.transpileModule(source, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 },
  }).outputText;
  const directory = await mkdtemp(join(tmpdir(), "jangolova-spook-test-"));
  const modulePath = join(directory, "xallet-spook.mjs");
  await writeFile(modulePath, output);
  return import(pathToFileURL(modulePath).href);
}

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const app = await readFile(new URL("../infra/deploy/blockade/app.py", import.meta.url), "utf8");
const container = await readFile(new URL("../infra/deploy/blockade/Containerfile", import.meta.url), "utf8");
const schema = JSON.parse(await readFile(new URL("../lib/blockade/protocol/v1alpha1/observation.schema.json", import.meta.url), "utf8"));

assert.equal(schema.properties.apiVersion.const, "blockade.observation/v1alpha1");
for (const route of ["/healthz", "/capabilities", "/v1/observe"]) assert.match(app, new RegExp(route.replaceAll("/", "\\/")));
assert.match(app, /YOLO\(/);
assert.match(app, /SAM\(/);
assert.match(container, /python:3\.12-slim/);
assert.match(container, /requirements\.txt/);
console.log("Blockade contract checks passed");

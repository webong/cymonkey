import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const config = await readFile(new URL("../infra/deploy/blockade/blockade.example.yaml", import.meta.url), "utf8");
const webllmConfig = await readFile(new URL("../infra/deploy/blockade/blockade.webllm.example.yaml", import.meta.url), "utf8");
const webllmDocs = await readFile(new URL("../docs/blockade-webllm.md", import.meta.url), "utf8");
const worker = await readFile(new URL("../providers/blockade-ultralytics/python/worker.py", import.meta.url), "utf8");
const runtime = await readFile(new URL("../providers/blockade-ultralytics/python/runtime.py", import.meta.url), "utf8");

assert.match(config, /providers:/);
assert.match(config, /provider: local-ultralytics/);
assert.match(config, /model: yolo-sam@1\.0\.0/);
assert.match(webllmConfig, /provider: webllm/);
assert.match(webllmConfig, /Phi-3\.5-vision-instruct-q4f16_1-MLC/);
assert.match(webllmDocs, /provider owned Chromium \+ dedicated Web Worker/);
assert.match(worker, /method == "health"/);
assert.match(runtime, /YOLO\(/);
assert.match(runtime, /SAM\(/);
console.log("Blockade provider fixture checks passed");

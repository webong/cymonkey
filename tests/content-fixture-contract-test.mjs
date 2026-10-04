import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const config = await readFile(new URL("../scripts/deploy/content/content.example.yaml", import.meta.url), "utf8");
const webllmConfig = await readFile(new URL("../scripts/deploy/content/content.webllm.example.yaml", import.meta.url), "utf8");
const endpointConfig = await readFile(new URL("../scripts/deploy/content/content.endpoints.example.yaml", import.meta.url), "utf8");
const webllmDocs = await readFile(new URL("../docs/content-webllm.md", import.meta.url), "utf8");
const worker = await readFile(new URL("../pkg/content-ultralytics/python/provider.py", import.meta.url), "utf8");
const runtime = await readFile(new URL("../pkg/content-ultralytics/python/inference.py", import.meta.url), "utf8");

assert.match(config, /apiVersion: cymonkey\.content-config\/v1alpha1/);
assert.match(config, /adapters:/);
assert.match(config, /plugin: local-ultralytics/);
assert.match(config, /model: yolo-sam@1\.0\.0/);
assert.match(webllmConfig, /plugin: webllm/);
assert.match(webllmConfig, /Phi-3\.5-vision-instruct-q4f16_1-MLC/);
assert.match(endpointConfig, /plugin: openai-compatible/);
assert.match(endpointConfig, /profile: ollama/);
assert.match(endpointConfig, /plugin: anthropic-compatible/);
assert.match(endpointConfig, /plugin: google-compatible/);
assert.match(endpointConfig, /modalities: text,image,audio,video/);
assert.match(webllmDocs, /plugin owned Chromium \+ dedicated Web Worker/);
assert.match(worker, /method\.startswith\("cymonkey\.content\."\)/);
assert.match(runtime, /YOLO\(/);
assert.match(runtime, /SAM\(/);
console.log("Cymonkey content plugin fixture checks passed");

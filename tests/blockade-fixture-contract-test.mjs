import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const launcher = await readFile(new URL("../deploy/blockade/run-fixture.sh", import.meta.url), "utf8");
const config = await readFile(new URL("../deploy/blockade/blockade.example.yaml", import.meta.url), "utf8");
const modelDocs = await readFile(new URL("../deploy/blockade/models/README.md", import.meta.url), "utf8");

for (const required of ["BLOCKADE_MODEL_CACHE", "BLOCKADE_YOLO_MODEL_FILE", "BLOCKADE_SAM_MODEL_FILE", "-v", ":/models:ro"]) {
  assert.match(launcher, new RegExp(required.replace(/[.*+?^${}()|[\\]\\]/g, "\\$&")));
}
assert.match(launcher, /missing YOLO weights/);
assert.match(launcher, /missing SAM weights/);
assert.match(config, /yoloModel: \/models\/yolo11n\.pt/);
assert.match(config, /samModel: \/models\/sam2_b\.pt/);
assert.match(modelDocs, /not committed/);
console.log("Blockade fixture contract checks passed");

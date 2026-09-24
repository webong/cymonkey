import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

test("presentation worker attaches through Puppeteer CDP and never launches a runtime", async () => {
  const source = await readFile(new URL("../scripts/presentation-worker.mjs", import.meta.url), "utf8");
  assert.match(source, /puppeteer\.connect/);
  assert.match(source, /window\.cymonkey\.jangolova/);
  assert.match(source, /presentation\.capture/);
  assert.match(source, /setRequestInterception/);
  assert.match(source, /allowedAssetOrigins/);
  assert.match(source, /withActionTimeout/);
  assert.match(source, /Runtime\.terminateExecution/);
  assert.match(source, /presentation\.mount/);
  assert.match(source, /supportedArtifactTransports/);
  assert.match(source, /headers: connectionHeaders/);
  assert.doesNotMatch(source, /puppeteer\.launch/);
});

test("web presentation host exposes declarative create, patch, artifact, and semantic activation", async () => {
  const source = await readFile(new URL("../examples/web-presentation/main.js", import.meta.url), "utf8");
  for (const capability of ["presentation.create", "presentation.replace", "presentation.write", "presentation.mount", "presentation.execute", "presentation.patch", "presentation.describe", "presentation.activate"]) {
    assert.match(source, new RegExp(capability.replaceAll(".", "\\.")));
  }
  assert.match(source, /window\.cymonkey[\s\S]{0,80}jangolova/);
  assert.match(source, /expectedStateRevision/);
  assert.match(source, /state revision conflict/);
});

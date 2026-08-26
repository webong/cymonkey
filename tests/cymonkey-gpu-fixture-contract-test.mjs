import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const fixtures = [
  {
    name: "Godot",
    container: "../deploy/godot-cymonkey-gpu/Containerfile",
    runner: "../deploy/godot-cymonkey-gpu/run-fixture.sh",
    requiredContainer: [/GODOT_IMAGE/, /NVIDIA_VISIBLE_DEVICES=all/, /NVIDIA_DRIVER_CAPABILITIES/, /EXPOSE 8090\/tcp/, /JANGOLOVA_ARTIFACT_DIR/],
    requiredRunner: [/JANGOLOVA_CYMONKEY_TOKEN/, /--display-driver/, /--rendering-method/, /--rendering-driver/, /nvidia-smi/, /xvfb-run/],
    forbiddenRunner: [/--headless/],
  },
  {
    name: "Unity",
    container: "../deploy/unity-cymonkey-gpu/Containerfile",
    runner: "../deploy/unity-cymonkey-gpu/run-fixture.sh",
    requiredContainer: [/UNITY_EDITOR_IMAGE/, /NVIDIA_VISIBLE_DEVICES=all/, /NVIDIA_DRIVER_CAPABILITIES/, /EXPOSE 8090\/tcp/, /UNITY_ARTIFACT_DIR/],
    requiredRunner: [/-batchmode/, /-executeMethod/, /nvidia-smi/, /xvfb-run/],
    forbiddenRunner: [/-nographics/],
  },
  {
    name: "Unreal",
    container: "../deploy/unreal-cymonkey-gpu/Containerfile",
    runner: "../deploy/unreal-cymonkey-gpu/run-fixture.sh",
    requiredContainer: [/UE_BUILD_IMAGE/, /UE_RUNTIME_IMAGE/, /NVIDIA_VISIBLE_DEVICES=all/, /NVIDIA_DRIVER_CAPABILITIES/, /EXPOSE 8090\/tcp/, /UNREAL_ARTIFACT_DIR/],
    requiredRunner: [/-RenderOffscreen/, /-unattended/, /nvidia-smi/, /xvfb-run/],
    forbiddenRunner: [/-nullrhi/],
  },
];

for (const fixture of fixtures) {
  const container = await readFile(new URL(fixture.container, import.meta.url), "utf8");
  const runner = await readFile(new URL(fixture.runner, import.meta.url), "utf8");
  for (const pattern of fixture.requiredContainer) assert.match(container, pattern, `${fixture.name} GPU container missing ${pattern}`);
  for (const pattern of fixture.requiredRunner) assert.match(runner, pattern, `${fixture.name} GPU runner missing ${pattern}`);
  for (const pattern of fixture.forbiddenRunner) assert.doesNotMatch(runner, pattern, `${fixture.name} GPU runner contains ${pattern}`);
  assert.doesNotMatch(`${container}\n${runner}`, /(?:PASSWORD|SERIAL|LICENSE|PRIVATE_KEY)\s*=/i);
}

console.log("Cymonkey GPU fixture environment contracts are valid.");

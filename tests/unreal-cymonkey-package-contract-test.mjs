import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const root = new URL("../pkg/unreal-cymonkey/", import.meta.url);
const plugin = JSON.parse(await readFile(new URL("JangolovaCymonkey.uplugin", root)));
const protocol = await readFile(new URL("Source/JangolovaCymonkey/Public/CymonkeyProtocol.h", root), "utf8");
const registryHeader = await readFile(new URL("Source/JangolovaCymonkey/Public/CymonkeyRegistryComponent.h", root), "utf8");
const registry = await readFile(new URL("Source/JangolovaCymonkey/Private/CymonkeyRegistryComponent.cpp", root), "utf8");
const transport = await readFile(new URL("Source/JangolovaCymonkey/Public/ICymonkeyTransportHost.h", root), "utf8");
const router = await readFile(new URL("Source/JangolovaCymonkey/Private/CymonkeyRequestRouter.cpp", root), "utf8");
const host = await readFile(new URL("Source/JangolovaCymonkey/Private/CymonkeyWebSocketHost.cpp", root), "utf8");
const hostHeader = await readFile(new URL("Source/JangolovaCymonkey/Public/CymonkeyWebSocketHost.h", root), "utf8");
const goProtocol = await readFile(new URL("../src/jangolova/contract/protocol.go", import.meta.url), "utf8");

assert.equal(plugin.Modules[0].Name, "JangolovaCymonkey");
assert.equal(plugin.Modules[0].Type, "Runtime");
const unrealVersion = protocol.match(/ProtocolVersion\[\].*TEXT\("([^"]+)"\)/)?.[1];
const goVersion = goProtocol.match(/ProtocolVersion\s+= "([^"]+)"/)?.[1];
assert.equal(unrealVersion, "cymonkey/v1alpha1");
assert.equal(unrealVersion, goVersion);
for (const method of ["hello", "capabilities", "describe", "act", "events", "health"]) {
  assert.ok(protocol.includes(`TEXT("${method}")`));
}
for (const kind of ["Scene", "Object", "UI", "Camera", "Material", "Animation", "Timeline", "Artifact", "Event"]) {
  assert.match(protocol, new RegExp(`\\b${kind}\\b`));
}
assert.match(protocol, /TObjectPtr<UObject> Target/);
assert.match(protocol, /TArray<FString> Actions/);
assert.match(registryHeader, /TMap<FString, const FCymonkeyRegistration\*> Allowlist/);
assert.match(registry, /check\(IsInGameThread\(\)\)/);
assert.match(registry, /action_not_allowlisted/);
assert.match(registry, /StableIdPattern/);
assert.doesNotMatch(registry, /TObjectIterator|GetAllActorsOfClass|ForEachObjectOfClass/);
assert.match(transport, /class JANGOLOVACYMONKEY_API ICymonkeyTransportHost/);
assert.match(hostHeader, /class JANGOLOVACYMONKEY_API FCymonkeyWebSocketHost/);
assert.match(host, /ConstantTimeEquals/);
assert.match(host, /Bearer %s/);
assert.match(router, /MaximumMessageBytes/);
assert.match(router, /AsyncTask\(ENamedThreads::GameThread/);
assert.match(router, /message_too_large/);
assert.doesNotMatch(`${registry}\n${transport}\n${router}\n${host}`, /RequestExit|QuitGame|ConsoleCommand.*quit|TerminateProc/);
console.log("Unreal Cymonkey package contract is valid.");

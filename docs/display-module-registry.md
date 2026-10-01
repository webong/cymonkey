# Display module registry

Cymonkey uses a reviewed registry to discover versioned display modules for
browser and render runtimes.

## Lifecycle

```text
discover metadata → filter by policy/platform/protocol → approve
    → pull immutable artifact → verify SHA-256 → mount into caller target
```

Discovery does not execute code. An entry may be metadata-only while a module
is being built; it becomes pullable when it contains an artifact for the
selected platform with an HTTPS URL and SHA-256 digest.

Registry documents use `cymonkey.display-modules/v1alpha1`:

```json
{
  "schemaVersion": "cymonkey.display-modules/v1alpha1",
  "registryId": "approved-display-modules",
  "modules": [
    {
      "id": "render/godot",
      "version": "1.0.0",
      "runtime": "godot",
      "protocolVersion": "cymonkey/v1alpha1",
      "status": "available",
      "platforms": ["linux-amd64"]
    }
  ]
}
```

Use Cymy for discovery and verified download:

```sh
cymy modules discover --registry https://registry.example/cymonkey.json
cymy modules pull \
  --registry https://registry.example/cymonkey.json \
  --module render/godot --platform linux-amd64 \
  --cache /var/cache/cymonkey/display-modules
```

The pull command returns a verified cache path. A host must still explicitly
approve and mount the artifact into a caller-owned target. Pulling never
launches a process, loads a shared library, or mounts a target.

Browser packages follow the same review principle but use the browser package
flow. Native render modules are selected for the target platform.

## Activation

The host re-verifies cached bytes, obtains approval, passes the verified bytes
to the runtime-specific mount operation, and checks `hello`, `capabilities`,
`describe`, and `health`. Failed negotiation disconnects the module. The host
remains responsible for target lifecycle and per-call authorization.

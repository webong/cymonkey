# Unity Cymonkey headless fixture

This source-only Unity 2022.3 project exercises `pkg/unity-cymonkey` in an
operator-supplied Linux Editor container. It deliberately does not depend on a
Unity installation on the developer's Mac.

`Packages/manifest.json` references the package through a local UPM path. The
Editor entrypoint constructs an explicit `object:fixture` registration, checks
all six Cymonkey methods, invokes `object.active.set`, observes the resulting
event, disposes the transport, and verifies that the target survives.

Run the static environment contract without Unity:

```sh
npm run test:unity-cymonkey-fixture
```

Run the real Editor fixture through the separate container after supplying an
approved `UNITY_EDITOR_IMAGE` and activating its Unity license outside the
repository. See `infra/deploy/unity-cymonkey-fixture/README.md`.

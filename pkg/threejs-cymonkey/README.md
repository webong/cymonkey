# Jangolova runtime library for Three.js

`@jangolova/threejs-cymonkey` is Jangolova's runtime library for explicitly
registered Three.js resources. It implements the `render` domain of
`cymonkey/v1alpha1`; it never scans a scene, global variables, or the page for
objects.

```ts
const cymonkey = new ThreeJSCymonkey();
cymonkey.register({
  id: 'camera:main', kind: 'camera', target: camera,
  actions: ['object.transform.set', 'camera.projection.set'],
});
cymonkey.installGlobal();
```

When used in a browser, Cymonkey enters the tab and mounts a reviewed
augmentation package containing this Jangolova library. Cymonkey routes opaque
semantic requests through its private `engine.call` route, scoped to
the augmentation ID. The Browser Extension does not import or otherwise know
about Three.js. This library owns the Three.js semantics and remains protected
by stable IDs and per-resource action allowlists; no render control is added to
the public page API.

## Existing-tab augmentation package

This package also ships a reviewed `augmentation-package` delivery. It creates
and owns a closed-shadow overlay, canvas, Three.js scene, camera, lights, and
objects on an eligible existing HTTP(S) tab. The default launch action mounts
a visible rotating cube; semantic calls can then describe resources, add or
remove boxes and spheres, and modify only explicitly registered objects,
materials, and the camera.

Build the composed extension for Chrome, Edge, Firefox, and Safari with:

```sh
npm run build:threejs-browser-package
```

The resulting extension artifacts contain
`augmentations/threejs/content.js`. The base Browser Extension package still
contains no Three.js dependency or Three.js-specific action.

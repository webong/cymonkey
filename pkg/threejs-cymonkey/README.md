# Three.js Cymonkey

`@jangolova/threejs-cymonkey` implements the `render` domain of
`cymonkey/v1alpha1` for explicitly registered Three.js resources. It
never scans a scene, global variables, or the page for objects.

```ts
const cymonkey = new ThreeJSCymonkey();
cymonkey.register({
  id: 'camera:main', kind: 'camera', target: camera,
  actions: ['object.transform.set', 'camera.projection.set'],
});
cymonkey.installGlobal();
```

When used in a browser, a product-specific augmentation package bundles this
runtime and exposes it through Cymonkey's generic private
`cymonkey-engine.call` route, scoped to that augmentation's ID. The Browser
Extension itself does not import or otherwise know about Three.js. The runtime
remains protected by stable IDs and per-resource action allowlists; no render
control is added to the public page API.

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

# Three.js Cymonkey

`@jangolova/threejs-cymonkey` implements the `render` domain of
`jangolova.cymonkey/v1alpha2` for explicitly registered Three.js resources. It
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
runtime and exposes it through Jangolova's generic private
`cymonkey-engine.call` route, scoped to that augmentation's ID. The Browser
Extension itself does not import or otherwise know about Three.js. The runtime
remains protected by stable IDs and per-resource action allowlists; no render
control is added to the public page API.

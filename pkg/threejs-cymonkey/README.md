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

The Jangolova Browser Extension's private `cymonkey-engine.call` control method
locates that installed runtime in the target tab's MAIN world. The runtime
remains protected by stable IDs and per-resource action allowlists; no render
control is added to the public page API.

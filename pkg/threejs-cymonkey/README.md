# Three.js Cymonkey

`@jangolova/threejs-pacman` implements `jangolova.pacman/v1alpha1` for explicitly
registered Three.js resources. It never scans a scene, global variables, or the
page for objects.

```ts
const pacman = new ThreeJSPacman();
pacman.register({
  id: 'camera:main', kind: 'camera', target: camera,
  actions: ['object.transform.set', 'camera.projection.set'],
});
pacman.installGlobal();
```

The Jangolova Browser Extension's private `pacman.call` control method locates
that installed runtime in the target tab's MAIN world. The runtime remains
protected by stable IDs and per-resource action allowlists; no Pacman control is
added to the public page API.

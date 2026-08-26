# Jangolova Interaction Runtime Image

This optional image packages the Jangolova provider, Node.js, Playwright Core,
Puppeteer Core, and the browser interaction worker. It does not contain
Chromium, a display server, or container/session topology.

Build it locally:

```sh
docker build -f deploy/engine-runtime/Containerfile \
  -t jangolova/engine-runtime:latest .
```

This is a local image name; no registry publication is implied.

The operator supplies `JANGOLOVA_PROVIDER_TOKEN` and places the image where it
can reach caller-owned target endpoints. When Xallet is the operator, Xallet
owns the targets, private application network, secrets, and workload lifecycle.

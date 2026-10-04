#!/usr/bin/env node
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import process from "node:process";

const port = Number.parseInt(process.env.PRESENTATION_TARGET_PORT || "8081", 10);
const root = new URL("../etc/web-presentation/", import.meta.url);
const assets = new Map([
  ["/", { file: new URL("index.html", root), type: "text/html; charset=utf-8" }],
  ["/index.html", { file: new URL("index.html", root), type: "text/html; charset=utf-8" }],
  ["/main.js", { file: new URL("main.js", root), type: "text/javascript; charset=utf-8" }],
]);

const server = createServer(async (request, response) => {
  const pathname = new URL(request.url || "/", "http://127.0.0.1").pathname;
  if (pathname === "/pixel.svg") {
    response.writeHead(200, { "cache-control": "no-store", "content-type": "image/svg+xml" });
    response.end('<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"><rect width="1" height="1" fill="red"/></svg>');
    return;
  }
  const asset = assets.get(pathname);
  if (!asset) {
    response.writeHead(404, { "content-type": "text/plain; charset=utf-8" });
    response.end("not found\n");
    return;
  }
  try {
    const contents = await readFile(asset.file);
    response.writeHead(200, {
      "cache-control": "no-store",
      "content-type": asset.type,
    });
    response.end(contents);
  } catch (error) {
    response.writeHead(500, { "content-type": "text/plain; charset=utf-8" });
    response.end(`${error?.message || error}\n`);
  }
});

server.listen(port, "127.0.0.1", () => {
  process.stdout.write(`presentation target listening on http://127.0.0.1:${port}\n`);
});

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => server.close(() => process.exit(0)));
}

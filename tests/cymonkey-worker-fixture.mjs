import process from "node:process";
import readline from "node:readline";

const lines = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
lines.on("line", (line) => {
  const request = JSON.parse(line);
  let result;
  if (request.method === "connect" || request.method === "reconnect") {
    if (request.params.endpoint !== "wss://browser.remote.example/devtools/browser/42") {
      respond({ id: request.id, error: "fixture received the wrong CDP endpoint" });
      return;
    }
    if (request.params.protocol !== "cdp") {
      respond({ id: request.id, error: "fixture received the wrong backend protocol" });
      return;
    }
    if (request.params.extension !== undefined) {
      respond({ id: request.id, error: "fixture received a retired extension option" });
      return;
    }
    result = request.method === "connect" ? { capabilities: ["script.register"] } : { reconnected: true };
  } else if (request.method === "health") {
    result = { connected: true };
  } else if (request.method === "disconnect") {
    result = { disconnected: true };
  } else {
    result = {};
  }
  respond({ id: request.id, result });
  if (request.method === "disconnect") setImmediate(() => process.exit(0));
});

function respond(value) {
  process.stdout.write(`${JSON.stringify(value)}\n`);
}

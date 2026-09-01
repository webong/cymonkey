const root = document.querySelector("#presentation");
let documentState = { type: "stack", children: [] };
let stateRevision = 0;
const events = [];
let sequence = 0;

const capabilities = [
  capability("presentation.create", "Create a declarative presentation document.", "write", ["document", "expectedStateRevision"]),
  capability("presentation.replace", "Replace the complete presentation document.", "write", ["document", "expectedStateRevision"]),
  capability("presentation.write", "Write HTML, CSS, and JavaScript presentation source.", "write", ["html", "expectedStateRevision"]),
  capability("presentation.mount", "Mount a versioned presentation artifact through an engine-supported location.", "external", ["artifact", "expectedStateRevision"]),
  capability("presentation.execute", "Execute presentation JavaScript against the mounted surface.", "external", ["code"]),
  capability("presentation.patch", "Apply bounded JSON-style document patches.", "write", ["operations", "expectedStateRevision"]),
  capability("presentation.describe", "Describe the current presentation document and viewport.", "read", []),
  capability("presentation.capture", "Capture the rendered presentation through the caller-owned browser.", "read", []),
  capability("presentation.activate", "Activate a presentation button by its semantic id.", "write", ["id"]),
];

function capability(name, description, effect, required) {
  return { name, description, effect, inputSchema: { type: "object", required, additionalProperties: true } };
}

function clone(value) { return JSON.parse(JSON.stringify(value)); }
function currentStateRevision() { return String(stateRevision); }

function requireExpectedStateRevision(input) {
  if (input.expectedStateRevision === undefined || input.expectedStateRevision === null) throw new Error("expectedStateRevision is required");
  const expected = String(input.expectedStateRevision);
  if (expected !== currentStateRevision()) throw new Error(`presentation state revision conflict: expected ${expected}, current ${currentStateRevision()}`);
}

function advanceStateRevision() {
  stateRevision += 1;
  return currentStateRevision();
}

function validateDocument(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("document must be an object");
  if (value.type !== undefined && typeof value.type !== "string") throw new Error("document.type must be a string");
  return clone(value);
}

function mountSource(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("source document must be an object");
  if (typeof value.html !== "string") throw new Error("source document.html must be a string");
  if (value.css !== undefined && typeof value.css !== "string") throw new Error("source document.css must be a string");
  if (value.js !== undefined && typeof value.js !== "string") throw new Error("source document.js must be a string");
  root.innerHTML = value.html;
  let style = document.querySelector("style[data-jangolova-presentation]");
  if (!style) { style = document.createElement("style"); style.dataset.jangolovaPresentation = "true"; document.head.append(style); }
  style.textContent = value.css || "";
  if (value.js) runCode(value.js);
}

function runCode(code) {
  if (typeof code !== "string" || code.length === 0) throw new Error("presentation JavaScript code is required");
  return new Function("root", "emit", code)(root, publishEvent);
}

function childrenOf(node) { return Array.isArray(node.children) ? node.children : []; }

function findNode(node, id) {
  if (node?.id === id) return node;
  for (const child of childrenOf(node)) {
    const found = findNode(child, id);
    if (found) return found;
  }
  return null;
}

function setPath(rootValue, path, value) {
  const parts = String(path || "").split(".").filter(Boolean);
  if (parts.length === 0) throw new Error("patch path is required");
  let target = rootValue;
  for (const part of parts.slice(0, -1)) {
    if (!target || typeof target !== "object") throw new Error(`patch path ${path} is invalid`);
    target = target[part];
  }
  if (!target || typeof target !== "object") throw new Error(`patch path ${path} is invalid`);
  target[parts.at(-1)] = clone(value);
}

function removePath(rootValue, path) {
  const parts = String(path || "").split(".").filter(Boolean);
  if (parts.length === 0) throw new Error("patch path is required");
  let target = rootValue;
  for (const part of parts.slice(0, -1)) target = target?.[part];
  if (!target || typeof target !== "object") throw new Error(`patch path ${path} is invalid`);
  if (Array.isArray(target)) target.splice(Number(parts.at(-1)), 1);
  else delete target[parts.at(-1)];
}

function renderNode(node) {
  const type = node?.type || "stack";
  if (type === "heading") {
    const element = document.createElement(node.level === 1 ? "h1" : node.level === 2 ? "h2" : "h3");
    element.textContent = node.text || "";
    return element;
  }
  if (type === "text") {
    const element = document.createElement("p");
    element.className = "presentation-text";
    element.textContent = node.text || "";
    return element;
  }
  if (type === "button") {
    const element = document.createElement("button");
    element.className = "presentation-button";
    element.type = "button";
    element.dataset.jangolovaId = node.id || "";
    element.textContent = node.label || node.text || "Activate";
    element.addEventListener("click", () => publishEvent("presentation.activate", { id: node.id || null }));
    return element;
  }
  const element = document.createElement(type === "card" ? "section" : "div");
  element.className = type === "card" ? "presentation-card" : "presentation-stack";
  for (const child of childrenOf(node)) element.append(renderNode(child));
  return element;
}

function render(value = documentState) {
  if (typeof value.html === "string") { mountSource(value); return; }
  root.replaceChildren(renderNode(value));
}

function describe() {
  return {
    engine: "jangolova-web-presentation",
    stateRevision: currentStateRevision(),
    document: clone(documentState),
    viewport: { width: innerWidth, height: innerHeight, devicePixelRatio },
  };
}

function act(name, input = {}) {
  if (name === "presentation.create" || name === "presentation.replace") {
    requireExpectedStateRevision(input);
    if (name === "presentation.create" && stateRevision !== 0) throw new Error("presentation.create requires an empty state revision");
    const next = input.document?.html !== undefined ? clone(input.document) : validateDocument(input.document);
    render(next);
    documentState = next;
    const nextRevision = advanceStateRevision();
    publishEvent(name, { stateRevision: nextRevision });
    return { ok: true, stateRevision: nextRevision };
  }
  if (name === "presentation.write") {
    requireExpectedStateRevision(input);
    const next = clone({ html: input.html, css: input.css || "", js: input.js || "" });
    mountSource(next);
    documentState = next;
    const nextRevision = advanceStateRevision();
    publishEvent(name, { bytes: new TextEncoder().encode(documentState.html).byteLength, stateRevision: nextRevision });
    return { ok: true, stateRevision: nextRevision };
  }
  if (name === "presentation.execute") {
    const result = runCode(input.code);
    publishEvent(name, {});
    return { ok: true, result: result === undefined ? null : result };
  }
  if (name === "presentation.patch") {
    requireExpectedStateRevision(input);
    if (!Array.isArray(input.operations)) throw new Error("presentation.patch operations must be an array");
    const next = clone(documentState);
    for (const operation of input.operations) {
      if (operation.op === "set") setPath(next, operation.path, operation.value);
      else if (operation.op === "remove") removePath(next, operation.path);
      else if (operation.op === "append") {
        const target = operation.path ? findNode(next, operation.path) : next;
        if (!target || !Array.isArray(target.children)) throw new Error("append target must have children");
        target.children.push(clone(operation.value));
      } else throw new Error(`unsupported presentation patch operation ${operation.op}`);
    }
    const validated = validateDocument(next);
    render(validated);
    documentState = validated;
    const nextRevision = advanceStateRevision();
    publishEvent(name, { count: input.operations.length, stateRevision: nextRevision });
    return { ok: true, stateRevision: nextRevision };
  }
  if (name === "presentation.describe") return describe();
  if (name === "presentation.activate") {
    const node = findNode(documentState, input.id);
    if (!node || node.type !== "button") throw new Error(`button ${JSON.stringify(input.id)} does not exist`);
    publishEvent("presentation.activate", { id: input.id });
    return { ok: true, id: input.id, action: node.action || null };
  }
  throw new Error(`unsupported presentation capability ${JSON.stringify(name)}`);
}

function publishEvent(type, data = {}) {
  sequence += 1;
  events.push({ id: String(sequence), type, occurredAt: new Date().toISOString(), data });
  if (events.length > 256) events.splice(0, events.length - 256);
}

function readEvents({ after = "0", types = [], limit = 100 } = {}) {
  const afterSequence = Number.parseInt(after || "0", 10);
  const selectedTypes = new Set(Array.isArray(types) ? types : []);
  const maximum = Math.min(Math.max(Number(limit) || 100, 1), 256);
  const selected = events.filter((event) => Number(event.id) > afterSequence && (selectedTypes.size === 0 || selectedTypes.has(event.type))).slice(0, maximum);
  return { events: selected, cursor: String(sequence) };
}

(window.cymonkey ??= {}).jangolova = {
  hello: () => ({ protocolVersion: "jangolova.bridge/v1alpha1", implementation: { name: "jangolova-web-presentation" }, features: ["presentation.document", "presentation.artifact.mount", "events.cursor"] }),
  capabilities: () => capabilities,
  describe,
  act,
  events: readEvents,
};

addEventListener("resize", () => publishEvent("presentation.resize", { width: innerWidth, height: innerHeight }));

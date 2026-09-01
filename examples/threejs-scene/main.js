import * as THREE from "/vendor/three.module.js";

const canvas = document.querySelector("#experience");
const summary = document.querySelector("#summary");
const renderer = new THREE.WebGLRenderer({ canvas, antialias: true });
renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
renderer.setSize(innerWidth, innerHeight, false);
renderer.outputColorSpace = THREE.SRGBColorSpace;

const scene = new THREE.Scene();
scene.background = new THREE.Color("#080b14");

const camera = new THREE.PerspectiveCamera(48, innerWidth / innerHeight, 0.1, 100);
camera.position.set(4, 3, 6);
const cameraTarget = new THREE.Vector3(0, 0, 0);
camera.lookAt(cameraTarget);

scene.add(new THREE.AmbientLight("#9fb6ff", 1.5));
const keyLight = new THREE.DirectionalLight("#ffffff", 3);
keyLight.position.set(4, 7, 5);
scene.add(keyLight);

const grid = new THREE.GridHelper(20, 20, "#26325a", "#151c36");
grid.position.y = -1.5;
scene.add(grid);

const objects = new Map();
const events = [];
let eventSequence = 0;
const raycaster = new THREE.Raycaster();
const pointer = new THREE.Vector2();

const vectorSchema = {
  type: "array",
  items: { type: "number" },
  minItems: 3,
  maxItems: 3,
};

const capabilities = [
  {
    name: "scene.describe",
    description: "Describe the current Three.js scene, camera, and animations.",
    inputSchema: { type: "object", additionalProperties: false },
    effect: "read",
  },
  {
    name: "object.create",
    description: "Create a box, sphere, or plane in the scene.",
    inputSchema: {
      type: "object",
      properties: {
        id: { type: "string" },
        type: { enum: ["box", "sphere", "plane"] },
        color: { type: "string" },
        position: vectorSchema,
        rotation: vectorSchema,
        scale: vectorSchema,
      },
      required: ["id", "type"],
      additionalProperties: false,
    },
    effect: "write",
  },
  {
    name: "object.update",
    description: "Update an existing scene object's transform or color.",
    inputSchema: {
      type: "object",
      properties: {
        id: { type: "string" },
        color: { type: "string" },
        position: vectorSchema,
        rotation: vectorSchema,
        scale: vectorSchema,
      },
      required: ["id"],
      additionalProperties: false,
    },
    effect: "write",
  },
  {
    name: "object.remove",
    description: "Remove an object from the scene.",
    inputSchema: {
      type: "object",
      properties: { id: { type: "string" } },
      required: ["id"],
      additionalProperties: false,
    },
    effect: "write",
  },
  {
    name: "camera.update",
    description: "Update the presentation camera position and target.",
    inputSchema: {
      type: "object",
      properties: { position: vectorSchema, target: vectorSchema },
      additionalProperties: false,
    },
    effect: "write",
  },
  {
    name: "animation.start",
    description: "Set an object's rotation speed in radians per second.",
    inputSchema: {
      type: "object",
      properties: { id: { type: "string" }, rotationSpeed: vectorSchema },
      required: ["id", "rotationSpeed"],
      additionalProperties: false,
    },
    effect: "write",
  },
];

function requireObject(id) {
  const record = objects.get(id);
  if (!record) throw new Error(`scene object ${JSON.stringify(id)} does not exist`);
  return record;
}

function vector(value, fallback) {
  if (value === undefined) {
    if (fallback === undefined) throw new Error("expected a three-number vector");
    return fallback;
  }
  if (!Array.isArray(value) || value.length !== 3 || value.some((item) => !Number.isFinite(item))) {
    throw new Error("expected a three-number vector");
  }
  return value;
}

function geometry(type) {
  switch (type) {
    case "box":
      return new THREE.BoxGeometry(2, 2, 2);
    case "sphere":
      return new THREE.SphereGeometry(1.25, 48, 32);
    case "plane":
      return new THREE.PlaneGeometry(3, 3);
    default:
      throw new Error(`unsupported object type ${JSON.stringify(type)}`);
  }
}

function applyTransform(object, input) {
  object.position.fromArray(vector(input.position, object.position.toArray()));
  object.rotation.fromArray([...vector(input.rotation, [
    object.rotation.x,
    object.rotation.y,
    object.rotation.z,
  ]), object.rotation.order]);
  object.scale.fromArray(vector(input.scale, object.scale.toArray()));
}

function serialize(record) {
  return {
    id: record.id,
    type: record.type,
    color: `#${record.object.material.color.getHexString()}`,
    position: record.object.position.toArray(),
    rotation: [record.object.rotation.x, record.object.rotation.y, record.object.rotation.z],
    scale: record.object.scale.toArray(),
    rotationSpeed: record.rotationSpeed.toArray(),
  };
}

function describe() {
  return {
    engine: "three.js",
    version: THREE.REVISION,
    objects: [...objects.values()].map(serialize),
    camera: {
      position: camera.position.toArray(),
      target: cameraTarget.toArray(),
      fieldOfView: camera.fov,
    },
    viewport: {
      width: renderer.domElement.width,
      height: renderer.domElement.height,
      pixelRatio: renderer.getPixelRatio(),
    },
  };
}

function act(name, input = {}) {
  switch (name) {
    case "scene.describe":
      return describe();
    case "object.create": {
      if (!input.id || typeof input.id !== "string") throw new Error("object.create id is required");
      if (objects.has(input.id)) throw new Error(`scene object ${JSON.stringify(input.id)} already exists`);
      const material = new THREE.MeshStandardMaterial({
        color: input.color || "#7c5cff",
        roughness: 0.36,
        metalness: 0.18,
      });
      const object = new THREE.Mesh(geometry(input.type), material);
      object.userData.jangolovaID = input.id;
      applyTransform(object, input);
      scene.add(object);
      const record = {
        id: input.id,
        type: input.type,
        object,
        rotationSpeed: new THREE.Vector3(),
      };
      objects.set(input.id, record);
      updateSummary();
      return { ok: true, object: serialize(record) };
    }
    case "object.update": {
      const record = requireObject(input.id);
      applyTransform(record.object, input);
      if (input.color !== undefined) record.object.material.color.set(input.color);
      return { ok: true, object: serialize(record) };
    }
    case "object.remove": {
      const record = requireObject(input.id);
      scene.remove(record.object);
      record.object.geometry.dispose();
      record.object.material.dispose();
      objects.delete(input.id);
      updateSummary();
      return { ok: true, id: input.id };
    }
    case "camera.update":
      camera.position.fromArray(vector(input.position, camera.position.toArray()));
      cameraTarget.fromArray(vector(input.target, cameraTarget.toArray()));
      camera.lookAt(cameraTarget);
      return { ok: true, camera: describe().camera };
    case "animation.start": {
      const record = requireObject(input.id);
      record.rotationSpeed.fromArray(vector(input.rotationSpeed));
      return { ok: true, object: serialize(record) };
    }
    default:
      throw new Error(`unsupported scene capability ${JSON.stringify(name)}`);
  }
}

function updateSummary() {
  const count = objects.size;
  summary.textContent = `${count} dynamic object${count === 1 ? "" : "s"} in scene`;
}

function publishEvent(type, data = {}) {
  eventSequence += 1;
  events.push({
    id: String(eventSequence),
    type,
    occurredAt: new Date().toISOString(),
    data,
  });
  if (events.length > 256) events.splice(0, events.length - 256);
}

function readEvents({ after = "0", types = [], limit = 100 } = {}) {
  const afterSequence = Number.parseInt(after || "0", 10);
  if (!Number.isSafeInteger(afterSequence) || afterSequence < 0) {
    throw new Error("events.after must be a non-negative integer cursor");
  }
  if (!Array.isArray(types) || types.some((type) => typeof type !== "string")) {
    throw new Error("events.types must be an array of strings");
  }
  if (!Number.isInteger(limit) || limit < 0 || limit > 1000) {
    throw new Error("events.limit must be between 0 and 1000");
  }
  const selectedTypes = new Set(types);
  const maximum = limit || 100;
  const selectedEvents = [];
  let cursor = afterSequence;
  for (const event of events) {
    const sequence = Number(event.id);
    if (sequence <= afterSequence) continue;
    cursor = sequence;
    if (selectedTypes.size === 0 || selectedTypes.has(event.type)) {
      selectedEvents.push(event);
      if (selectedEvents.length >= maximum) break;
    }
  }
  return {
    events: selectedEvents,
    cursor: String(Math.max(cursor, afterSequence)),
  };
}

(window.cymonkey ??= {}).jangolova = {
  hello: () => ({
    protocolVersion: "jangolova.bridge/v1alpha1",
    implementation: {
      name: "jangolova-threejs-scene",
      version: THREE.REVISION,
    },
    features: ["events.cursor"],
  }),
  capabilities: () => capabilities,
  describe,
  act,
  events: readEvents,
};

canvas.addEventListener("pointerdown", (event) => {
  const rect = canvas.getBoundingClientRect();
  pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
  pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
  raycaster.setFromCamera(pointer, camera);
  const selected = raycaster.intersectObjects(
    [...objects.values()].map((record) => record.object),
    false,
  )[0];
  publishEvent("pointer.select", {
    objectId: selected?.object.userData.jangolovaID || null,
    x: event.clientX - rect.left,
    y: event.clientY - rect.top,
  });
});

addEventListener("resize", () => {
  camera.aspect = innerWidth / innerHeight;
  camera.updateProjectionMatrix();
  renderer.setSize(innerWidth, innerHeight, false);
});

const clock = new THREE.Clock();
renderer.setAnimationLoop(() => {
  const delta = Math.min(clock.getDelta(), 0.1);
  for (const record of objects.values()) {
    record.object.rotation.x += record.rotationSpeed.x * delta;
    record.object.rotation.y += record.rotationSpeed.y * delta;
    record.object.rotation.z += record.rotationSpeed.z * delta;
  }
  renderer.render(scene, camera);
});

updateSummary();

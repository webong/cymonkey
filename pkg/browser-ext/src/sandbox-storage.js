export function installEphemeralWebStorage(target = globalThis) {
  for (const name of ['localStorage', 'sessionStorage']) {
    try {
      if (target[name]) continue;
    } catch {
      // Opaque-origin extension sandboxes throw from the native getter.
    }
    Object.defineProperty(target, name, {
      configurable: true,
      value: createMemoryStorage(),
    });
  }
}

export function createMemoryStorage() {
  const values = new Map();
  return {
    get length() { return values.size; },
    clear() { values.clear(); },
    getItem(key) { return values.get(String(key)) ?? null; },
    key(index) { return [...values.keys()][Number(index)] ?? null; },
    removeItem(key) { values.delete(String(key)); },
    setItem(key, value) { values.set(String(key), String(value)); },
  };
}

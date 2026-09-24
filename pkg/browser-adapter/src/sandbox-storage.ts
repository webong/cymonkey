// Opaque-origin sandbox pages can have throwing native storage getters. A
// caller may opt into ephemeral Storage-shaped memory for such a page.
export function installEphemeralWebStorage(target: Record<string, unknown> = globalThis as Record<string, unknown>) {
  for (const name of ['localStorage', 'sessionStorage']) {
    try { if (target[name]) continue; } catch { /* opaque-origin getter */ }
    Object.defineProperty(target, name, {configurable: true, value: createMemoryStorage()});
  }
}

export function createMemoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() { return values.size; },
    clear() { values.clear(); },
    getItem(key) { return values.get(String(key)) ?? null; },
    key(index) { return [...values.keys()][Number(index)] ?? null; },
    removeItem(key) { values.delete(String(key)); },
    setItem(key, value) { values.set(String(key), String(value)); },
  };
}

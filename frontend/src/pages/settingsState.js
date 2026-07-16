export function createMirrorTestState(initial = {}) {
  const results = { ...initial };
  const requests = new Map();
  async function run(tool, url, request) {
    const requestId = (requests.get(tool) || 0) + 1;
    requests.set(tool, requestId);
    try {
      const result = await request();
      if (requests.get(tool) === requestId) results[tool] = { ...result, url, requestId, lastChecked: new Date().toISOString() };
    } catch (error) {
      if (requests.get(tool) === requestId) results[tool] = { success: false, message: String(error), url, requestId, lastChecked: new Date().toISOString() };
    }
    return results[tool];
  }
  return { results, run };
}

export function createLatestRequestController() {
  const pending = {};
  const requests = new Map();
  async function run(key, input, request) {
    const requestId = (requests.get(key) || 0) + 1;
    requests.set(key, requestId);
    pending[key] = true;
    const snapshot = structuredClone(input);
    try {
      const value = await request(snapshot);
      return { current: requests.get(key) === requestId, input: snapshot, value };
    } catch (error) {
      return { current: requests.get(key) === requestId, input: snapshot, error };
    } finally {
      if (requests.get(key) === requestId) delete pending[key];
    }
  }
  return { pending, run };
}

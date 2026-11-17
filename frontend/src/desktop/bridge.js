// The desktop seam normalises Wails 3 event envelopes and window operations.
// Tests pass explicit adapters; unknown calls are never silently successful.
export function createDesktopBridge(bindings, { Events, Window, Application }) {
  if (!bindings || !Events || !Window || !Application) {
    throw new Error('Desktop bindings and runtime are required');
  }
  const subscriptions = new Map();
  const runtime = {
    EventsOn(name, callback) {
      const off = Events.On(name, event => callback(event.data));
      const owned = subscriptions.get(name) || new Set();
      let disposed = false;
      const unsubscribe = () => {
        if (disposed) return;
        disposed = true;
        off();
        owned.delete(unsubscribe);
        if (!owned.size) subscriptions.delete(name);
      };
      owned.add(unsubscribe);
      subscriptions.set(name, owned);
      return unsubscribe;
    },
    EventsOff(...names) {
      for (const name of names) {
        for (const off of [...(subscriptions.get(name) || [])]) off();
      }
    },
    Quit: () => Application.Quit(),
    WindowClose: () => Window.Close(),
    WindowIsMaximised: () => Window.IsMaximised(),
    WindowMinimise: () => Window.Minimise(),
    WindowToggleMaximise: () => Window.ToggleMaximise(),
  };
  return { app: bindings, runtime };
}

// A compatibility namespace keeps existing pages and external browser fixtures
// stable while the generated v3 bindings remain the sole production caller.
export function installDesktopBridge(target, bridge) {
  target.go = { gui: { App: bridge.app } };
  target.runtime = bridge.runtime;
}

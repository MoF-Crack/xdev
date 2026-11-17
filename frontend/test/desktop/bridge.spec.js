import { describe, it, expect, vi } from 'vitest';
import { createDesktopBridge, installDesktopBridge } from '../../src/desktop/bridge.js';

function fixture() {
  const handlers = new Map();
  const Events = { On: vi.fn((name, cb) => { const set = handlers.get(name) || new Set(); set.add(cb); handlers.set(name, set); return () => set.delete(cb); }) };
  const Window = { Close: vi.fn(), IsMaximised: vi.fn(async () => true), Minimise: vi.fn(), ToggleMaximise: vi.fn() };
  const Application = { Quit: vi.fn() };
  const app = { StartInstall: vi.fn() };
  return { handlers, Events, Window, Application, app, bridge: createDesktopBridge(app, { Events, Window, Application }) };
}
describe('Wails 3 desktop adapter', () => {
  it('unwraps the event envelope and keeps false/null payloads exact', () => {
    const f = fixture(), listener = vi.fn();
    f.bridge.runtime.EventsOn('event', listener);
    for (const cb of f.handlers.get('event')) { cb({ name: 'event', data: null }); cb({ name: 'event', data: false }); }
    expect(listener.mock.calls).toEqual([[null], [false]]);
  });
  it('owns individual subscriptions without deleting other sessions', () => {
    const f = fixture(), a = vi.fn(), b = vi.fn();
    const offA = f.bridge.runtime.EventsOn('session', a);
    f.bridge.runtime.EventsOn('session', b);
    offA(); offA();
    for (const cb of f.handlers.get('session')) cb({data: {id: 'b'}});
    expect(a).not.toHaveBeenCalled(); expect(b).toHaveBeenCalledOnce();
    f.bridge.runtime.EventsOff('session');
    expect(f.handlers.get('session').size).toBe(0);
  });
  it('exposes only actual generated bindings and routes window methods', async () => {
    const f = fixture(), target = {};
    installDesktopBridge(target, f.bridge);
    expect(target.go.gui.App).toBe(f.app);
    expect(target.go.gui.App.UnknownMethod).toBeUndefined();
    expect(await target.runtime.WindowIsMaximised()).toBe(true);
    target.runtime.Quit(); target.runtime.WindowMinimise(); target.runtime.WindowClose();
    expect(f.Application.Quit).toHaveBeenCalledOnce();
    expect(f.Window.Minimise).toHaveBeenCalledOnce(); expect(f.Window.Close).toHaveBeenCalledOnce();
  });
});

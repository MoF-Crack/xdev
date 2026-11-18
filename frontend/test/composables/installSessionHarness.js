import { vi } from 'vitest';
import { installWailsMocks } from '../mocks/wails.js';

// Explicitly driven backend: acceptance never schedules a hidden completion.
export function installSessionHarness(overrides = {}) {
  const listeners = new Map();
  const snapshots = new Map();
  function emit(id, patch) {
    const previous = snapshots.get(id) || { sessionId: id, revision: 0 };
    const snapshot = { ...previous, revision: previous.revision + 1, ...patch };
    snapshots.set(id, snapshot);
    for (const fn of listeners.get('installSession') || []) fn(snapshot);
    return snapshot;
  }
  const app = {
    StartInstall: vi.fn(async spec => {
      emit(spec.sessionId, { ...spec, status: 'accepted', phase: 'Preparing', canCancel: true, canPause: false });
      return spec.sessionId;
    }),
    GetInstallSession: vi.fn(async id => { if (!snapshots.has(id)) throw new Error('not found'); return snapshots.get(id); }),
    PauseDownload: vi.fn(async id => emit(id, { phase: 'Paused', paused: true })),
    ResumeDownload: vi.fn(async id => emit(id, { phase: 'Downloading', paused: false })),
    CancelInstall: vi.fn(async id => emit(id, { status: 'cancelling', canCancel: false, canPause: false })),
    ...overrides,
  };
  const runtime = {
    EventsOn: vi.fn((name, fn) => {
      if (!listeners.has(name)) listeners.set(name, new Set());
      listeners.get(name).add(fn);
      return () => listeners.get(name).delete(fn);
    }), EventsOff: vi.fn(),
  };
  installWailsMocks(app, runtime);
  return { app, runtime, listeners, snapshots, emit };
}

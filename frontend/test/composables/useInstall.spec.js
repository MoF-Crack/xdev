import { effectScope, nextTick } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { cleanupWailsMocks, installWailsMocks } from '../mocks/wails.js';
import { useInstall } from '../../src/composables/useInstall.js';

afterEach(cleanupWailsMocks);

function setup(overrides = {}) {
  const listeners = {};
  const app = {
    InstallTool: vi.fn(async () => null), PauseDownload: vi.fn(async () => null),
    ResumeDownload: vi.fn(async () => null), CancelDownload: vi.fn(async () => null), ...overrides,
  };
  const runtime = {
    EventsOn: vi.fn((name, callback) => { (listeners[name] ||= new Set()).add(callback); return () => listeners[name].delete(callback); }),
    EventsOff: vi.fn((name) => { delete listeners[name]; }),
  };
  installWailsMocks(app, runtime);
  return { app, runtime, listeners };
}

describe('useInstall', () => {
  it('registers listeners once and disposes them idempotently', () => {
    const { runtime } = setup();
    const install = useInstall();
    expect(runtime.EventsOn.mock.calls.map(([name]) => name)).toEqual(['installProgress', 'installError']);
    install.dispose(); install.dispose();
    expect(runtime.EventsOff).not.toHaveBeenCalled();
  });

  it('cleans listeners when its component scope is stopped', () => {
    const { runtime } = setup();
    const scope = effectScope();
    scope.run(() => useInstall());
    scope.stop();
    expect(runtime.EventsOff).not.toHaveBeenCalled();
  });

  it('guards concurrent installs, ignores stale task events, and supports controls', async () => {
    let finish;
    const { app, listeners } = setup({ InstallTool: vi.fn(() => new Promise((resolve) => { finish = resolve; })) });
    const state = useInstall();
    const first = state.install('node', '22');
    expect(await state.install('go', '1.24')).toBe(false);
    const emit = (name, data) => listeners[name].forEach((listener) => listener(data));
    emit('installProgress', { taskId: 'go@1.24', percent: 90, phase: 'Downloading' });
    expect(state.progress.value.percent).toBe(0);
    emit('installProgress', { taskId: 'node@22', percent: 40, phase: 'Downloading', bytes: 400, totalBytes: 1000, speed: '2 MB/s' });
    expect(state.progress.value.percent).toBe(40);
    await state.pause(); await state.resume(); await state.cancel();
    expect(app.PauseDownload).toHaveBeenCalledWith('node@22');
    expect(app.ResumeDownload).toHaveBeenCalledWith('node@22');
    expect(app.CancelDownload).toHaveBeenCalledWith('node@22');
    finish(); await first;
  });

  it('recovers state after an API error so another install can start', async () => {
    const { app } = setup({ InstallTool: vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(null) });
    const state = useInstall();
    await expect(state.install('node', '22')).resolves.toBe(false);
    expect(state.error.value.message).toContain('offline');
    expect(state.phase.value).toBe('error');
    await expect(state.install('go', '1.24')).resolves.toBe(true);
    expect(app.InstallTool).toHaveBeenCalledTimes(2);
  });

  it('reports completion, event errors, and cancellation with the current task payload', async () => {
    const { listeners } = setup();
    const onComplete = vi.fn();
    const onError = vi.fn();
    const onCancel = vi.fn();
    const state = useInstall({ onComplete, onError, onCancel });
    await state.install('node', '22');
    listeners.installProgress.forEach((fn) => fn({ taskId: 'node@22', tool: 'node', version: '22', phase: 'Completed', percent: 100 }));
    expect(onComplete).toHaveBeenCalledWith(expect.objectContaining({ tool: 'node', version: '22' }));
    await state.install('go', '1.24');
    listeners.installError.forEach((fn) => fn({ taskId: 'go@1.24', tool: 'go', error: 'offline' }));
    expect(onError).toHaveBeenCalledWith(expect.objectContaining({ tool: 'go', error: 'offline' }));
    await state.install('java', '21');
    await state.cancel();
    expect(onCancel).toHaveBeenCalledWith(expect.objectContaining({ taskId: 'java@21', tool: 'java', version: '21' }));
  });

  it('uses exact task identity and disposing one instance preserves another', async () => {
    const { listeners } = setup();
    const first = useInstall(); const second = useInstall();
    await first.install('node', '22'); await second.install('go', '1.24');
    first.dispose();
    listeners.installProgress.forEach((fn) => fn({ tool: 'go', version: '1.24', percent: 55 }));
    expect(first.progress.value.percent).toBe(0);
    expect(second.progress.value.percent).toBe(55);
    listeners.installProgress.forEach((fn) => fn({ tool: 'node', version: '22', percent: 99 }));
    expect(second.progress.value.percent).toBe(55);
  });
});

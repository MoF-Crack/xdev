import { effectScope, nextTick } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { cleanupWailsMocks, installWailsMocks } from '../mocks/wails.js';
import { useInstall } from '../../src/composables/useInstall.js';

afterEach(cleanupWailsMocks);

function setup(overrides = {}) {
  const listeners = {};
  const app = {
    InstallTool: vi.fn(async () => null), InstallToolWithForm: vi.fn(async () => null), InstallToolConfirmed: vi.fn(async () => null), PauseDownload: vi.fn(async () => null),
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

  it('uses the form-specific install API when a form filter is supplied', async () => {
    const { app } = setup();
    const state = useInstall();
    await expect(state.install('opencode', '1.2.3', 'gui')).resolves.toBe(true);
    expect(app.InstallToolWithForm).toHaveBeenCalledWith('opencode', '1.2.3', 'gui', false);
    expect(app.InstallTool).not.toHaveBeenCalled();
  });

  it('forwards confirmed=true through the form-specific install API', async () => {
    const { app } = setup();
    const state = useInstall();
    await expect(state.installConfirmed('opencode', '1.2.3', 'cli')).resolves.toBe(true);
    expect(app.InstallToolWithForm).toHaveBeenCalledWith('opencode', '1.2.3', 'cli', true);
    expect(app.InstallToolConfirmed).not.toHaveBeenCalled();
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

  it('resets the bar to 0 on a new attempt or fallback status, keeps phase text', async () => {
    const { listeners } = setup();
    const state = useInstall();
    await state.install('node', '22');
    const emit = (data) => listeners.installProgress.forEach((fn) => fn(data));
    emit({ taskId: 'node@22', percent: 97, phase: 'Downloading...', speed: '1.2 MB/s' });
    expect(state.progress.value.percent).toBe(97);
    // First attempt of a new URL → hard reset to 0 (backend emits percent 0).
    emit({ taskId: 'node@22', percent: 0, phase: 'Attempt 1/3 for github.com' });
    expect(state.progress.value.percent).toBe(0);
    expect(state.phase.value).toBe('Attempt 1/3 for github.com');
    // Byte progress resumes afterwards.
    emit({ taskId: 'node@22', percent: 10, phase: 'Downloading...' });
    expect(state.progress.value.percent).toBe(10);
  });

  it('preserves the bar on in-URL retries (percent < 0 sentinel)', async () => {
    const { listeners } = setup();
    const state = useInstall();
    await state.install('node', '22');
    const emit = (data) => listeners.installProgress.forEach((fn) => fn(data));
    emit({ taskId: 'node@22', percent: 97, phase: 'Downloading...', speed: '1.2 MB/s', eta: '5s' });
    expect(state.progress.value.percent).toBe(97);
    // Status-only update (Attempt 2/3…): the whole progress is preserved,
    // only the phase changes.
    emit({ taskId: 'node@22', percent: -1, phase: 'Attempt 2/3 for github.com' });
    expect(state.progress.value.percent).toBe(97);
    expect(state.progress.value.speed).toBe('1.2 MB/s');
    expect(state.progress.value.eta).toBe('5s');
    expect(state.phase.value).toBe('Attempt 2/3 for github.com');
  });

  // --- Spec 035 F2a: waitForCompletion waiter + race guards ---

  it('waitForCompletion resolves completed on the Completed event', async () => {
    const { listeners } = setup();
    const state = useInstall();
    await state.install('node', '22');
    const pending = state.waitForCompletion();
    listeners.installProgress.forEach((fn) => fn({ taskId: 'node@22', phase: 'Completed', percent: 100 }));
    await expect(pending).resolves.toBe('completed');
  });

  it('waitForCompletion resolves error on the installError event', async () => {
    const { listeners } = setup();
    const state = useInstall();
    await state.install('go', '1.24');
    const pending = state.waitForCompletion();
    listeners.installError.forEach((fn) => fn({ taskId: 'go@1.24', error: 'x' }));
    await expect(pending).resolves.toBe('error');
    expect(state.error.value.message).toBe('x');
  });

  it('waitForCompletion resolves cancelled after cancel()', async () => {
    const { app } = setup();
    const state = useInstall();
    await state.install('java', '21');
    const pending = state.waitForCompletion();
    await state.cancel();
    await expect(pending).resolves.toBe('cancelled');
    expect(app.CancelDownload).toHaveBeenCalledWith('java@21');
  });

  it('a cancel ack event resolves cancelled once and suppresses the error toast', async () => {
    // Race: user presses Cancel; before the IPC returns, the backend
    // already emits installError("下载已取消"). The event must resolve
    // the waiter as 'cancelled' and NOT surface an error/reportError.
    const { listeners, app } = setup({
      CancelDownload: vi.fn(() => new Promise((resolve) => { setTimeout(resolve, 10); })),
    });
    const onError = vi.fn();
    const state = useInstall({ onError });
    await state.install('java', '21');
    const pending = state.waitForCompletion();
    const cancelPromise = state.cancel();
    listeners.installError.forEach((fn) => fn({ taskId: 'java@21', error: '下载已取消' }));
    await cancelPromise;
    await expect(pending).resolves.toBe('cancelled');
    expect(state.error.value).toBeNull();
    expect(onError).not.toHaveBeenCalled();
    expect(app.CancelDownload).toHaveBeenCalled();
  });

  it('an error arriving after cancel completed does not overwrite lastResult', async () => {
    const { listeners } = setup();
    const state = useInstall();
    await state.install('java', '21');
    await state.cancel();
    // Late event after active=false — dropped by the guard entirely.
    listeners.installError.forEach((fn) => fn({ taskId: 'java@21', error: 'late' }));
    await expect(state.waitForCompletion()).resolves.toBe('cancelled');
  });

  it('waitForCompletion returns the recorded result when the event landed first', async () => {
    // Fast path: already-installed versions emit Completed synchronously,
    // BEFORE waitForCompletion registers — lastResult must cover it.
    const { listeners } = setup();
    const state = useInstall();
    await state.install('node', '22');
    listeners.installProgress.forEach((fn) => fn({ taskId: 'node@22', phase: 'Completed', percent: 100 }));
    await expect(state.waitForCompletion()).resolves.toBe('completed');
  });

  it('dispose releases a pending waiter as disposed', async () => {
    const { listeners } = setup();
    const scope = effectScope();
    let state;
    scope.run(() => { state = useInstall(); });
    await state.install('node', '22');
    const pending = state.waitForCompletion();
    scope.stop();
    // dispose notified the waiter BEFORE unsubscribing, so it resolves
    // 'disposed' even though no further events can arrive.
    expect(listeners.installProgress).toBeDefined();
    await expect(pending).resolves.toBe('disposed');
  });

  it('waitForCompletion with nothing recorded resolves idle', async () => {
    const state = useInstall();
    await expect(state.waitForCompletion()).resolves.toBe('idle');
  });
});

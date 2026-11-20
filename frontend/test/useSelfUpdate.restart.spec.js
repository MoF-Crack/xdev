import { describe, expect, it, vi } from 'vitest';
import { useSelfUpdate } from '../src/composables/useSelfUpdate.js';

const available = { current: '1.0.0-beta.3', latest: '1.0.0-rc.2', platform: 'windows/amd64', assetName: 'xdev-windows-amd64-v1.0.0-rc.2.exe', hasUpdate: true, applied: false, ready: false, restarting: false };
const ready = { ...available, ready: true };
function fixture(overrides = {}) {
  const backend = { CheckForUpdate: vi.fn().mockResolvedValue(available), PrepareUpdate: vi.fn().mockResolvedValue(ready), ApplyUpdate: vi.fn(), RestartToUpdate: vi.fn().mockResolvedValue({ ...ready, restarting: true }), IgnoreSelfUpdate: vi.fn(), ...overrides };
  const runtime = { WindowClose: vi.fn() };
  const message = { success: vi.fn(), info: vi.fn(), error: vi.fn() };
  const state = useSelfUpdate({ app: () => backend, runtime: () => runtime, message, t: key => key });
  return { backend, runtime, message, state };
}
function deferred() { let resolve; const promise = new Promise(r => { resolve = r; }); return { promise, resolve }; }

describe('download then confirmed restart', () => {
  it('stages without quitting, retains ready across close/reopen, and only confirms restart once', async () => {
    const { state, backend, runtime, message } = fixture();
    await state.check(); expect(state.downloadOnly.value).toBe(true);
    await state.apply(); expect(state.status.value).toBe('ready');
    expect(state.info.value.applied).toBe(false);
    expect(backend.ApplyUpdate).not.toHaveBeenCalled();
    expect(backend.RestartToUpdate).not.toHaveBeenCalled();
    expect(runtime.WindowClose).not.toHaveBeenCalled();
    expect(message.success).not.toHaveBeenCalled();
    expect(state.close()).toBe(true); await state.check(); await state.apply();
    expect(state.status.value).toBe('ready');
    expect(backend.CheckForUpdate).toHaveBeenCalledTimes(1);
    expect(backend.PrepareUpdate).toHaveBeenCalledTimes(1);
    await state.restart(); await state.restart(); await state.check();
    expect(backend.RestartToUpdate).toHaveBeenCalledTimes(1);
    expect(state.status.value).toBe('restarting');
    expect(state.close()).toBe(false); expect(state.quit()).toBe(false);
    expect(runtime.WindowClose).not.toHaveBeenCalled();
  });
  it('never treats completed or ready progress as preparation confirmation', async () => {
    const response = deferred();
    const { state } = fixture({ PrepareUpdate: vi.fn().mockReturnValue(response.promise) });
    await state.check(); const pending = state.apply();
    for (const phase of ['completed', 'ready']) {
      state.handleEvent({ ...available, action: 'progress', source: 'manual', phase, percent: 100, ready: true });
      expect(state.status.value).toBe('updating'); expect(state.info.value.ready).toBe(false);
    }
    response.resolve(ready); await pending; expect(state.status.value).toBe('ready');
  });
  it('retains a staged update when helper cannot start and retries without another download', async () => {
    const restart = vi.fn().mockResolvedValueOnce({ ...ready, error: 'helper unavailable' }).mockResolvedValueOnce({ ...ready, restarting: true });
    const { state, backend, runtime } = fixture({ RestartToUpdate: restart });
    await state.check(); await state.apply();
    expect(await state.restart()).toBe(false); expect(state.status.value).toBe('ready');
    expect(state.error.value).toEqual({ kind: 'restart', message: 'helper unavailable' });
    expect(state.busy.value).toBe(false);
    await state.retry(); expect(state.status.value).toBe('restarting');
    expect(restart).toHaveBeenCalledTimes(2); expect(backend.PrepareUpdate).toHaveBeenCalledTimes(1);
    expect(runtime.WindowClose).not.toHaveBeenCalled();
  });
  it('invalidated payload requires a fresh preparation and never quits', async () => {
    const { state, backend, runtime } = fixture({ RestartToUpdate: vi.fn().mockResolvedValue({ ...available, ready: false, error: 'staged payload changed' }) });
    await state.check(); await state.apply(); await state.restart();
    expect(state.status.value).toBe('error'); expect(state.info.value.ready).toBe(false);
    await state.retry(); expect(backend.CheckForUpdate).toHaveBeenCalledTimes(2);
    expect(state.status.value).toBe('available'); await state.apply();
    expect(backend.PrepareUpdate).toHaveBeenCalledTimes(2); expect(state.status.value).toBe('ready');
    expect(runtime.WindowClose).not.toHaveBeenCalled();
  });
  it('accepts startup ready but ignores late offers and progress that would lose the prepared package', () => {
    const { state } = fixture();
    state.handleEvent({ ...ready, action: 'ready', source: 'startup' });
    expect(state.status.value).toBe('ready');
    state.handleEvent({ ...available, action: 'ask', latest: '9.0.0' });
    state.handleEvent({ ...available, action: 'progress', source: 'startup', phase: 'ready' });
    expect(state.status.value).toBe('ready'); expect(state.info.value.latest).toBe(ready.latest);
  });
  it('rollback errors on reopening remain explicit and retry a check instead of reinstalling', async () => {
    const { state, backend } = fixture();
    state.handleEvent({ current: available.current, latest: available.current, hasUpdate: false, action: 'applied-error', ready: false, error: 'new process could not start; old restored' });
    expect(state.status.value).toBe('error'); expect(state.info.value.ready).toBe(false);
    await state.retry(); expect(backend.CheckForUpdate).toHaveBeenCalledTimes(1);
    expect(backend.RestartToUpdate).not.toHaveBeenCalled(); expect(backend.PrepareUpdate).not.toHaveBeenCalled();
  });
  it('keeps non-Windows and legacy backends on their existing installation contract', async () => {
    const { state, backend } = fixture({ CheckForUpdate: vi.fn().mockResolvedValue({ ...available, platform: 'linux/amd64' }), PrepareUpdate: vi.fn().mockResolvedValue({ ...available, platform: 'linux/amd64', applied: true }) });
    await state.check(); expect(state.downloadOnly.value).toBe(false); await state.apply();
    expect(state.status.value).toBe('applied'); expect(backend.RestartToUpdate).not.toHaveBeenCalled();
  });
});

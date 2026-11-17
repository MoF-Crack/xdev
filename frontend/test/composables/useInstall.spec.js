import { effectScope } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanupWailsMocks } from '../mocks/wails.js';
import { useInstall } from '../../src/composables/useInstall.js';
import { installSessionHarness } from './installSessionHarness.js';
afterEach(cleanupWailsMocks);

describe('useInstall session contract', () => {
  it('subscribes once and disposes only its own listener idempotently', async () => {
    const h = installSessionHarness();
    const scope = effectScope();
    const first = scope.run(() => useInstall());
    const second = useInstall();
    await first.install('node', '22'); await second.install('go', '1.24');
    scope.stop(); first.dispose();
    expect(h.runtime.EventsOn.mock.calls.map(([n]) => n)).toEqual(['installSession', 'installSession']);
    expect(h.listeners.get('installSession').size).toBe(1);
    expect(h.runtime.EventsOff).not.toHaveBeenCalled();
    h.emit(second.taskId.value, { percent: 55 });
    expect(first.progress.value.percent).toBe(0);
    expect(second.progress.value.percent).toBe(55);
    await expect(first.waitForCompletion()).resolves.toBe('disposed');
    second.dispose();
  });
  it('acceptance keeps the waiter pending and rejects a second active install', async () => {
    installSessionHarness(); const s = useInstall();
    await expect(s.waitForCompletion()).resolves.toBe('idle');
    expect(await s.install('node', '22')).toBe(true);
    const done = vi.fn(); s.waitForCompletion().then(done);
    expect(await s.install('go', '1.24')).toBe(false);
    await Promise.resolve();
    expect(done).not.toHaveBeenCalled(); expect(s.active.value).toBe(true);
    s.dispose();
  });
  it.each(['completed', 'error', 'cancelled', 'needs-repair'])('terminal %s releases exactly once', async status => {
    const h = installSessionHarness(); const notify = vi.fn();
    const s = useInstall({ onComplete: notify, onError: notify, onCancel: notify, onNeedsRepair: notify });
    await s.install('node', '22'); const id = s.taskId.value;
    h.emit(id, { status, error: status === 'error' || status === 'needs-repair' ? 'broken' : '' });
    h.emit(id, { status: 'completed' });
    await expect(s.waitForCompletion()).resolves.toBe(status);
    expect(s.active.value).toBe(false); expect(s.canCancel.value).toBe(false);
    expect(notify).toHaveBeenCalledTimes(1);
    expect(notify).toHaveBeenCalledWith(expect.objectContaining({ tool: 'node', version: '22', sessionId: id, status }));
    s.dispose();
  });
  it('does not reuse retry identity or accept stale/missing identity or reordered snapshots', async () => {
    const h = installSessionHarness(); const s = useInstall();
    await s.install('node', '22'); const old = s.taskId.value;
    h.emit(old, { status: 'error' }); await s.install('node', '22'); const id = s.taskId.value;
    expect(id).not.toBe(old);
    h.emit(old, { status: 'completed', percent: 100 });
    h.emit('', { tool: 'node', version: '22', percent: 99 });
    expect(s.progress.value.percent).toBe(0); expect(s.active.value).toBe(true);
    h.emit(id, { revision: 10, percent: 40 }); h.emit(id, { revision: 9, percent: 90 });
    expect(s.progress.value.percent).toBe(40); s.dispose();
  });
  it('forwards form, confirmation and repair intent to StartInstall', async () => {
    const h = installSessionHarness(); const s = useInstall();
    await s.install('opencode', '1', 'gui');
    expect(h.app.StartInstall).toHaveBeenLastCalledWith(expect.objectContaining({ tool: 'opencode', version: '1', form: 'gui', confirmed: false }));
    h.emit(s.taskId.value, { status: 'completed' });
    await s.installConfirmed('opencode', '2', 'cli');
    expect(h.app.StartInstall).toHaveBeenLastCalledWith(expect.objectContaining({ form: 'cli', confirmed: true }));
    h.emit(s.taskId.value, { status: 'completed' }); await s.repair('opencode', '2', 'gui');
    expect(h.app.StartInstall).toHaveBeenLastCalledWith(expect.objectContaining({ form: 'gui', force: true, confirmed: true })); s.dispose();
  });
  it('uses backend capabilities and waits for cleanup after cancellation ACK', async () => {
    const h = installSessionHarness(); const onError = vi.fn(); const s = useInstall({ onError });
    await s.install('node', '22'); const id = s.taskId.value;
    expect(await s.pause()).toBe(false);
    h.emit(id, { status: 'downloading', canPause: true, canCancel: true });
    expect(await s.pause()).toBe(true); expect(await s.resume()).toBe(true);
    expect(h.app.PauseDownload).toHaveBeenCalledWith(id); expect(h.app.ResumeDownload).toHaveBeenCalledWith(id);
    const pending = s.cancel(); const done = vi.fn(); pending.then(done);
    await Promise.resolve(); await Promise.resolve();
    expect(done).not.toHaveBeenCalled(); expect(s.active.value).toBe(true);
    h.emit(id, { status: 'cancelled' });
    expect(await pending).toBe(true); expect(h.app.CancelInstall).toHaveBeenCalledWith(id);
    expect(onError).not.toHaveBeenCalled(); s.dispose();
  });
  it('rejects controls after commit and recovers from cancellation losing the commit race', async () => {
    const h = installSessionHarness(); const s = useInstall(); await s.install('node', '22'); const id = s.taskId.value;
    h.app.CancelInstall.mockImplementation(async () => { h.emit(id, { status: 'committing', canCancel: false, canPause: false }); throw Error('already committing'); });
    expect(await s.cancel()).toBe(false); expect(s.active.value).toBe(true);
    expect(await s.cancel()).toBe(false); expect(await s.pause()).toBe(false);
    expect(h.app.CancelInstall).toHaveBeenCalledTimes(1);
    h.emit(id, { status: 'completed' }); await expect(s.waitForCompletion()).resolves.toBe('completed'); s.dispose();
  });
  it('recovers a lost acceptance reply from the authoritative snapshot', async () => {
    const h = installSessionHarness();
    h.app.StartInstall.mockImplementation(async spec => { h.emit(spec.sessionId, { status: 'completed' }); throw Error('reply lost'); });
    const s = useInstall(); expect(await s.install('node', '22')).toBe(true);
    await expect(s.waitForCompletion()).resolves.toBe('completed'); s.dispose();
  });
  it('reports rejected starts and allows a retry', async () => {
    const h = installSessionHarness(); h.app.StartInstall.mockRejectedValueOnce(Error('offline'));
    const s = useInstall(); expect(await s.install('node', '22')).toBe(false);
    await expect(s.waitForCompletion()).resolves.toBe('error'); expect(s.error.value.message).toContain('offline');
    expect(await s.install('node', '22')).toBe(true); s.dispose();
  });
  it('keeps progress on negative sentinel and resets it on fallback', async () => {
    const h = installSessionHarness(); const s = useInstall(); await s.install('node', '22'); const id = s.taskId.value;
    h.emit(id, { percent: 97, speed: '2 MB/s', eta: '5s' });
    h.emit(id, { percent: -1, phase: 'Attempt 2/3' });
    expect(s.progress.value).toMatchObject({ percent: 97, speed: '2 MB/s', eta: '5s' });
    h.emit(id, { percent: 0, phase: 'Trying fallback' }); expect(s.progress.value.percent).toBe(0);
    expect(s.phase.value).toBe('Trying fallback'); s.dispose();
  });
});

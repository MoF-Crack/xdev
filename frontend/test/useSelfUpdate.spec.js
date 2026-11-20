import { afterEach, describe, expect, it, vi } from 'vitest';
import { useSelfUpdate } from '../src/composables/useSelfUpdate.js';

const t = (key, params) => params ? `${key}:${params.latest}` : key;
const assetName = 'xdev-windows-amd64-v1.0.0-beta.2.zip';
const assetInfo = { platform: 'windows/amd64', assetName, downloadUrls: { [assetName]: 'https://example.test/beta.2.zip' } };
const available = { current: '1.0.0-alpha.3', latest: '1.0.0-beta.2', hasUpdate: true, applied: false, ...assetInfo };
const progress = { action: 'progress', current: available.current, latest: available.latest, phase: 'downloading', downloadedBytes: 320, totalBytes: 1000, percent: 32 };
function deferred() { let resolve; let reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
function fixture(overrides = {}) {
  const app = { CheckForUpdate: vi.fn().mockResolvedValue({ ...available }), ApplyUpdate: vi.fn().mockResolvedValue({ ...available, applied: true }), IgnoreSelfUpdate: vi.fn().mockResolvedValue(undefined), ...overrides };
  const message = { success: vi.fn(), error: vi.fn(), info: vi.fn() };
  const runtime = { WindowClose: vi.fn() };
  const state = useSelfUpdate({ t, message, app: () => app, runtime: () => runtime });
  return { state, app, message, runtime };
}
afterEach(() => { document.body.innerHTML = ''; });

describe('useSelfUpdate', () => {
  it('manual check opens confirmation without applying and retains the original trigger', async () => {
    const trigger = document.createElement('button'); document.body.append(trigger); trigger.focus();
    const { state, app, message } = fixture();
    expect(await state.check()).toBe(true);
    expect(state.show.value).toBe(true);
    expect(state.status.value).toBe('available');
    expect(state.returnFocusTo.value).toBe(trigger);
    expect(state.dialogInfo.value).toMatchObject(assetInfo);
    expect(app.ApplyUpdate).not.toHaveBeenCalled();
    expect(message.info).not.toHaveBeenCalled();
  });

  it('uses a fresh archive snapshot for checks and startup offers, including older payloads', async () => {
    const legacy = { current: available.current, latest: available.latest, hasUpdate: true };
    const check = vi.fn().mockResolvedValueOnce(available).mockResolvedValueOnce(legacy);
    const { state, app } = fixture({ CheckForUpdate: check });
    await state.check();
    expect(state.dialogInfo.value).toMatchObject(assetInfo);
    await state.check();
    expect(state.dialogInfo.value.assetName).toBeFalsy();
    expect(state.dialogInfo.value.downloadUrls).toBeUndefined();
    expect(app.ApplyUpdate).not.toHaveBeenCalled();
    state.handleEvent({ action: 'ask', ...available });
    expect(state.dialogInfo.value).toMatchObject(assetInfo);
    state.handleEvent({ action: 'notify', ...legacy, latest: '1.0.0-beta.3' });
    expect(state.dialogInfo.value.assetName).toBeFalsy();
    expect(state.dialogInfo.value.downloadUrls).toBeUndefined();
    expect(await state.apply()).toBe(true);
  });

  it('preserves the same candidate on sparse progress and a thrown apply, but never inherits a different candidate', async () => {
    const response = deferred();
    const { state } = fixture({ ApplyUpdate: vi.fn().mockReturnValue(response.promise) });
    await state.check();
    const applying = state.apply();
    state.handleEvent(progress);
    expect(state.dialogInfo.value).toMatchObject(assetInfo);
    response.reject(new Error('network offline'));
    await applying;
    expect(state.dialogInfo.value).toMatchObject(assetInfo);
    state.handleEvent({ ...progress, source: 'startup', latest: '1.0.0-beta.3' });
    expect(state.dialogInfo.value.assetName).toBeFalsy();
    expect(state.dialogInfo.value.downloadUrls).toBeUndefined();
  });

  it('clears the manual resource on a fresh same-release apply failure that reports no selected asset', async () => {
    const { state, app } = fixture({ ApplyUpdate: vi.fn().mockResolvedValue({
      current: available.current, latest: available.latest, platform: available.platform, hasUpdate: true, applied: false,
      error: 'No matching archive for windows/amd64',
    }) });
    await state.check();
    expect(await state.apply()).toBe(false);
    expect(state.status.value).toBe('error');
    expect(state.dialogInfo.value.platform).toBe(available.platform);
    expect(state.dialogInfo.value.assetName).toBeFalsy();
    expect(state.dialogInfo.value.downloadUrls).toBeUndefined();
    await state.retry();
    expect(app.ApplyUpdate).toHaveBeenCalledTimes(2);
  });

  it('applies through the existing API, consumes real progress, waits for applied and leaves a restart result open', async () => {
    const response = deferred();
    const { state, app, message, runtime } = fixture({ ApplyUpdate: vi.fn().mockReturnValue(response.promise) });
    await state.check();
    const applying = state.apply();
    expect(state.busy.value).toBe(true);
    expect(state.progress.value).toBeNull();
    state.handleEvent(progress);
    expect(state.progress.value).toEqual({ phase: 'downloading', downloadedBytes: 320, totalBytes: 1000, percent: 32 });
    state.handleEvent({ ...progress, phase: 'completed', downloadedBytes: 1000, percent: 100 });
    state.handleEvent({ action: 'applied', latest: available.latest, applied: true });
    expect(state.status.value).toBe('updating');
    expect(message.success).not.toHaveBeenCalled();
    response.resolve({ ...available, applied: true });
    expect(await applying).toBe(true);
    expect(app.ApplyUpdate).toHaveBeenCalledTimes(1);
    expect(state.status.value).toBe('applied');
    expect(state.show.value).toBe(true);
    expect(state.busy.value).toBe(false);
    expect(message.success).toHaveBeenCalledExactlyOnceWith(`self_update.applied:${available.latest}`);
    expect(runtime.WindowClose).not.toHaveBeenCalled();
    // Until a restart, checks/reopen must not offer a duplicate installation.
    await state.check(); await state.apply();
    expect(app.CheckForUpdate).toHaveBeenCalledTimes(1);
    expect(app.ApplyUpdate).toHaveBeenCalledTimes(1);
  });

  it('refetched already-up-to-date is not an installation success', async () => {
    const { state, message } = fixture({ ApplyUpdate: vi.fn().mockResolvedValue({ current: available.latest, latest: available.latest, hasUpdate: false, applied: false }) });
    await state.check();
    expect(await state.apply()).toBe(false);
    expect(state.status.value).toBe('up-to-date');
    expect(state.show.value).toBe(true);
    expect(message.success).not.toHaveBeenCalled();
    expect(message.info).toHaveBeenCalledWith('self_update.no_update_applied');
  });

  it.each(['throw', 'result'])('check %s failure is inline, never up-to-date, and retries the check', async mode => {
    const check = vi.fn();
    if (mode === 'throw') check.mockRejectedValueOnce(new Error('network offline'));
    else check.mockResolvedValueOnce({ current: available.current, latest: '', hasUpdate: false, applied: false, error: 'network offline' });
    check.mockResolvedValueOnce(available);
    const { state, message } = fixture({ CheckForUpdate: check });
    expect(await state.check()).toBe(false);
    expect(state.status.value).toBe('error');
    expect(state.error.value).toEqual({ kind: 'check', message: 'network offline' });
    expect(state.show.value).toBe(true);
    expect(message.success).not.toHaveBeenCalled();
    expect(await state.retry()).toBe(true);
    expect(check).toHaveBeenCalledTimes(2);
    expect(state.error.value).toBeNull();
    expect(state.info.value.error).toBe('');
    expect(state.status.value).toBe('available');
  });

  it('installation error keeps details and retries ApplyUpdate instead of reporting success', async () => {
    const apply = vi.fn().mockResolvedValueOnce({ ...available, error: 'checksum mismatch' }).mockResolvedValueOnce({ ...available, applied: true });
    const { state, app, message } = fixture({ ApplyUpdate: apply });
    await state.check();
    await state.apply();
    expect(state.error.value).toEqual({ kind: 'apply', message: 'checksum mismatch' });
    expect(state.status.value).toBe('error');
    expect(state.show.value).toBe(true);
    expect(message.success).not.toHaveBeenCalled();
    expect(await state.retry()).toBe(true);
    expect(app.CheckForUpdate).toHaveBeenCalledTimes(1);
    expect(app.ApplyUpdate).toHaveBeenCalledTimes(2);
    expect(state.error.value).toBeNull();
    expect(state.info.value.error).toBe('');
  });

  it.each([undefined, null, {}, { ...available, applied: false }, { hasUpdate: false, applied: true, latest: available.latest }])('does not accept an unconfirmed ApplyUpdate response: %j', async result => {
    const { state, message } = fixture({ ApplyUpdate: vi.fn().mockResolvedValue(result) });
    await state.check(); await state.apply();
    expect(state.status.value).toBe('error');
    expect(state.info.value.applied).toBe(false);
    expect(message.success).not.toHaveBeenCalled();
    expect(state.show.value).toBe(true);
  });

  it.each(['failure', 'no-update'])('late manual progress cannot revoke an authoritative %s reply', async outcome => {
    const result = outcome === 'failure' ? { ...available, error: 'checksum mismatch' }
      : { current: available.latest, latest: available.latest, hasUpdate: false, applied: false };
    const { state } = fixture({ ApplyUpdate: vi.fn().mockResolvedValue(result) });
    await state.check(); await state.apply();
    const status = state.status.value;
    const error = state.error.value;
    state.handleEvent({ ...progress, source: 'manual' });
    state.handleEvent(progress); // Also tolerate legacy/untagged delayed events.
    expect(state.status.value).toBe(status);
    expect(state.error.value).toEqual(error);
    expect(state.busy.value).toBe(false);
    state.handleEvent({ ...progress, source: 'startup' });
    expect(state.status.value).toBe('updating');
    expect(state.busy.value).toBe(true);
  });

  it('blocks concurrent checks, apply, ignore, close and quit while checking', async () => {
    const response = deferred();
    const { state, app, runtime } = fixture({ CheckForUpdate: vi.fn().mockReturnValue(response.promise) });
    const checking = state.check();
    state.handleEvent({ action: 'notify', latest: 'stale-release' });
    await state.check(); await state.apply(); await state.ignore();
    expect(state.close()).toBe(false); expect(state.quit()).toBe(false);
    expect(app.CheckForUpdate).toHaveBeenCalledTimes(1);
    expect(app.ApplyUpdate).not.toHaveBeenCalled();
    expect(app.IgnoreSelfUpdate).not.toHaveBeenCalled();
    expect(runtime.WindowClose).not.toHaveBeenCalled();
    response.resolve(available); await checking;
    expect(state.status.value).toBe('available');
  });

  it('blocks concurrent apply and dismissal throughout installation, including completed progress', async () => {
    const response = deferred();
    const { state, app, runtime } = fixture({ ApplyUpdate: vi.fn().mockReturnValue(response.promise) });
    await state.check(); const applying = state.apply();
    state.handleEvent({ ...progress, phase: 'completed', percent: 100 });
    await state.apply(); await state.check(); await state.ignore();
    expect(state.close()).toBe(false); expect(state.quit()).toBe(false);
    expect(app.ApplyUpdate).toHaveBeenCalledTimes(1);
    expect(app.CheckForUpdate).toHaveBeenCalledTimes(1);
    expect(app.IgnoreSelfUpdate).not.toHaveBeenCalled();
    expect(runtime.WindowClose).not.toHaveBeenCalled();
    response.resolve({ ...available, applied: true }); await applying;
    expect(state.close()).toBe(true);
  });

  it.each(['ask', 'notify'])('startup %s opens the same actionable dialog, not just a toast', async action => {
    const { state, app, message } = fixture();
    state.handleEvent({ action, current: available.current, latest: available.latest, downloadUrls: available.downloadUrls });
    expect(state.show.value).toBe(true); expect(state.source.value).toBe('startup');
    expect(state.status.value).toBe('available');
    expect(message.info).not.toHaveBeenCalled();
    await state.apply(); expect(app.ApplyUpdate).toHaveBeenCalledTimes(1);
  });

  it('auto-update progress wins over a late manual busy response and errors remain retryable', async () => {
    const response = deferred();
    const { state, message } = fixture({ CheckForUpdate: vi.fn().mockReturnValue(response.promise) });
    const checking = state.check();
    state.handleEvent(progress);
    response.resolve({ ...available, error: 'update busy' }); await checking;
    expect(state.status.value).toBe('updating');
    expect(state.error.value).toBeNull();
    expect(state.progress.value.percent).toBe(32);
    state.handleEvent({ action: 'applied-error', current: available.current, latest: available.latest, error: 'access denied' });
    expect(state.status.value).toBe('error');
    expect(state.error.value.message).toBe('access denied');
    expect(state.show.value).toBe(true);
    expect(message.success).not.toHaveBeenCalled();
    await state.retry(); expect(state.status.value).toBe('applied');
  });

  it('a late startup failure cannot revoke a confirmed manual installation or trigger a duplicate retry', async () => {
    const { state, app, message } = fixture();
    await state.check();
    expect(await state.apply()).toBe(true);
    state.handleEvent({ action: 'applied-error', source: 'startup', current: available.current, latest: available.latest, error: 'old startup checksum failure' });
    expect(state.status.value).toBe('applied');
    expect(state.info.value.applied).toBe(true);
    expect(state.error.value).toBeNull();
    await state.retry();
    expect(app.ApplyUpdate).toHaveBeenCalledTimes(1);
    expect(message.success).toHaveBeenCalledTimes(1);
  });

  it('startup applied event requires explicit success and ignores duplicate terminal events', () => {
    const { state, message, runtime } = fixture();
    state.handleEvent({ action: 'applied', latest: available.latest });
    expect(state.status.value).toBe('error'); expect(message.success).not.toHaveBeenCalled();
    state.handleEvent({ action: 'applied', latest: available.latest, applied: true });
    state.handleEvent({ action: 'applied', latest: available.latest, applied: true });
    expect(state.status.value).toBe('applied'); expect(state.show.value).toBe(true);
    expect(message.success).toHaveBeenCalledTimes(1);
    expect(runtime.WindowClose).not.toHaveBeenCalled();
  });

  it('ignore is single-flight, keeps the dialog busy, and ignores duplicate startup notifications', async () => {
    const response = deferred();
    const { state, app } = fixture({ IgnoreSelfUpdate: vi.fn().mockReturnValue(response.promise) });
    state.handleEvent({ action: 'ask', ...available });
    const ignoring = state.ignore();
    expect(state.dismissing.value).toBe(true); expect(state.close()).toBe(false);
    await state.apply(); await state.check(); await state.ignore();
    expect(app.IgnoreSelfUpdate).toHaveBeenCalledTimes(1);
    response.resolve(); await ignoring;
    state.handleEvent({ action: 'ask', ...available });
    expect(state.show.value).toBe(false);
    expect(state.open()).toBe(true); // Ignored startup versions remain manually actionable.
  });

  it('does not mutate a disposed view or toast after a late check', async () => {
    const response = deferred();
    const { state, message } = fixture({ CheckForUpdate: vi.fn().mockReturnValue(response.promise) });
    const checking = state.check(); state.dispose(); response.resolve(available); await checking;
    state.handleEvent(progress);
    expect(state.info.value).toBeNull(); expect(state.show.value).toBe(false);
    expect(message.success).not.toHaveBeenCalled(); expect(message.info).not.toHaveBeenCalled();
  });
});

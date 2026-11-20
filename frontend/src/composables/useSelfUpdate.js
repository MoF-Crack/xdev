import { computed, getCurrentScope, onScopeDispose, ref } from 'vue';

const progressPhases = new Set(['downloading', 'verifying', 'extracting', 'installing', 'completed', 'ready']);
const emptyInfo = () => ({ current: '', latest: '', hasUpdate: false, applied: false, ready: false, restarting: false });

export function useSelfUpdate({ t, message, app = () => window.go?.gui?.App, runtime = () => window.runtime } = {}) {
  const info = ref(null);
  const show = ref(false);
  const checking = ref(false);
  const updating = ref(false);
  const restarting = ref(false);
  const dismissing = ref(false);
  const error = ref(null);
  const progress = ref(null);
  const source = ref('manual');
  const returnFocusTo = ref(null);
  const busy = computed(() => checking.value || updating.value || restarting.value || dismissing.value);
  const dialogInfo = computed(() => info.value || emptyInfo());
  // Older bindings and non-Windows builds still install in the download call.
  const downloadOnly = computed(() => typeof app()?.PrepareUpdate === 'function'
    && (!info.value?.platform || String(info.value.platform).startsWith('windows/')));
  const status = computed(() => {
    if (restarting.value) return 'restarting';
    if (updating.value) return 'updating';
    if (checking.value) return 'checking';
    if (dismissing.value) return 'dismissing';
    if (info.value?.ready === true) return 'ready';
    if (error.value) return 'error';
    if (info.value?.applied === true) return 'applied';
    if (info.value?.hasUpdate === true) return 'available';
    return info.value?.hasUpdate === false ? 'up-to-date' : 'idle';
  });

  let disposed = false;
  let revision = 0;
  let operation = '';
  let manualApplyFinished = false;
  let ignoredLatest = '';

  function notify(kind, text) {
    if (!disposed) message?.[kind]?.(text);
  }
  function rememberFocus() {
    if (!show.value) returnFocusTo.value = document.activeElement;
  }
  function resultInfo(result, previous = null) {
    const snapshot = { ...emptyInfo(), ...previous, ...result };
    return {
      ...snapshot,
      error: typeof result?.error === 'string' ? result.error : '',
      applied: snapshot.applied === true,
      ready: snapshot.ready === true && snapshot.applied !== true,
      restarting: snapshot.restarting === true,
    };
  }
  function validateObject(result) {
    if (!result || typeof result !== 'object' || Array.isArray(result)) throw new Error(t('self_update.invalid_response'));
    return result;
  }
  function validateResult(result) {
    validateObject(result);
    if (typeof result.error === 'string' && result.error) return result;
    if (typeof result.hasUpdate !== 'boolean' || (result.hasUpdate && (typeof result.latest !== 'string' || !result.latest))
      || (!result.hasUpdate && (result.applied === true || result.ready === true))) throw new Error(t('self_update.invalid_response'));
    return result;
  }
  function fail(kind, reason, result) {
    const detail = reason?.message || String(reason || t('self_update.invalid_response'));
    // Only restart failures describe the already prepared candidate. Other
    // actual replies supersede the prior selection (including omitted assets).
    const previous = kind === 'restart' || (!result && kind !== 'check') ? info.value : null;
    info.value = {
      ...resultInfo(result, previous), applied: false, restarting: false,
      ready: kind === 'restart' && (result?.ready === true || (result?.ready !== false && previous?.ready === true)),
      ...(kind === 'check' ? { hasUpdate: false } : {}),
    };
    error.value = { kind, message: detail };
    show.value = true;
    notify('error', `${t(kind === 'check' ? 'update_check_failed' : kind === 'restart' ? 'self_update.restart_failed' : 'self_update.error_title')}: ${detail}`);
  }

  async function check() {
    if (disposed || busy.value) return false;
    // A staged or applied update remains actionable without rechecking.
    if (info.value?.ready === true || info.value?.applied === true) return open();
    rememberFocus();
    if (!show.value) source.value = 'manual';
    const request = ++revision;
    operation = 'check';
    checking.value = true;
    error.value = null;
    progress.value = null;
    try {
      const result = validateResult(await app().CheckForUpdate());
      if (disposed || request !== revision) return false;
      if (result.error) {
        fail('check', result.error, result);
        return false;
      }
      info.value = { ...resultInfo(result), applied: false, restarting: false };
      if (result.hasUpdate) show.value = true;
      else notify('success', t('up_to_date'));
      return true;
    } catch (reason) {
      if (!disposed && request === revision) fail('check', reason);
      return false;
    } finally {
      if (operation === 'check') operation = '';
      checking.value = false;
    }
  }

  function open() {
    if (disposed || busy.value || !info.value) return false;
    rememberFocus();
    source.value = 'manual';
    show.value = true;
    return true;
  }

  async function apply() {
    if (disposed || busy.value || info.value?.hasUpdate !== true || info.value.ready || info.value.applied || error.value?.kind === 'check') return false;
    rememberFocus();
    show.value = true;
    const request = ++revision;
    operation = 'apply';
    manualApplyFinished = false;
    updating.value = true;
    error.value = null;
    progress.value = null;
    try {
      const backend = app();
      const prepare = typeof backend.PrepareUpdate === 'function';
      const result = validateResult(await (prepare ? backend.PrepareUpdate() : backend.ApplyUpdate()));
      if (disposed || request !== revision) return false;
      if (result.error) {
        fail('apply', result.error, result);
        return false;
      }
      info.value = { ...resultInfo(result), restarting: false };
      if (info.value.applied) {
        notify('success', t('self_update.applied', { latest: result.latest }));
        return true;
      }
      if (info.value.ready) return true;
      if (!result.hasUpdate) {
        notify('info', t('self_update.no_update_applied'));
        return false;
      }
      fail('apply', t(prepare && downloadOnly.value ? 'self_update.not_ready' : 'self_update.not_applied'), result);
      return false;
    } catch (reason) {
      if (!disposed && request === revision) fail('apply', reason);
      return false;
    } finally {
      manualApplyFinished = true;
      if (operation === 'apply') operation = '';
      updating.value = false;
    }
  }

  async function restart() {
    if (disposed || busy.value || info.value?.ready !== true) return false;
    rememberFocus();
    show.value = true;
    operation = 'restart';
    const request = ++revision;
    restarting.value = true;
    error.value = null;
    progress.value = null;
    let accepted = false;
    try {
      const backend = app();
      if (typeof backend?.RestartToUpdate !== 'function') throw new Error(t('self_update.restart_unavailable'));
      const result = validateObject(await backend.RestartToUpdate());
      if (disposed || request !== revision) return false;
      if (result.error || result.restarting !== true) {
        fail('restart', result.error || t('self_update.restart_unconfirmed'), result);
        return false;
      }
      info.value = resultInfo(result, info.value);
      // The backend owns graceful exit. Stay locked after its acknowledgement.
      accepted = true;
      return true;
    } catch (reason) {
      if (!disposed && request === revision) fail('restart', reason);
      return false;
    } finally {
      if (operation === 'restart') operation = '';
      if (!accepted) restarting.value = false;
    }
  }

  function close() {
    if (disposed || busy.value) return false;
    show.value = false;
    return true;
  }

  async function ignore() {
    if (disposed || busy.value || info.value?.hasUpdate !== true || info.value.ready || info.value.applied) return false;
    const latest = info.value.latest;
    const request = ++revision;
    operation = 'ignore';
    dismissing.value = true;
    error.value = null;
    try {
      await app().IgnoreSelfUpdate(latest);
      if (disposed || request !== revision) return false;
      ignoredLatest = latest;
      show.value = false;
      notify('info', t('self_update.ignored', { latest }));
      return true;
    } catch (reason) {
      if (!disposed && request === revision) fail('ignore', reason);
      return false;
    } finally {
      if (operation === 'ignore') operation = '';
      dismissing.value = false;
    }
  }

  function quit() {
    if (info.value?.ready === true || !close()) return false;
    runtime()?.WindowClose?.();
    return true;
  }

  function retry() {
    if (info.value?.ready === true) return restart();
    if (error.value?.kind === 'apply' && info.value?.hasUpdate === true) return apply();
    if (error.value?.kind === 'ignore') return ignore();
    return check();
  }

  function handleEvent(data) {
    if (disposed || !data || typeof data !== 'object' || restarting.value || info.value?.ready || info.value?.applied) return;
    if (data.action === 'ask' || data.action === 'notify') {
      if (busy.value || !data.latest || data.latest === ignoredLatest) return;
      rememberFocus();
      info.value = resultInfo({ ...data, hasUpdate: true, applied: false, ready: false, restarting: false });
      source.value = 'startup';
      error.value = null;
      progress.value = null;
      show.value = true;
      return;
    }
    if (data.action === 'progress') {
      if (!progressPhases.has(data.phase) || operation === 'ignore') return;
      // A delayed manual progress event cannot override its API reply.
      if (manualApplyFinished && operation !== 'apply' && data.source !== 'startup') return;
      if (operation !== 'apply' && !updating.value) {
        ++revision;
        rememberFocus();
        source.value = data.source === 'manual' ? 'manual' : 'startup';
      }
      const previous = info.value;
      const sameCandidate = data.latest && data.latest === previous?.latest
        && (!data.current || data.current === previous.current)
        && (!Object.hasOwn(data, 'platform') || data.platform === previous.platform)
        && (!Object.hasOwn(data, 'assetName') || data.assetName === previous.assetName);
      // Progress has no authority to declare installation or a staged update.
      info.value = resultInfo({ ...data, hasUpdate: true, applied: false, ready: false, restarting: false }, sameCandidate ? previous : null);
      progress.value = {
        phase: data.phase,
        downloadedBytes: Math.max(0, Number(data.downloadedBytes) || 0),
        totalBytes: Math.max(0, Number(data.totalBytes) || 0),
        percent: typeof data.percent === 'number' && Number.isFinite(data.percent) ? Math.max(0, Math.min(100, data.percent)) : null,
      };
      error.value = null;
      updating.value = true;
      show.value = true;
      return;
    }
    if (!['ready', 'applied', 'applied-error'].includes(data.action) || operation === 'apply' || operation === 'ignore') return;
    if (data.action === 'ready' && (data.ready !== true || data.applied === true || data.hasUpdate === false || data.error || !data.latest)) return;
    ++revision;
    rememberFocus();
    source.value = 'startup';
    updating.value = false;
    progress.value = null;
    show.value = true;
    if (data.action === 'ready') {
      info.value = resultInfo({ ...data, hasUpdate: true, applied: false, restarting: false });
      error.value = null;
    } else if (data.action === 'applied' && data.applied === true) {
      info.value = resultInfo({ ...data, hasUpdate: true, applied: true, restarting: false });
      error.value = null;
      notify('success', t('self_update.applied', { latest: data.latest }));
    } else {
      // Legacy errors often omit hasUpdate; a rollback explicitly sets false.
      fail('apply', data.error || t(data.action === 'applied-error' ? 'self_update.apply_failed' : 'self_update.not_applied'), { ...data, hasUpdate: data.hasUpdate !== false, applied: false });
    }
  }

  function dispose() { disposed = true; ++revision; }
  if (getCurrentScope()) onScopeDispose(dispose);
  return { info, dialogInfo, show, checking, updating, restarting, dismissing, busy, downloadOnly, error, progress, source, status, returnFocusTo, check, open, apply, restart, close, ignore, quit, retry, handleEvent, dispose };
}

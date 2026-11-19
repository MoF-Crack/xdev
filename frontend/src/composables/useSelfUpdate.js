import { computed, getCurrentScope, onScopeDispose, ref } from 'vue';

const progressPhases = new Set(['downloading', 'verifying', 'extracting', 'installing', 'completed']);
const emptyInfo = () => ({ current: '', latest: '', hasUpdate: false, applied: false });

// One state machine owns manual checks, startup notifications and installation.
// The existing ApplyUpdate binding rechecks the release before installing it;
// only its explicit applied=true (or the same authoritative startup event) is
// installation success. Download completion by itself is not success.
export function useSelfUpdate({ t, message, app = () => window.go.gui.App, runtime = () => window.runtime } = {}) {
  const info = ref(null);
  const show = ref(false);
  const checking = ref(false);
  const updating = ref(false);
  const dismissing = ref(false);
  const error = ref(null);
  const progress = ref(null);
  const source = ref('manual');
  const returnFocusTo = ref(null);
  const busy = computed(() => checking.value || updating.value || dismissing.value);
  const dialogInfo = computed(() => info.value || emptyInfo());
  const status = computed(() => {
    if (updating.value) return 'updating';
    if (checking.value) return 'checking';
    if (dismissing.value) return 'dismissing';
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
  function resultInfo(result, previous = info.value) {
    return {
      ...emptyInfo(),
      ...previous,
      ...result,
      error: typeof result?.error === 'string' ? result.error : '',
      applied: result?.applied === true,
    };
  }
  function validateResult(result) {
    if (!result || typeof result !== 'object' || Array.isArray(result)) throw new Error(t('self_update.invalid_response'));
    if (typeof result.error === 'string' && result.error) return result;
    if (typeof result.hasUpdate !== 'boolean' || (result.hasUpdate && (typeof result.latest !== 'string' || !result.latest)) || (result.applied === true && !result.hasUpdate)) throw new Error(t('self_update.invalid_response'));
    return result;
  }
  function fail(kind, reason, result) {
    const detail = reason?.message || String(reason || t('self_update.invalid_response'));
    info.value = { ...resultInfo(result), applied: false, ...(kind === 'check' ? { hasUpdate: false } : {}) };
    error.value = { kind, message: detail };
    show.value = true;
    notify('error', `${t(kind === 'check' ? 'update_check_failed' : 'self_update.error_title')}: ${detail}`);
  }

  async function check() {
    if (disposed || busy.value) return false;
    // The running process still reports the old version until the user reopens
    // XDev. Do not offer to reinstall the same release after a successful apply.
    if (info.value?.applied === true) return open();
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
      info.value = { ...resultInfo(result), applied: false };
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
    if (disposed || busy.value || info.value?.hasUpdate !== true || info.value?.applied === true || error.value?.kind === 'check') return false;
    rememberFocus();
    show.value = true;
    const request = ++revision;
    operation = 'apply';
    manualApplyFinished = false;
    updating.value = true;
    error.value = null;
    progress.value = null;
    try {
      const result = validateResult(await app().ApplyUpdate());
      if (disposed || request !== revision) return false;
      info.value = resultInfo(result);
      if (result.error) {
        fail('apply', result.error, result);
        return false;
      }
      if (result.applied === true) {
        notify('success', t('self_update.applied', { latest: result.latest }));
        return true;
      }
      if (result.hasUpdate === false) {
        // The second check can truthfully find no update; no binary was applied.
        notify('info', t('self_update.no_update_applied'));
        return false;
      }
      fail('apply', t('self_update.not_applied'), result);
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

  function close() {
    if (disposed || busy.value) return false;
    show.value = false;
    return true;
  }

  async function ignore() {
    if (disposed || busy.value || info.value?.hasUpdate !== true || info.value?.applied === true) return false;
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
    if (!close()) return false;
    runtime()?.WindowClose?.();
    return true;
  }

  function retry() {
    if (error.value?.kind === 'apply' && info.value?.hasUpdate === true) return apply();
    if (error.value?.kind === 'ignore') return ignore();
    return check();
  }

  function handleEvent(data) {
    if (disposed || !data || typeof data !== 'object') return;
    if (data.action === 'ask' || data.action === 'notify') {
      // An older startup check must not replace a manual in-flight operation,
      // an installed result, or a version the user has already ignored.
      if (busy.value || info.value?.applied === true || !data.latest || data.latest === ignoredLatest) return;
      rememberFocus();
      info.value = resultInfo({ ...data, hasUpdate: true, applied: false });
      source.value = 'startup';
      error.value = null;
      progress.value = null;
      show.value = true;
      return;
    }
    if (data.action === 'progress') {
      if (!progressPhases.has(data.phase) || info.value?.applied === true || operation === 'ignore') return;
      // Event delivery and the API reply can arrive on different channels. A
      // delayed manual progress event must not erase its already-returned error
      // or no-update result and leave the dialog busy forever. Explicit startup
      // progress can still start a genuinely new automatic operation.
      if (manualApplyFinished && operation !== 'apply' && data.source !== 'startup') return;
      if (operation !== 'apply' && !updating.value) {
        // A real auto-update can start while a manual check is waiting on its
        // busy response. Its progress is authoritative; drop that late check.
        ++revision;
        rememberFocus();
        source.value = data.source === 'manual' ? 'manual' : 'startup';
      }
      info.value = resultInfo({ ...data, hasUpdate: true, applied: false });
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
    if (data.action !== 'applied' && data.action !== 'applied-error') return;
    // A delayed startup event cannot undo an authoritative installation reply.
    // The running process still reports its old version until reopened, so
    // revoking applied would offer a duplicate download/replacement.
    if (info.value?.applied === true) return;
    // Manual ApplyUpdate's awaited return owns its terminal outcome and toast.
    // Startup events do not unlock that local operation early or double-report.
    if (operation === 'apply' || operation === 'ignore') return;
    if (data.action === 'applied' && info.value?.applied === true && info.value.latest === data.latest) return;
    ++revision;
    rememberFocus();
    source.value = 'startup';
    updating.value = false;
    show.value = true;
    if (data.action === 'applied' && data.applied === true) {
      info.value = resultInfo({ ...data, hasUpdate: true, applied: true });
      error.value = null;
      notify('success', t('self_update.applied', { latest: data.latest }));
    } else {
      fail('apply', data.error || t(data.action === 'applied-error' ? 'self_update.apply_failed' : 'self_update.not_applied'), { ...data, hasUpdate: true, applied: false });
    }
  }

  function dispose() { disposed = true; ++revision; }
  if (getCurrentScope()) onScopeDispose(dispose);
  return { info, dialogInfo, show, checking, updating, dismissing, busy, error, progress, source, status, returnFocusTo, check, open, apply, close, ignore, quit, retry, handleEvent, dispose };
}

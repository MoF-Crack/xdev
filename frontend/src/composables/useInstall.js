import { getCurrentScope, onScopeDispose, ref } from 'vue';

let sequence = 0;
const newSessionId = () => globalThis.crypto?.randomUUID?.()
  || `install-${Date.now()}-${++sequence}-${Math.random().toString(36).slice(2)}`;
const terminalStates = new Set(['completed', 'error', 'cancelled', 'needs-repair']);

// A session subscribes before StartInstall; the caller-generated identity makes
// even synchronous completion safe. Tool/version are display fields, never IDs.
export function useInstall({ onComplete, onError, onCancel, onNeedsRepair } = {}) {
  const phase = ref('idle');
  const status = ref('idle');
  const progress = ref({ percent: 0, bytes: 0, totalBytes: 0, speed: '', eta: '' });
  const error = ref(null);
  const active = ref(false);
  const paused = ref(false);
  const canCancel = ref(false);
  const canPause = ref(false);
  const cancelling = ref(false);
  const taskId = ref('');
  let disposed = false;
  let revision = 0;
  let current = null;
  let completion = Promise.resolve('idle');
  let resolveCompletion = null;

  function callback(fn, data) {
    // A presentation callback cannot prevent waiters from receiving the outcome.
    try { Promise.resolve(fn?.(data)).catch(console.error); } catch (cause) { console.error(cause); }
  }
  function applySnapshot(data = {}) {
    if (disposed || !active.value || data.sessionId !== taskId.value) return;
    if (data.revision != null && data.revision <= revision) return;
    revision = data.revision ?? revision + 1;
    status.value = data.status || status.value;
    phase.value = data.phase || phase.value;
    canCancel.value = data.canCancel === true;
    canPause.value = data.canPause === true;
    paused.value = data.paused === true;
    cancelling.value = data.status === 'cancelling';
    if (data.percent != null && data.percent >= 0) {
      progress.value = {
        percent: Math.round(data.percent), bytes: data.bytes ?? 0,
        totalBytes: data.totalBytes ?? 0, speed: data.speed || '', eta: data.eta || '',
      };
    }
    if (!terminalStates.has(data.status)) return;
    active.value = paused.value = canCancel.value = canPause.value = cancelling.value = false;
    if (data.status === 'error' || data.status === 'needs-repair') {
      error.value = { message: data.error || data.message || data.status, details: data.error || data.message || data.status };
    }
    const resolve = resolveCompletion;
    resolveCompletion = null;
    resolve?.(data.status);
    const event = { ...current, ...data, taskId: taskId.value };
    if (data.status === 'completed') callback(onComplete, event);
    else if (data.status === 'cancelled') callback(onCancel, event);
    else if (data.status === 'needs-repair') callback(onNeedsRepair || onError, event);
    else callback(onError, event);
  }
  const unsubscribe = window.runtime?.EventsOn?.('installSession', applySnapshot);

  async function refresh(id = taskId.value) {
    if (typeof window.go.gui.App.GetInstallSession !== 'function') return false;
    try {
      const snapshot = await window.go.gui.App.GetInstallSession(id);
      applySnapshot(snapshot);
      return snapshot?.sessionId === id;
    } catch { return false; }
  }

  async function start(spec) {
    if (!spec?.tool || active.value || disposed) return false;
    taskId.value = newSessionId();
    const id = taskId.value;
    current = { ...spec, sessionId: id };
    revision = 0;
    completion = new Promise((resolve) => { resolveCompletion = resolve; });
    active.value = true;
    paused.value = canCancel.value = canPause.value = cancelling.value = false;
    status.value = 'accepted'; phase.value = 'Preparing'; error.value = null;
    progress.value = { percent: 0, bytes: 0, totalBytes: 0, speed: '', eta: '' };
    try {
      const acceptedId = await window.go.gui.App.StartInstall(current);
      if (acceptedId !== id) throw new Error('Install session identity mismatch');
      await refresh(id);
      return true;
    } catch (cause) {
      // An IPC response can fail after the backend accepted work. Its snapshot
      // remains the authority, including a terminal event received meanwhile.
      if (await refresh(id)) return true;
      if (taskId.value === id && active.value) {
        applySnapshot({ ...current, status: 'error', phase: 'error', error: String(cause?.message || cause) });
      }
      return false;
    }
  }

  const install = (tool, version, form = '') => start({ tool, version, form, confirmed: false });
  const installConfirmed = (tool, version, form = '') => start({ tool, version, form, confirmed: true });
  const repair = (tool, version, form = '') => start({ tool, version, form, confirmed: true, force: true });
  const waitForCompletion = () => completion;

  async function control(method) {
    if (!active.value || !canPause.value || (method === 'PauseDownload' ? paused.value : !paused.value)) return false;
    const id = taskId.value;
    try { await window.go.gui.App[method](id); await refresh(id); return true; }
    catch (cause) { await refresh(id); error.value = { message: String(cause?.message || cause) }; return false; }
  }
  const pause = () => control('PauseDownload');
  const resume = () => control('ResumeDownload');

  async function cancel() {
    if (!active.value || !canCancel.value || cancelling.value) return false;
    const id = taskId.value;
    const outcome = completion;
    cancelling.value = true;
    canCancel.value = canPause.value = false;
    try {
      await window.go.gui.App.CancelInstall(id);
      await refresh(id);
      // CancelInstall is only acceptance. Keep the UI and resource ownership
      // active until the worker confirms cancellation after cleanup.
      return (await outcome) === 'cancelled';
    } catch (cause) {
      await refresh(id);
      if (taskId.value === id && active.value) {
        cancelling.value = false;
        error.value = { message: String(cause?.message || cause) };
      }
      return false;
    }
  }

  function dispose() {
    if (disposed) return;
    disposed = true;
    resolveCompletion?.('disposed');
    resolveCompletion = null;
    if (typeof unsubscribe === 'function') unsubscribe();
  }
  if (getCurrentScope()) onScopeDispose(dispose);
  return { start, install, installConfirmed, repair, pause, resume, cancel, waitForCompletion,
    phase, status, progress, error, active, paused, canCancel, canPause, cancelling, taskId, dispose };
}

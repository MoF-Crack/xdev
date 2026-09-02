import { getCurrentScope, onScopeDispose, ref } from 'vue';

export function useInstall({ onComplete, onError: reportError, onCancel } = {}) {
  const phase = ref('idle');
  const progress = ref({ percent: 0, bytes: 0, totalBytes: 0, speed: '', eta: '' });
  const error = ref(null);
  const active = ref(false);
  const paused = ref(false);
  const taskId = ref('');
  const currentTool = ref('');
  const currentVersion = ref('');
  let disposed = false;
  // Spec 035 F2a: completion waiter for event-driven batch updates.
  // completionWaiter is a single resolve slot; lastResult covers the
  // race where the event lands before waitForCompletion registers
  // (e.g. the fast path emits Completed synchronously); cancelRequested
  // lets onError swallow the backend's late “下载已取消” toast
  // when the user already pressed Cancel.
  let completionWaiter = null;
  let lastResult = null;
  let cancelRequested = false;

  function notifyCompletion(result) {
    lastResult = result;
    const waiter = completionWaiter;
    completionWaiter = null;
    if (waiter) waiter(result);
  }

  const belongsToCurrentTask = (data) => data?.taskId
    ? data.taskId === taskId.value
    : data?.tool === currentTool.value && data?.version === currentVersion.value;
  const onProgress = (data = {}) => {
    if (!active.value || !belongsToCurrentTask(data)) return;
    phase.value = data.phase || phase.value;
    // Percent < 0 is a status-only update (retry/fallback/proxy messages):
    // keep the entire current progress (bar, bytes, speed, ETA) — only the
    // phase text changes. A real event (percent >= 0) updates the bar,
    // including hard resets to 0 for new attempts/URLs.
    if (data.percent != null && data.percent < 0) return;
    progress.value = {
      percent: Math.round(data.percent || 0), bytes: data.bytes ?? data.transferredBytes ?? 0,
      totalBytes: data.totalBytes ?? data.total ?? 0, speed: data.speed || '', eta: data.eta || '',
    };
    if (data.phase === 'Completed') {
      active.value = false;
      paused.value = false;
      onComplete?.(data);
      notifyCompletion('completed');
    }
  };
  const onError = (data = {}) => {
    if (!active.value || !belongsToCurrentTask(data)) return;
    if (cancelRequested) {
      // The backend acknowledges our cancel with an installError
      // ("下载已取消"). The user already knows — swallow the error
      // surface and resolve the waiter as 'cancelled'.
      phase.value = 'Cancelled';
      active.value = false;
      paused.value = false;
      notifyCompletion('cancelled');
      return;
    }
    error.value = { message: data.error || data.message || String(data), details: data.details || data.error || String(data) };
    phase.value = 'error';
    active.value = false;
    paused.value = false;
    reportError?.(data);
    notifyCompletion('error');
  };

  const unsubscribeProgress = window.runtime?.EventsOn?.('installProgress', onProgress);
  const unsubscribeError = window.runtime?.EventsOn?.('installError', onError);

  async function install(tool, version, formFilter = '') {
    return installInternal(tool, version, false, formFilter);
  }

  // installConfirmed is like install but calls InstallToolConfirmed with
  // confirmed=true so the backend skips its native Wails MessageDialog
  // (which can be hidden behind the window). The frontend is responsible
  // for showing a Naive UI confirmation dialog before calling this.
  async function installConfirmed(tool, version, formFilter = '') {
    return installInternal(tool, version, true, formFilter);
  }

  async function installInternal(tool, version, confirmed, formFilter = '') {
    if (!tool || !version || active.value) return false;
    cancelRequested = false; // fresh item — reset the cancel race flag
    active.value = true;
    paused.value = false;
    taskId.value = `${tool}@${version}`;
    currentTool.value = tool;
    currentVersion.value = version;
    phase.value = 'Downloading';
    progress.value = { percent: 0, bytes: 0, totalBytes: 0, speed: '', eta: '' };
    error.value = null;
    try {
      if (formFilter) {
        await window.go.gui.App.InstallToolWithForm(tool, version, formFilter, confirmed);
      } else if (confirmed) {
        await window.go.gui.App.InstallToolConfirmed(tool, version, true);
      } else {
        await window.go.gui.App.InstallTool(tool, version);
      }
      return true;
    } catch (cause) {
      error.value = { message: String(cause?.message || cause), details: String(cause?.stack || cause) };
      phase.value = 'error';
      active.value = false;
      paused.value = false;
      reportError?.({ taskId: taskId.value, tool, version, error: error.value.message, details: error.value.details });
      return false;
    }
  }

  // repair force-reinstalls an existing (possibly incomplete) version.
  // The backend shows a native confirmation dialog before removing the
  // existing install directory; progress events flow as usual.
  async function repair(tool, version) {
    if (!tool || !version || active.value) return false;
    cancelRequested = false; // fresh item — reset the cancel race flag (mirrors installInternal)
    active.value = true;
    paused.value = false;
    taskId.value = `${tool}@${version}`;
    currentTool.value = tool;
    currentVersion.value = version;
    phase.value = 'Downloading';
    progress.value = { percent: 0, bytes: 0, totalBytes: 0, speed: '', eta: '' };
    error.value = null;
    try {
      await window.go.gui.App.RepairInstall(tool, version);
      return true;
    } catch (cause) {
      error.value = { message: String(cause?.message || cause), details: String(cause?.stack || cause) };
      phase.value = 'error';
      active.value = false;
      paused.value = false;
      reportError?.({ taskId: taskId.value, tool, version, error: error.value.message, details: error.value.details });
      return false;
    }
  }

  async function pause() {
    if (!active.value || paused.value || !taskId.value) return false;
    try { await window.go.gui.App.PauseDownload(taskId.value); paused.value = true; phase.value = 'Paused'; return true; }
    catch (cause) { error.value = { message: String(cause?.message || cause), details: String(cause?.stack || cause) }; return false; }
  }
  async function resume() {
    if (!active.value || !paused.value || !taskId.value) return false;
    try { await window.go.gui.App.ResumeDownload(taskId.value); paused.value = false; phase.value = 'Downloading'; return true; }
    catch (cause) { error.value = { message: String(cause?.message || cause), details: String(cause?.stack || cause) }; return false; }
  }
  async function cancel() {
    if (!active.value || !taskId.value) return false;
    cancelRequested = true; // backend will ack with a cancel installError
    try {
      const [tool, version] = taskId.value.split('@');
      await window.go.gui.App.CancelDownload(taskId.value);
      active.value = false; paused.value = false; phase.value = 'Cancelled';
      onCancel?.({ taskId: taskId.value, tool, version });
      notifyCompletion('cancelled');
      return true;
    }
    catch (cause) {
      // IPC failed — no event may ever arrive. Reset the flag so a
      // retry is possible; the waiter stays pending (the backend
      // timeout emit from Spec 035 F11 is the final backstop).
      cancelRequested = false;
      error.value = { message: String(cause?.message || cause), details: String(cause?.stack || cause) };
      return false;
    }
  }
  function dispose() {
    if (disposed) return;
    disposed = true;
    notifyCompletion('disposed'); // release any pending batch waiter
    if (typeof unsubscribeProgress === 'function') unsubscribeProgress();
    if (typeof unsubscribeError === 'function') unsubscribeError();
  }
  // waitForCompletion resolves when the current install finishes.
  // Results: 'completed' | 'error' | 'cancelled' | 'disposed' | 'idle'
  // ('idle' = no active install and no recorded result — e.g. all
  // events were lost; batch callers conservatively count it failed).
  function waitForCompletion() {
    if (!active.value) {
      const recorded = lastResult;
      lastResult = null;
      return Promise.resolve(recorded ?? 'idle');
    }
    lastResult = null;
    return new Promise((resolve) => { completionWaiter = resolve; });
  }

  if (getCurrentScope()) onScopeDispose(dispose);
  return { install, installConfirmed, repair, pause, resume, cancel, waitForCompletion, phase, progress, error, active, paused, taskId, dispose };
}

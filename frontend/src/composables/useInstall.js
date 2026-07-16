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

  const belongsToCurrentTask = (data) => data?.taskId
    ? data.taskId === taskId.value
    : data?.tool === currentTool.value && data?.version === currentVersion.value;
  const onProgress = (data = {}) => {
    if (!active.value || !belongsToCurrentTask(data)) return;
    phase.value = data.phase || phase.value;
    progress.value = {
      percent: Math.round(data.percent || 0), bytes: data.bytes ?? data.transferredBytes ?? 0,
      totalBytes: data.totalBytes ?? data.total ?? 0, speed: data.speed || '', eta: data.eta || '',
    };
    if (data.phase === 'Completed') {
      active.value = false;
      paused.value = false;
      onComplete?.(data);
    }
  };
  const onError = (data = {}) => {
    if (!active.value || !belongsToCurrentTask(data)) return;
    error.value = { message: data.error || data.message || String(data), details: data.details || data.error || String(data) };
    phase.value = 'error';
    active.value = false;
    paused.value = false;
    reportError?.(data);
  };

  const unsubscribeProgress = window.runtime?.EventsOn?.('installProgress', onProgress);
  const unsubscribeError = window.runtime?.EventsOn?.('installError', onError);

  async function install(tool, version) {
    if (!tool || !version || active.value) return false;
    active.value = true;
    paused.value = false;
    taskId.value = `${tool}@${version}`;
    currentTool.value = tool;
    currentVersion.value = version;
    phase.value = 'Downloading';
    progress.value = { percent: 0, bytes: 0, totalBytes: 0, speed: '', eta: '' };
    error.value = null;
    try {
      await window.go.gui.App.InstallTool(tool, version);
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
    try {
      const [tool, version] = taskId.value.split('@');
      await window.go.gui.App.CancelDownload(taskId.value);
      active.value = false; paused.value = false; phase.value = 'Cancelled';
      onCancel?.({ taskId: taskId.value, tool, version });
      return true;
    }
    catch (cause) { error.value = { message: String(cause?.message || cause), details: String(cause?.stack || cause) }; return false; }
  }
  function dispose() {
    if (disposed) return;
    disposed = true;
    if (typeof unsubscribeProgress === 'function') unsubscribeProgress();
    if (typeof unsubscribeError === 'function') unsubscribeError();
  }
  if (getCurrentScope()) onScopeDispose(dispose);
  return { install, pause, resume, cancel, phase, progress, error, active, paused, taskId, dispose };
}

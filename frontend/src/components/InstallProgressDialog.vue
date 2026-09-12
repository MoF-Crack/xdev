<template>
  <div v-if="show" class="install-modal">
    <div class="n-modal-mask" @click="requestClose(false)" />
    <!-- Spec 037 阶段 1：多项并行安装模式（items 非空时启用） -->
    <section v-if="multiItems.length" ref="dialog" class="n-modal n-modal--wide" role="dialog" aria-modal="true" tabindex="-1" :aria-label="t('installing')" @keydown="onKeydown">
      <h2>{{ t('install.parallel_title') || t('installing') }} <span class="batch-count">({{ completedCount }}/{{ multiItems.length }})</span></h2>
      <n-space vertical>
        <div v-for="row in multiItems" :key="row.tool + '@' + row.version" class="install-row" :class="{ 'install-row--done': !row.active && !row.error, 'install-row--error': row.error }" :data-batch-item="row.tool">
          <div class="install-row__head">
            <span class="install-row__name xdev-mono">{{ row.tool }}@{{ row.version }}</span>
            <span class="install-row__phase">{{ localizedRowPhase(row) }}</span>
            <n-button v-if="row.active" size="tiny" type="error" data-action="cancel-item" :aria-label="t('download_cancel') + ' ' + row.tool" @click="$emit('cancel-item', row)">{{ t('download_cancel') }}</n-button>
            <n-text v-else-if="row.error" class="install-row__err" :title="row.error.message">{{ row.error.message }}</n-text>
            <n-text v-else class="install-row__done">✔</n-text>
          </div>
          <n-progress type="line" :percentage="row.percent || 0" :aria-label="t('install.progress') + ' ' + row.tool" :status="row.error ? 'error' : row.active ? 'default' : 'success'" />
        </div>
        <n-button v-if="anyActive" data-action="background" @click="runInBackground">{{ t('install.background') }}</n-button>
        <div class="dialog-actions">
          <n-button v-if="anyActive" data-action="cancel-all" type="error" @click="$emit('cancel-all')">{{ t('download_cancel') }}</n-button>
          <n-button data-action="close" :disabled="anyActive" @click="requestClose(false)">{{ t('btn.close') }}</n-button>
        </div>
      </n-space>
    </section>
    <!-- 单项安装模式（原路径不变） -->
    <section v-else ref="dialog" class="n-modal" role="dialog" aria-modal="true" tabindex="-1" :aria-label="t('installing')" @keydown="onKeydown">
      <h2>{{ t('installing') }}</h2>
      <n-space vertical>
        <div role="status" aria-live="polite"><strong>{{ localizedPhase }}</strong><span v-if="progress.percent != null"> - {{ progress.percent }}%</span></div>
        <n-progress type="line" :percentage="progress.percent || 0" :aria-label="t('install.progress')" />
        <n-text v-if="progress.totalBytes">{{ formatBytes(progress.bytes) }} / {{ formatBytes(progress.totalBytes) }}</n-text>
        <n-text v-if="progress.speed">{{ progress.speed }}<span v-if="progress.eta"> ({{ t('install.eta') }}: {{ progress.eta }})</span></n-text>
        <n-alert v-if="error" class="install-error" type="error"><div class="install-error__message">{{ error.message }}</div>
        </n-alert>
        <n-space v-if="active">
          <n-button ref="pauseButton" data-action="pause" :disabled="paused" @click="$emit('pause')">{{ t('download_pause') }}</n-button>
          <n-button data-action="resume" :disabled="!paused" @click="$emit('resume')">{{ t('download_resume') }}</n-button>
          <n-button data-action="cancel" type="error" @click="$emit('cancel')">{{ t('download_cancel') }}</n-button>
        </n-space>
        <n-button v-if="active" data-action="background" @click="runInBackground">{{ t('install.background') }}</n-button>
        <div class="dialog-actions">
          <n-button v-if="error" data-action="copy-error" :disabled="copied" @click="copyError">{{ copied ? t('install.copied') : t('install.copy_error') }}</n-button>
          <n-button data-action="close" :disabled="active" @click="requestClose(false)">{{ t('btn.close') }}</n-button>
        </div>
      </n-space>
    </section>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { NAlert, NButton, NProgress, NSpace, NText } from 'naive-ui';
import { formatBytes } from '../utils/format.js';

const props = defineProps({ show: Boolean, active: Boolean, paused: Boolean, phase: { type: String, default: '' }, progress: { type: Object, default: () => ({}) }, error: Object, returnFocusTo: Object, t: { type: Function, required: true }, items: { type: Array, default: () => [] } });
const emit = defineEmits(['update:show', 'pause', 'resume', 'cancel', 'cancel-item', 'cancel-all']);
// Spec 037 阶段 1：多项模式派生态。
const multiItems = computed(() => (Array.isArray(props.items) ? props.items : []));
const anyActive = computed(() => multiItems.value.some((r) => r.active));
const completedCount = computed(() => multiItems.value.filter((r) => !r.active).length);
function localizedRowPhase(row) {
  const key = phaseKeyMap[row.phase];
  return key ? props.t(key) : row.phase || '';
}
const dialog = ref(null);
const pauseButton = ref(null);
const copied = ref(false);
let openingFocus = null;
let copiedTimer = null;

function focusTarget() {
  return pauseButton.value?.$el && !pauseButton.value.$el.disabled ? pauseButton.value.$el : dialog.value;
}
function keepFocusInside(event) {
  if (props.show && !dialog.value?.contains(event.target)) focusTarget()?.focus();
}
// Ticket 08 (audit-fixes / P1-36): the map must cover every phase the
// backend/frontend can emit — useInstall.js locally sets 'Cancelled',
// 'Paused' and 'error' after cancel/pause/failure; previously those fell
// through and the raw phase word was shown untranslated.
const phaseKeyMap = { Downloading: 'installing_downloading', 'Downloading...': 'installing_downloading', Extracting: 'installing_extracting', 'Extracting...': 'installing_extracting', Installing: 'installing_installing', 'Installing...': 'installing_installing', Completed: 'installing_completed', Cancelled: 'installing_cancelled', Paused: 'installing_paused', error: 'installing_error', Error: 'installing_error' };
const localizedPhase = computed(() => { const key = phaseKeyMap[props.phase]; return key ? props.t(key) : props.phase; });
function runInBackground() { emit('update:show', false); }
function requestClose(show) { if (props.active || anyActive.value) return; emit('update:show', show); }
function clearCopiedTimer() { if (copiedTimer) { clearTimeout(copiedTimer); copiedTimer = null; } }
async function copyError() {
  if (!props.error?.message) return;
  try {
    await navigator.clipboard.writeText(props.error.message);
    copied.value = true;
    clearCopiedTimer();
    copiedTimer = setTimeout(() => { copied.value = false; copiedTimer = null; }, 2000);
  } catch {
    copied.value = false;
  }
}
function onKeydown(event) {
  if (event.key === 'Escape') { event.preventDefault(); requestClose(false); return; }
  if (event.key !== 'Tab') return;
  const items = [...dialog.value.querySelectorAll('button:not(:disabled), [href], input:not(:disabled), [tabindex]:not([tabindex="-1"])')];
  if (!items.length) { event.preventDefault(); dialog.value.focus(); return; }
  const first = items[0];
  const last = items.at(-1);
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
}
watch(() => props.show, (visible, wasVisible) => {
  document.removeEventListener('focusin', keepFocusInside);
  if (visible) {
    openingFocus = props.returnFocusTo || document.activeElement;
    document.addEventListener('focusin', keepFocusInside);
    focusTarget()?.focus();
  } else if (wasVisible) {
    (props.returnFocusTo || openingFocus)?.focus?.();
  }
}, { flush: 'post', immediate: true });
onBeforeUnmount(() => { document.removeEventListener('focusin', keepFocusInside); clearCopiedTimer(); });
</script>

<style scoped>
.install-modal { position: fixed; inset: 0; z-index: 3000; display: grid; place-items: center; }
.n-modal-mask { position: absolute; inset: 0; background: var(--xdev-overlay); }
.n-modal { position: relative; width: min(440px, calc(100vw - 24px)); max-height: calc(100vh - 48px); overflow-y: auto; padding: var(--xdev-space-5); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-lg); background: var(--xdev-bg-panel); box-shadow: var(--xdev-shadow-lg); color: var(--xdev-text-primary); }
.n-modal:focus-visible, .n-modal:focus-within { box-shadow: var(--xdev-shadow-lg), var(--xdev-focus-ring); }
.n-modal--wide { width: min(560px, calc(100vw - 24px)); }
.batch-count { font-size: var(--xdev-font-size-sm); color: var(--xdev-text-muted); font-weight: 400; }
.install-row { display: flex; flex-direction: column; gap: var(--xdev-space-1); padding: var(--xdev-space-2) 0; border-bottom: 1px solid var(--xdev-border-subtle); }
.install-row:last-of-type { border-bottom: 0; }
.install-row__head { display: flex; align-items: center; gap: var(--xdev-space-2); }
.install-row__name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.install-row__phase { flex: 1; min-width: 0; font-size: var(--xdev-font-size-xs); color: var(--xdev-text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.install-row__err { max-width: 240px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--xdev-text-muted); }
.install-row__done { color: var(--xdev-text-muted); }
h2 { margin: 0 0 var(--xdev-space-4); font-size: var(--xdev-font-size-lg); }
.install-error__message { max-height: 200px; overflow-y: auto; word-break: break-word; overflow-wrap: anywhere; white-space: pre-wrap; }
.dialog-actions { display: flex; align-items: center; justify-content: flex-end; gap: var(--xdev-space-3); flex-wrap: wrap; }
.dialog-actions :deep(.n-button) { min-width: 96px; white-space: nowrap; }
</style>

<template>
  <div v-if="show" class="install-modal">
    <div class="n-modal-mask" @click="requestClose(false)" />
    <section ref="dialog" class="n-modal" role="dialog" aria-modal="true" tabindex="-1" :aria-label="t('installing')" @keydown="onKeydown">
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

const props = defineProps({ show: Boolean, active: Boolean, paused: Boolean, phase: { type: String, default: '' }, progress: { type: Object, default: () => ({}) }, error: Object, returnFocusTo: Object, t: { type: Function, required: true } });
const emit = defineEmits(['update:show', 'pause', 'resume', 'cancel']);
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
const phaseKeyMap = { Downloading: 'installing_downloading', 'Downloading...': 'installing_downloading', Extracting: 'installing_extracting', 'Extracting...': 'installing_extracting', Installing: 'installing_installing', 'Installing...': 'installing_installing', Completed: 'installing_completed' };
const localizedPhase = computed(() => { const key = phaseKeyMap[props.phase]; return key ? props.t(key) : props.phase; });
function runInBackground() { emit('update:show', false); }
function requestClose(show) { if (props.active) return; emit('update:show', show); }
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
h2 { margin: 0 0 var(--xdev-space-4); font-size: var(--xdev-font-size-lg); }
.install-error__message { max-height: 200px; overflow-y: auto; word-break: break-word; overflow-wrap: anywhere; white-space: pre-wrap; }
.dialog-actions { display: flex; align-items: center; justify-content: flex-end; gap: var(--xdev-space-3); flex-wrap: wrap; }
.dialog-actions :deep(.n-button) { min-width: 96px; white-space: nowrap; }
</style>

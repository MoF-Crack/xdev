<template>
  <div v-if="show" class="install-modal">
    <div class="n-modal-mask" @click="requestClose(false)" />
    <section ref="dialog" class="n-modal" role="dialog" aria-modal="true" tabindex="-1" :aria-label="t('installing')" @keydown="onKeydown">
      <h2>{{ t('installing') }}</h2>
      <n-space vertical>
        <div role="status" aria-live="polite"><strong>{{ phase }}</strong><span v-if="progress.percent != null"> - {{ progress.percent }}%</span><span v-if="error"> - {{ error.message }}</span></div>
        <n-progress type="line" :percentage="progress.percent || 0" :aria-label="t('install.progress')" />
        <n-text v-if="progress.totalBytes">{{ formatBytes(progress.bytes) }} / {{ formatBytes(progress.totalBytes) }}</n-text>
        <n-text v-if="progress.speed">{{ progress.speed }}<span v-if="progress.eta"> (ETA: {{ progress.eta }})</span></n-text>
        <n-alert v-if="error" type="error"><div>{{ error.message }}</div><CopyableValue v-if="error.details" :value="error.details" /></n-alert>
        <n-space v-if="active">
          <n-button ref="pauseButton" data-action="pause" :disabled="paused" @click="$emit('pause')">{{ t('download_pause') }}</n-button>
          <n-button data-action="resume" :disabled="!paused" @click="$emit('resume')">{{ t('download_resume') }}</n-button>
          <n-button data-action="cancel" type="error" @click="$emit('cancel')">{{ t('download_cancel') }}</n-button>
        </n-space>
        <n-button data-action="close" :disabled="active" @click="requestClose(false)">{{ t('btn.close') }}</n-button>
      </n-space>
    </section>
  </div>
</template>

<script setup>
import { onBeforeUnmount, ref, watch } from 'vue';
import { NAlert, NButton, NProgress, NSpace, NText } from 'naive-ui';
import CopyableValue from './CopyableValue.vue';

const props = defineProps({ show: Boolean, active: Boolean, paused: Boolean, phase: { type: String, default: '' }, progress: { type: Object, default: () => ({}) }, error: Object, returnFocusTo: Object, t: { type: Function, required: true } });
const emit = defineEmits(['update:show', 'pause', 'resume', 'cancel']);
const dialog = ref(null);
const pauseButton = ref(null);
let openingFocus = null;

function focusTarget() {
  return pauseButton.value?.$el && !pauseButton.value.$el.disabled ? pauseButton.value.$el : dialog.value;
}
function keepFocusInside(event) {
  if (props.show && !dialog.value?.contains(event.target)) focusTarget()?.focus();
}
function requestClose(show) { if (!props.active) emit('update:show', show); }
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
onBeforeUnmount(() => document.removeEventListener('focusin', keepFocusInside));
function formatBytes(value = 0) { if (value < 1024) return `${value} B`; const units = ['KB', 'MB', 'GB']; let size = value / 1024, index = 0; while (size >= 1024 && index < units.length - 1) { size /= 1024; index++; } return `${size.toFixed(1)} ${units[index]}`; }
</script>

<style scoped>
.install-modal { position: fixed; inset: 0; z-index: 3000; display: grid; place-items: center; }
.n-modal-mask { position: absolute; inset: 0; background: rgba(0, 0, 0, 0.56); }
.n-modal { position: relative; width: min(440px, calc(100vw - 24px)); padding: var(--xdev-space-5); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-lg); background: var(--xdev-bg-panel); box-shadow: var(--xdev-shadow-lg); color: var(--xdev-text-primary); }
.n-modal:focus-visible, .n-modal:focus-within { box-shadow: var(--xdev-shadow-lg), var(--xdev-focus-ring); }
h2 { margin: 0 0 var(--xdev-space-4); font-size: var(--xdev-font-size-lg); }
</style>

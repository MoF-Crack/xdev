<template>
  <div v-if="show" class="self-update-modal" data-testid="self-update-dialog">
    <div class="self-update-mask" @click="requestClose" />
    <section ref="panel" class="self-update-panel" role="dialog" aria-modal="true" tabindex="-1"
      :aria-labelledby="titleId" :aria-describedby="descriptionId" :aria-busy="busy" @keydown="onKeydown">
      <div class="self-update__heading">
        <h2 :id="titleId">{{ title }}</h2>
        <n-button ref="closeButton" data-action="self-update-close" size="small" secondary :disabled="busy"
          :aria-label="t('btn.close')" @click="requestClose">{{ t('btn.close') }}</n-button>
      </div>
      <p :id="descriptionId" class="self-update__text">
        {{ effectiveStatus === 'applied' ? t('self_update.applied', { latest })
          : effectiveStatus === 'up-to-date' ? t('self_update.no_update_applied')
          : current && latest ? t('self_update.text', { current, latest }) : t('settings.self_update') }}
      </p>
      <p v-if="effectiveStatus === 'available'" class="self-update__text">{{ t('self_update.confirm') }}</p>
      <div v-if="error" class="self-update__error" role="alert" data-testid="self-update-error">
        <strong>{{ t(error.kind === 'check' ? 'update_check_failed' : 'self_update.error_title') }}</strong>
        <p>{{ error.message }}</p>
      </div>
      <div v-if="checking || updating" class="self-update__progress" data-testid="self-update-progress">
        <p role="status" aria-live="polite">
          {{ checking ? t('self_update.checking') : progress ? t(`self_update.phase.${progress.phase}`) : t('self_update.preparing') }}
        </p>
        <template v-if="progress?.phase === 'downloading'">
          <progress max="100" :value="downloadPercent" :aria-label="t('self_update.download_progress')" />
          <p class="xdev-mono" data-testid="self-update-bytes">
            {{ formatBytes(progress.downloadedBytes) }}<template v-if="progress.totalBytes > 0"> / {{ formatBytes(progress.totalBytes) }}</template>
            <template v-if="downloadPercent != null"> · {{ downloadPercent }}%</template>
          </p>
        </template>
      </div>
      <p v-if="busy" class="self-update__hint">{{ t('self_update.busy') }}</p>
      <p v-if="effectiveStatus === 'applied'" class="self-update__hint" role="status" data-testid="self-update-restart">{{ t('self_update.restart_hint') }}</p>
      <ul v-if="!busy && effectiveStatus === 'available' && downloads.length" class="self-update__downloads">
        <li v-for="download in downloads" :key="download.name">
          <a :href="download.url" target="_blank" rel="noopener noreferrer">{{ t('self_update.download_archive') }} · {{ download.name }}</a>
        </li>
      </ul>
      <div class="dialog-actions">
        <template v-if="source === 'startup' && (effectiveStatus === 'available' || effectiveStatus === 'updating')">
          <n-button data-action="self-update-quit" :disabled="busy" :aria-label="t('self_update.quit')" @click="requestAction('quit')">{{ t('self_update.quit') }}</n-button>
          <n-button data-action="self-update-ignore" secondary :disabled="busy" :aria-label="t('self_update.ignore')" @click="requestAction('ignore')">{{ t('self_update.ignore') }}</n-button>
        </template>
        <n-button v-if="effectiveStatus === 'error' || checking" data-action="self-update-retry" type="primary" :disabled="busy" :loading="checking"
          :aria-label="t('btn.retry')" @click="requestAction('retry')">{{ t('btn.retry') }}</n-button>
        <n-button v-else-if="effectiveStatus === 'available' || effectiveStatus === 'updating'" data-action="self-update-update" type="primary" :disabled="busy" :loading="updating"
          :aria-label="t('self_update.download_install')" @click="requestAction('update')">{{ t('self_update.download_install') }}</n-button>
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue';
import { NButton } from 'naive-ui';
import { formatBytes } from '../utils/format.js';

const props = defineProps({
  show: Boolean,
  current: { type: String, default: '' },
  latest: { type: String, default: '' },
  updating: Boolean,
  checking: Boolean,
  dismissing: Boolean,
  error: { type: Object, default: null },
  progress: { type: Object, default: null },
  status: { type: String, default: 'available' },
  source: { type: String, default: 'startup' },
  downloadUrls: { type: Object, default: () => ({}) },
  returnFocusTo: Object,
  t: { type: Function, required: true },
});
const emit = defineEmits(['update', 'ignore', 'quit', 'close', 'retry']);
const panel = ref(null);
const closeButton = ref(null);
const submitted = ref(false);
const titleId = `${useId()}-self-update-title`;
const descriptionId = `${titleId}-description`;
const busy = computed(() => props.updating || props.checking || props.dismissing || submitted.value);
const effectiveStatus = computed(() => props.updating ? 'updating' : props.checking ? 'checking' : props.dismissing ? 'dismissing' : props.error ? 'error' : props.status);
const title = computed(() => props.t(effectiveStatus.value === 'applied' ? 'self_update.applied_title' : effectiveStatus.value === 'error' ? 'self_update.error_title' : effectiveStatus.value === 'up-to-date' ? 'settings.self_update' : 'self_update.title'));
const downloadPercent = computed(() => props.progress?.totalBytes > 0 && typeof props.progress.percent === 'number' && Number.isFinite(props.progress.percent)
  ? Math.max(0, Math.min(100, props.progress.percent)) : undefined);
const downloads = computed(() => Object.entries(props.downloadUrls || {}).flatMap(([name, url]) => {
  if (!name || typeof url !== 'string') return [];
  try { return new URL(url).protocol === 'https:' ? [{ name, url }] : []; } catch { return []; }
}));
let openingFocus = null;

function focusInside() {
  const button = closeButton.value?.$el;
  // Native focus scrolls the dialog's own overflow when a control was below
  // the fold; the fixed modal does not scroll the underlying page.
  (button && !button.disabled ? button : panel.value)?.focus();
}
function keepFocusInside(event) {
  if (props.show && panel.value && !panel.value.contains(event.target)) focusInside();
}
function restoreFocus() {
  const target = props.returnFocusTo?.$el || props.returnFocusTo || openingFocus;
  if (target?.isConnected) target.focus?.({ preventScroll: true });
}
function requestClose() { if (!busy.value) emit('close'); }
function requestAction(action) {
  if (busy.value) return;
  // Lock synchronously, before a parent prop update renders, so repeated clicks
  // cannot emit two apply/retry/ignore requests in the same event turn.
  submitted.value = true;
  emit(action);
}
function onKeydown(event) {
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); requestClose(); return; }
  if (event.key !== 'Tab') return;
  const items = [...panel.value.querySelectorAll('button:not(:disabled), [href], input:not(:disabled), [tabindex]:not([tabindex="-1"])')]
    .filter(item => item.tabIndex >= 0 && item.getAttribute('aria-disabled') !== 'true' && !item.closest('[hidden], [inert]'));
  if (!items.length) { event.preventDefault(); panel.value.focus(); return; }
  const first = items[0];
  const last = items.at(-1);
  if (event.shiftKey && (document.activeElement === first || document.activeElement === panel.value)) { event.preventDefault(); last.focus(); }
  else if (!event.shiftKey && (document.activeElement === last || !items.includes(document.activeElement))) { event.preventDefault(); first.focus(); }
}
watch(() => [props.show, props.updating, props.checking, props.dismissing, props.status, props.error], () => { submitted.value = false; }, { flush: 'sync' });
watch(() => props.show, async (visible, wasVisible) => {
  document.removeEventListener('focusin', keepFocusInside);
  if (visible) {
    openingFocus = props.returnFocusTo || document.activeElement;
    document.addEventListener('focusin', keepFocusInside);
    await nextTick();
    if (props.show) focusInside();
  } else if (wasVisible) {
    restoreFocus();
  }
}, { flush: 'post', immediate: true });
watch(busy, () => {
  if (!props.show || !panel.value) return;
  const active = document.activeElement;
  if (active === panel.value || !panel.value.contains(active) || active?.disabled) focusInside();
}, { flush: 'post' });
onBeforeUnmount(() => { document.removeEventListener('focusin', keepFocusInside); if (props.show) restoreFocus(); });
</script>

<style scoped>
.self-update-modal { position: fixed; inset: 0; z-index: 3000; display: grid; place-items: center; }
.self-update-mask { position: absolute; inset: 0; background: var(--xdev-overlay); }
.self-update-panel { position: relative; width: min(520px, calc(100vw - 24px)); max-height: calc(100vh - 48px); overflow-y: auto; padding: var(--xdev-space-5); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-lg); background: var(--xdev-bg-panel); box-shadow: var(--xdev-shadow-lg); color: var(--xdev-text-primary); }
.self-update-panel:focus-visible { outline: 2px solid var(--xdev-accent); outline-offset: 2px; }
.self-update__heading { display: flex; align-items: start; justify-content: space-between; gap: var(--xdev-space-3); margin-bottom: var(--xdev-space-4); }
h2 { margin: 0; font-size: var(--xdev-font-size-lg); }
.self-update__text { margin: 0 0 var(--xdev-space-4); line-height: 1.6; }
.self-update__error { padding: var(--xdev-space-3); margin-bottom: var(--xdev-space-4); border: 1px solid var(--xdev-error); border-radius: var(--xdev-radius-sm); background: var(--xdev-error-soft); color: var(--xdev-error); overflow-wrap: anywhere; }
.self-update__error p { margin: var(--xdev-space-2) 0 0; white-space: pre-wrap; }
.self-update__hint { color: var(--xdev-text-secondary); line-height: 1.6; }
.self-update__progress p { margin: var(--xdev-space-2) 0; }
progress { display: block; width: 100%; height: 12px; accent-color: var(--xdev-accent); }
.self-update__downloads { display: grid; gap: var(--xdev-space-2); margin: var(--xdev-space-4) 0; padding-left: var(--xdev-space-4); }
.self-update__downloads a { color: var(--xdev-accent); overflow-wrap: anywhere; }
.self-update__downloads a:focus-visible { outline: 2px solid var(--xdev-accent); outline-offset: 2px; }
.dialog-actions { display: flex; align-items: center; justify-content: flex-end; gap: var(--xdev-space-3); flex-wrap: wrap; }
.dialog-actions :deep(.n-button) { min-width: 96px; white-space: normal; }
</style>

<template>
  <span class="confirm-action">
    <span ref="trigger" class="confirm-action__trigger" @click="open"><slot name="trigger" /></span>
    <span v-if="visible" class="confirm-action__backdrop">
      <span ref="dialog" class="confirm-action__dialog" role="dialog" aria-modal="true" tabindex="-1" :aria-labelledby="titleId" :aria-describedby="description ? descriptionId : undefined" @keydown="onKeydown">
        <strong :id="titleId">{{ title }}</strong>
        <span v-if="description" :id="descriptionId" class="confirm-action__description">{{ description }}</span>
        <span class="confirm-action__actions">
          <button ref="cancelButton" type="button" class="xdev-btn xdev-btn--secondary" data-action="cancel" :disabled="cancelDisabled" @click="cancel">{{ cancelLabel }}</button>
          <button ref="confirmButton" type="button" class="xdev-btn xdev-btn--danger confirm-action__danger" data-action="confirm" :disabled="pending || submitted" @click="confirm">{{ confirmLabel }}</button>
        </span>
      </span>
    </span>
  </span>
</template>

<script setup>
import { nextTick, onBeforeUnmount, ref, watch } from 'vue';
const props = defineProps({ title: { type: String, required: true }, description: { type: String, default: '' }, confirmLabel: { type: String, default: 'Confirm' }, cancelLabel: { type: String, default: 'Cancel' }, initialFocus: { type: String, default: 'cancel', validator: (value) => ['cancel', 'confirm'].includes(value) }, pending: Boolean, cancelDisabled: Boolean });
const emit = defineEmits(['cancel', 'confirm']);
const visible = ref(false);
const trigger = ref(null);
const dialog = ref(null);
const cancelButton = ref(null);
const confirmButton = ref(null);
const submitted = ref(false);
const titleId = `confirm-action-${Math.random().toString(36).slice(2)}`;
const descriptionId = `${titleId}-description`;
function focusTarget() {
  if (props.cancelDisabled && (props.pending || submitted.value)) return dialog.value;
  if (props.initialFocus === 'confirm' && !props.pending && !submitted.value) return confirmButton.value;
  return props.cancelDisabled ? dialog.value : cancelButton.value;
}
async function focusInside() { await nextTick(); focusTarget()?.focus(); }
async function open() { submitted.value = false; visible.value = true; document.addEventListener('focusin', keepFocusInside); await focusInside(); }
function keepFocusInside(event) { if (visible.value && !dialog.value?.contains(event.target)) focusTarget()?.focus(); }
function onKeydown(event) {
  if (event.key === 'Escape' && !props.cancelDisabled) { event.preventDefault(); cancel(); return; }
  if (event.key !== 'Tab') return;
  const available = [cancelButton.value, confirmButton.value].filter((button) => button && !button.disabled);
  if (!available.length) { event.preventDefault(); dialog.value?.focus(); return; }
  const first = available[0];
  const last = available[available.length - 1];
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
}
function cancel() { if (!props.cancelDisabled) close('cancel'); }
function confirm(event) { if (props.pending || submitted.value) return; submitted.value = true; if (event?.currentTarget) event.currentTarget.disabled = true; close('confirm'); }
async function close(event) { visible.value = false; document.removeEventListener('focusin', keepFocusInside); emit(event); await nextTick(); trigger.value?.querySelector('button, [href], [tabindex]:not([tabindex="-1"])')?.focus(); }
onBeforeUnmount(() => document.removeEventListener('focusin', keepFocusInside));
watch(() => [props.pending, props.cancelDisabled], () => { if (visible.value) focusTarget()?.focus(); }, { flush: 'post' });
</script>

<style scoped>
.confirm-action, .confirm-action__trigger { display: inline-flex; }
.confirm-action__backdrop { position: fixed; inset: 0; z-index: 1000; display: grid; place-items: center; padding: var(--xdev-space-4); background: var(--xdev-overlay); }
.confirm-action__dialog { display: grid; width: min(420px, 100%); gap: var(--xdev-space-3); padding: var(--xdev-space-6); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-lg); background: var(--xdev-bg-panel); box-shadow: var(--xdev-shadow-lg); color: var(--xdev-text-primary); }
.confirm-action__description { color: var(--xdev-text-muted); line-height: 1.5; }
.confirm-action__actions { display: flex; justify-content: flex-end; gap: var(--xdev-space-2); margin-top: var(--xdev-space-2); }
button { padding: var(--xdev-space-2) var(--xdev-space-3); border: 1px solid var(--xdev-border-strong); border-radius: var(--xdev-radius-md); background: var(--xdev-bg-elevated); color: var(--xdev-text-primary); cursor: pointer; }
.confirm-action__danger { border-color: var(--xdev-error); background: var(--xdev-error-soft); color: var(--xdev-error); font-weight: 700; }
</style>

<template>
  <span class="copyable-value">
    <code class="xdev-mono" :title="value">{{ displayValue }}</code>
    <button type="button" class="xdev-btn xdev-btn--secondary xdev-btn--sm" :aria-label="copyLabel" :disabled="copying" @click="copy"><slot name="icon">Copy</slot></button>
    <span v-if="feedback" class="copyable-value__feedback" role="status" :data-announcement="announcement">{{ feedback }}</span>
  </span>
</template>

<script setup>
import { computed, nextTick, ref } from 'vue';
const props = defineProps({ value: { type: String, required: true }, copyLabel: { type: String, default: 'Copy value' }, successLabel: { type: String, default: 'Copied' }, failureLabel: { type: String, default: 'Copy failed' }, maxLength: { type: Number, default: 36 } });
const emit = defineEmits(['success', 'failure']);
const feedback = ref('');
const copying = ref(false);
const announcement = ref(0);
const displayValue = computed(() => {
  if (props.value.length <= props.maxLength) return props.value;
  const side = Math.floor((props.maxLength - 1) / 2);
  return `${props.value.slice(0, side)}…${props.value.slice(-side)}`;
});
async function copy() {
  if (copying.value) return;
  copying.value = true;
  feedback.value = '';
  await nextTick();
  try {
    await navigator.clipboard.writeText(props.value);
    feedback.value = props.successLabel;
    announcement.value += 1;
    emit('success', props.value);
  } catch (error) {
    feedback.value = props.failureLabel;
    announcement.value += 1;
    emit('failure', error);
  } finally {
    copying.value = false;
  }
}
</script>

<style scoped>
.copyable-value { display: inline-flex; align-items: center; gap: var(--xdev-space-2); min-width: 0; }
code { overflow: hidden; color: var(--xdev-text-secondary); white-space: nowrap; }
button { padding: var(--xdev-space-1) var(--xdev-space-2); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-sm); background: transparent; color: var(--xdev-text-muted); cursor: pointer; }
button:hover { color: var(--xdev-text-primary); border-color: var(--xdev-border-strong); }
.copyable-value__feedback { color: var(--xdev-text-muted); font-size: var(--xdev-font-size-xs); }
</style>

<template>
  <span class="copyable-value">
    <button type="button" class="copyable-value__path xdev-mono" :aria-label="copyLabel" :title="hoverTitle" :disabled="copying" @click="copy">{{ displayValue }}</button>
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
const hoverTitle = computed(() => `${props.value}\n${props.copyLabel}`);
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
.copyable-value__path { overflow: hidden; max-width: 100%; padding: 0; border: 0; background: transparent; color: var(--xdev-text-secondary); white-space: nowrap; text-overflow: ellipsis; cursor: pointer; text-align: left; }
.copyable-value__path:hover { color: var(--xdev-text-primary); text-decoration: underline; text-underline-offset: 2px; }
.copyable-value__path:disabled { cursor: wait; }
.copyable-value__feedback { color: var(--xdev-text-muted); font-size: var(--xdev-font-size-xs); }
</style>

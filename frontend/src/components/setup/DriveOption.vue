<template>
  <label class="drive-option" :class="{ selected: modelValue === drive.letter }">
    <input
      type="radio"
      name="setup-drive"
      :value="drive.letter"
      :checked="modelValue === drive.letter"
      @change="$emit('update:modelValue', drive.letter)"
    >
    <span class="drive-option__content">
      <span class="drive-option__heading">
        <strong>{{ drive.letter }}</strong>
        <span v-if="system" class="drive-option__badge danger">{{ systemLabel }}</span>
        <span v-else-if="recommended" class="drive-option__badge success">{{ recommendedLabel }}</span>
      </span>
      <span class="drive-option__space">{{ space }}</span>
      <span v-if="system" class="drive-option__reason">{{ systemReason }}</span>
      <span v-else-if="recommended" class="drive-option__reason">{{ recommendedReason }}</span>
    </span>
  </label>
</template>

<script setup>
defineProps({
  drive: { type: Object, required: true }, modelValue: { type: String, default: '' },
  system: Boolean, recommended: Boolean, space: { type: String, required: true },
  systemLabel: { type: String, required: true }, recommendedLabel: { type: String, required: true },
  systemReason: { type: String, required: true }, recommendedReason: { type: String, required: true },
});
defineEmits(['update:modelValue']);
</script>

<style scoped>
.drive-option { display: flex; gap: 12px; padding: 12px 14px; border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-md); cursor: pointer; }
.drive-option:hover { background: var(--xdev-accent-soft); border-color: var(--xdev-border-strong); }
.drive-option:focus-within { box-shadow: var(--xdev-focus-ring); }
.drive-option.selected { background: var(--xdev-accent-soft); border-color: var(--xdev-accent); }
.drive-option input { flex: 0 0 auto; margin-top: 4px; accent-color: var(--xdev-accent); }
.drive-option__content { display: grid; min-width: 0; gap: 3px; }
.drive-option__heading { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.drive-option__badge { padding: 2px 7px; border-radius: var(--xdev-radius-pill); font-size: 11px; font-weight: 600; }
.drive-option__badge.danger { color: var(--xdev-error); background: var(--xdev-error-soft); }
.drive-option__badge.success { color: var(--xdev-success); background: var(--xdev-success-soft); }
.drive-option__space, .drive-option__reason { color: var(--xdev-text-muted); font-size: 12px; }
</style>

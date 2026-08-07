<template>
  <fieldset class="form-section">
    <legend>{{ title }}</legend>
    <p v-if="description" class="form-section__description">{{ description }}</p>
    <div class="form-section__fields"><slot /></div>
    <div v-if="$slots.actions" class="form-section__actions"><slot name="actions" /></div>
  </fieldset>
</template>

<script setup>
defineProps({ title: { type: String, required: true }, description: { type: String, default: '' } });
</script>

<style scoped>
.form-section { min-width: 0; margin: 0; padding: var(--xdev-space-5) 0; border: 0; border-bottom: 1px solid var(--xdev-border-subtle); }
legend { padding: 0; color: var(--xdev-text-primary); font-size: var(--xdev-font-size-lg); font-weight: 600; }
.form-section__description { margin: var(--xdev-space-1) 0 0; color: var(--xdev-text-muted); font-size: var(--xdev-font-size-sm); }
.form-section__fields { display: grid; gap: var(--xdev-space-4); margin-top: var(--xdev-space-4); }
.form-section__actions { display: flex; justify-content: flex-end; gap: var(--xdev-space-2); margin-top: var(--xdev-space-5); flex-wrap: wrap; }
.form-section__actions :deep(button) { min-height: 36px; }
@media (max-width: 560px) { .form-section__actions { justify-content: stretch; } .form-section__actions :deep(button) { flex: 1 1 140px; } }
</style>

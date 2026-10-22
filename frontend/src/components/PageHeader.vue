<template>
  <header class="page-header">
    <div class="page-header__copy">
      <h1><slot name="title">{{ title }}</slot></h1>
      <div v-if="$slots.description || description" class="page-header__description">
        <slot name="description">{{ description }}</slot>
      </div>
    </div>
    <div v-if="$slots.primary || $slots.secondary" class="page-header__actions">
      <div v-if="$slots.secondary" class="page-header__secondary"><slot name="secondary" /></div>
      <slot name="primary" />
    </div>
  </header>
</template>

<script setup>
defineProps({ title: { type: String, default: '' }, description: { type: String, default: '' } });
</script>

<style scoped>
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--xdev-space-6); }
.page-header__copy { min-width: 0; }
h1 { margin: 0; color: var(--xdev-text-primary); font-size: 20px; font-weight: 650; letter-spacing: -0.01em; line-height: 1.25; }
.page-header__description { margin-top: var(--xdev-space-2); color: var(--xdev-text-muted); line-height: 1.5; }
.page-header__actions, .page-header__secondary { display: flex; align-items: center; gap: var(--xdev-space-3); flex-wrap: wrap; }
@media (max-width: 799px) { .page-header { flex-direction: column; } .page-header__actions { width: 100%; } }
</style>

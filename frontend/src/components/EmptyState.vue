<template>
  <div class="empty-state" role="status">
    <div v-if="$slots.icon" class="empty-state__icon"><slot name="icon" /></div>
    <h3>{{ title }}</h3>
    <p v-if="description">{{ description }}</p>
    <div v-if="$slots.action" class="empty-state__action"><slot name="action" /></div>
  </div>
</template>

<script setup>
defineProps({ title: { type: String, required: true }, description: { type: String, default: '' } });
</script>

<style scoped>
.empty-state { display: grid; justify-items: center; padding: var(--xdev-space-8); text-align: center; color: var(--xdev-text-muted); }
.empty-state__icon { margin-bottom: var(--xdev-space-3); color: var(--xdev-text-dim); font-size: 28px; }
h3 { margin: 0; color: var(--xdev-text-primary); font-size: var(--xdev-font-size-lg); }
p { max-width: 52ch; margin: var(--xdev-space-2) 0 0; line-height: 1.5; }
.empty-state__action { margin-top: var(--xdev-space-4); }
</style>

<template>
  <span class="status-badge" :class="`status-badge--${tone}`" :data-tone="tone" :role="live ? 'status' : undefined">
    <span v-if="$slots.icon" class="status-badge__icon"><slot name="icon" /></span><span><slot /></span>
  </span>
</template>

<script setup>
defineProps({ live: Boolean, tone: { type: String, default: 'neutral', validator: (value) => ['neutral', 'info', 'success', 'warning', 'error'].includes(value) } });
</script>

<style scoped>
.status-badge { display: inline-flex; align-items: center; gap: var(--xdev-space-1); padding: 3px var(--xdev-space-2); border-radius: var(--xdev-radius-pill); background: var(--xdev-bg-muted); color: var(--xdev-text-secondary); font-size: var(--xdev-font-size-xs); font-weight: 600; }
  .status-badge--info { color: var(--xdev-text-primary); background: var(--xdev-info-soft); }
.status-badge--success { color: var(--xdev-success); background: var(--xdev-success-soft); }
.status-badge--warning { color: var(--xdev-warning); background: var(--xdev-warning-soft); }
.status-badge--error { color: var(--xdev-error); background: var(--xdev-error-soft); }
.status-badge__icon { display: inline-flex; }
</style>

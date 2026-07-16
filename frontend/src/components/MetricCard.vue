<template>
  <article class="metric-card">
    <span class="metric-card__label">{{ label }}</span>
    <strong class="metric-card__value">{{ value }}</strong>
  </article>
</template>

<script setup>
defineProps({ label: { type: String, required: true }, value: { type: [String, Number], default: 0 } });
</script>

<style scoped>
.metric-card { min-width: 0; padding: var(--xdev-space-4); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-md); background: var(--xdev-bg-panel); }
.metric-card__label { display: block; margin-bottom: var(--xdev-space-2); color: var(--xdev-text-muted); font-size: 12px; }
.metric-card__value { color: var(--xdev-text-primary); font-size: clamp(24px, 3vw, 34px); font-variant-numeric: tabular-nums; letter-spacing: -0.04em; }
</style>

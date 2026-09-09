<template>
  <button v-if="clickable" type="button" class="metric-card metric-card--clickable" @click="$emit('click')">
    <span class="metric-card__label">{{ label }}</span>
    <span class="metric-card__value-row"><strong class="metric-card__value">{{ value }}</strong><span class="metric-card__chevron" aria-hidden="true">›</span></span>
    <span v-if="hint" class="metric-card__hint">{{ hint }}</span>
  </button>
  <article v-else class="metric-card">
    <span class="metric-card__label">{{ label }}</span>
    <strong class="metric-card__value">{{ value }}</strong>
  </article>
</template>

<script setup>
defineProps({ label: { type: String, required: true }, value: { type: [String, Number], default: 0 }, clickable: Boolean, hint: { type: String, default: '' } });
defineEmits(['click']);
</script>

<style scoped>
.metric-card { min-width: 0; padding: var(--xdev-space-4); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-md); background: var(--xdev-bg-card); }
.metric-card--clickable { width: 100%; text-align: left; color: inherit; font: inherit; cursor: pointer; transition: border-color var(--xdev-motion-fast) var(--xdev-ease-standard); }
.metric-card--clickable:hover { border-color: var(--xdev-accent); }
.metric-card--clickable:focus-visible { outline: none; box-shadow: var(--xdev-focus-ring); }
.metric-card__value-row { display: flex; align-items: center; justify-content: space-between; gap: var(--xdev-space-2); }
.metric-card__chevron { color: var(--xdev-text-dim); font-size: 26px; line-height: 1; transition: color var(--xdev-motion-fast), transform var(--xdev-motion-fast); }
.metric-card--clickable:hover .metric-card__chevron { color: var(--xdev-accent); transform: translateX(2px); }
.metric-card__hint { display: block; margin-top: var(--xdev-space-2); color: var(--xdev-text-muted); font-size: var(--xdev-font-size-xs); }
.metric-card__label { display: block; margin-bottom: var(--xdev-space-3); color: var(--xdev-text-muted); font-size: 11px; font-weight: 600; letter-spacing: 0.1em; text-transform: uppercase; }
.metric-card__value { color: var(--xdev-text-primary); font-size: clamp(22px, 2.6vw, 30px); font-weight: 650; font-variant-numeric: tabular-nums; letter-spacing: -0.02em; }
</style>

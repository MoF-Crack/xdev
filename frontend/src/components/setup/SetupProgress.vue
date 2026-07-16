<template>
  <nav class="setup-progress" :aria-label="label">
    <ol>
      <li
        v-for="(item, index) in steps"
        :key="item"
        class="setup-progress__step"
        :class="{ complete: index + 1 < current }"
        :aria-current="index + 1 === current ? 'step' : undefined"
      >
        <span class="setup-progress__number" aria-hidden="true">{{ index + 1 }}</span>
        <span>{{ item }}</span>
      </li>
    </ol>
  </nav>
</template>

<script setup>
defineProps({
  current: { type: Number, required: true },
  label: { type: String, required: true },
  steps: { type: Array, required: true },
});
</script>

<style scoped>
.setup-progress ol { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; padding: 0; margin: 0; list-style: none; }
.setup-progress__step { display: flex; align-items: center; gap: 8px; color: var(--xdev-text-muted); font-size: 13px; }
.setup-progress__number { display: grid; flex: 0 0 24px; height: 24px; place-items: center; border: 1px solid var(--xdev-border-strong); border-radius: 50%; }
.setup-progress__step[aria-current="step"] { color: var(--xdev-text-primary); font-weight: 600; }
.setup-progress__step[aria-current="step"] .setup-progress__number { color: var(--xdev-text-on-accent); background: var(--xdev-accent); border-color: var(--xdev-accent); }
.setup-progress__step.complete .setup-progress__number { color: var(--xdev-success); border-color: var(--xdev-success); }
@media (max-width: 480px) { .setup-progress__step { align-items: flex-start; flex-direction: column; } }
</style>

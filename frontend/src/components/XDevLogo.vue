<template>
  <svg
    class="xdev-logo"
    :class="`xdev-logo--${variant}`"
    :viewBox="variant === 'wordmark' ? F2_WORDMARK_VIEW_BOX : F2_SYMBOL_VIEW_BOX"
    fill="none"
    focusable="false"
    :role="decorative ? undefined : 'img'"
    :aria-label="decorative ? undefined : 'XDev'"
    :aria-hidden="decorative ? 'true' : undefined"
    :data-single-x="variant === 'wordmark' ? 'true' : undefined"
  >
    <g
      class="xdev-logo__symbol"
      data-letter="x"
      :transform="variant === 'wordmark' ? F2_WORDMARK_SYMBOL_TRANSFORM : undefined"
    >
      <path v-for="path in F2_SYMBOL_PATHS" :key="path" :d="path" fill="currentColor" />
    </g>
    <g v-if="variant === 'wordmark'" class="xdev-logo__dev" data-letters="dev" :transform="F2_WORDMARK_DEV_TRANSFORM">
      <path :d="F2_DEV_PATH" fill="none" stroke="currentColor" stroke-width="10" stroke-linecap="round" stroke-linejoin="round" />
    </g>
  </svg>
</template>

<script setup>
import {
  F2_DEV_PATH,
  F2_SYMBOL_PATHS,
  F2_SYMBOL_VIEW_BOX,
  F2_WORDMARK_DEV_TRANSFORM,
  F2_WORDMARK_SYMBOL_TRANSFORM,
  F2_WORDMARK_VIEW_BOX,
} from '../brand/f2.js';

defineProps({
  decorative: Boolean,
  variant: {
    type: String,
    default: 'symbol',
    validator: (value) => ['symbol', 'wordmark'].includes(value),
  },
});
</script>

<style scoped>
.xdev-logo { display: block; width: 100%; height: 100%; }
.xdev-logo__symbol { color: var(--xdev-accent, #5655CD); }
.xdev-logo__dev { color: var(--xdev-text-primary, currentColor); }
</style>

<template>
  <svg class="xdev-logo" viewBox="0 0 28 28" :role="decorative ? undefined : 'img'" :aria-label="decorative ? undefined : 'XDev'" :aria-hidden="decorative ? 'true' : undefined">
    <rect x="0.5" y="0.5" width="27" height="27" rx="5" fill="var(--xdev-bg-app)" stroke="var(--xdev-border-subtle)" />
    <path d="M4 3h6l4 8.5L18 3h6l-8.5 11L24 25h-6l-4-8.5L10 25H4l8.5-11z" fill="currentColor" />
    <circle cx="14" cy="14" r="2" fill="var(--xdev-bg-app)" />
  </svg>
</template>

<script setup>
defineProps({ decorative: Boolean });
</script>

<style scoped>
.xdev-logo { display: block; width: 100%; height: 100%; color: var(--xdev-accent); }
</style>

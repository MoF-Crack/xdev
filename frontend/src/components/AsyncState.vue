<template>
  <div v-if="error" class="async-state async-state--error" role="alert">
    <slot name="error" :error="error"><p>{{ errorMessage }}</p></slot>
    <button v-if="retryLabel" type="button" class="xdev-btn xdev-btn--secondary" @click="$emit('retry')">{{ retryLabel }}</button>
  </div>
  <div v-else-if="loading" class="async-state async-state--loading" role="status" aria-live="polite"><slot name="loading">Loading...</slot></div>
  <slot v-else-if="empty" name="empty" />
  <slot v-else />
</template>

<script setup>
import { computed } from 'vue';
const props = defineProps({ loading: Boolean, empty: Boolean, error: { type: [String, Error], default: null }, retryLabel: { type: String, default: 'Retry' } });
defineEmits(['retry']);
const errorMessage = computed(() => props.error instanceof Error ? props.error.message : props.error);
</script>

<style scoped>
.async-state { padding: var(--xdev-space-6); color: var(--xdev-text-muted); text-align: center; }
.async-state--error { color: var(--xdev-error); }
.async-state p { margin: 0 0 var(--xdev-space-3); }
</style>

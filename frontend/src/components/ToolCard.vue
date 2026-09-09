<template>
  <article class="tool-card" data-testid="tool-card">
    <div><strong>{{ tool.displayName || tool.name }}</strong><span class="form-badges"><span v-for="f in tool.formFactor || []" :key="f" class="form-badge" :data-form="f">{{ t('form.' + f) }}</span></span><p>{{ tool.description }}</p></div>
    <StatusBadge :tone="tool.installed ? 'success' : 'neutral'">{{ status }}</StatusBadge>
    <div class="tool-card__actions">
      <button class="xdev-btn xdev-btn--secondary" type="button" data-action="detail" :data-tool="tool.name" @click="$emit('open-detail', tool)">{{ t('btn.detail') }}</button>
      <span v-if="!tool.installed && !tool.canInstall && tool.installHint" class="tool-card__hint">{{ tool.installHint }}</span>
      <span v-if="isLocal" class="tool-card__hint">{{ t('catalog.local_detected') }}</span>
      <button class="xdev-btn xdev-btn--secondary" type="button" data-action="favorite" :aria-label="favoriteLabel" :title="favoriteLabel" @click="$emit('toggle-favorite', tool)">{{ tool.isFavorite ? '★' : '☆' }}</button>
    </div>
  </article>
</template>

<script setup>
import { computed } from 'vue';
import StatusBadge from './StatusBadge.vue';
const props = defineProps({ tool: { type: Object, required: true }, t: { type: Function, required: true } });
defineEmits(['open-detail', 'toggle-favorite']);
const isLocal = computed(() => !props.tool.installed && Boolean(props.tool.localVersion || props.tool.localPath));
const favoriteLabel = computed(() => props.t(props.tool.isFavorite ? 'favorites.remove' : 'favorites.add'));
const status = computed(() => props.tool.installed ? props.t('status.installed') : props.tool.localVersion ? props.tool.localVersion : props.t('status.not_installed'));
</script>

<style scoped>
.tool-card { display: grid; grid-template-columns: 1fr auto; gap: var(--xdev-space-3); padding: var(--xdev-space-4); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-md); background: var(--xdev-bg-card); }
.tool-card p { margin: 4px 0 0; color: var(--xdev-text-muted); font-size: 12px; }
.form-badges { display: inline-flex; gap: 4px; align-items: center; flex-wrap: wrap; margin-left: 6px; }
.form-badge { padding: 1px 6px; border-radius: var(--xdev-radius-sm); font-size: 10px; font-weight: 600; letter-spacing: 0.04em; border: 1px solid var(--xdev-border-strong); color: var(--xdev-text-muted); }
.form-badge[data-form="gui"] { color: var(--xdev-accent); border-color: var(--xdev-accent); }
.tool-card__actions { grid-column: 1 / -1; display: flex; gap: var(--xdev-space-2); }
.tool-card__hint { align-self: center; color: var(--xdev-text-muted); font-size: 11px; }
button:disabled { cursor: not-allowed; opacity: .45; }
</style>

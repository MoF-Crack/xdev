<template>
  <article class="tool-card" data-testid="tool-card">
    <div><strong>{{ tool.displayName || tool.name }}</strong><p>{{ tool.description }}</p></div>
    <StatusBadge :tone="tool.installed ? 'success' : 'neutral'">{{ status }}</StatusBadge>
    <div class="tool-card__actions">
      <button type="button" data-action="detail" :data-tool="tool.name" @click="$emit('open-detail', tool)">{{ t('btn.detail') }}</button>
      <button v-if="category === 'package-manager' && tool.canInstall" type="button" data-action="package-manager" :disabled="operatingTool === (tool.toolName || tool.name)" @click="$emit(tool.installed ? 'update-package-manager' : 'install-package-manager', tool)">{{ t(tool.installed ? 'btn.update' : 'btn.install') }}</button>
      <button v-else-if="category !== 'package-manager' && canInstall" type="button" data-action="install" @click="$emit('install', tool)">{{ t('btn.install') }}</button>
      <span v-if="!tool.installed && !tool.canInstall && tool.installHint" class="tool-card__hint">{{ tool.installHint }}</span>
      <span v-if="isLocal" class="tool-card__hint">{{ t('catalog.local_detected') }}</span>
      <button type="button" data-action="favorite" :aria-label="favoriteLabel" :title="favoriteLabel" @click="$emit('toggle-favorite', tool)">{{ tool.isFavorite ? '★' : '☆' }}</button>
    </div>
  </article>
</template>

<script setup>
import { computed } from 'vue';
import StatusBadge from './StatusBadge.vue';
const props = defineProps({ operatingTool: { type: String, default: '' }, tool: { type: Object, required: true }, category: { type: String, default: '' }, t: { type: Function, required: true } });
defineEmits(['open-detail', 'install', 'install-package-manager', 'update-package-manager', 'toggle-favorite']);
const canInstall = computed(() => !props.tool.installed && props.tool.canInstall === true);
const isLocal = computed(() => !props.tool.installed && Boolean(props.tool.localVersion || props.tool.localPath));
const favoriteLabel = computed(() => props.t(props.tool.isFavorite ? 'favorites.remove' : 'favorites.add'));
const status = computed(() => props.tool.installed ? props.t('status.installed') : props.tool.localVersion ? props.tool.localVersion : props.t('status.not_installed'));
</script>

<style scoped>
.tool-card { display: grid; grid-template-columns: 1fr auto; gap: var(--xdev-space-3); padding: var(--xdev-space-4); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-md); background: var(--xdev-bg-panel); }
.tool-card p { margin: 4px 0 0; color: var(--xdev-text-muted); font-size: 12px; }
.tool-card__actions { grid-column: 1 / -1; display: flex; gap: var(--xdev-space-2); }
.tool-card__hint { align-self: center; color: var(--xdev-text-muted); font-size: 11px; }
button { padding: 6px 10px; border: 1px solid var(--xdev-border-strong); border-radius: var(--xdev-radius-sm); background: transparent; color: var(--xdev-text-primary); cursor: pointer; }
button:disabled { cursor: not-allowed; opacity: .45; }
</style>

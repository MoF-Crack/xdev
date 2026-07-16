<template>
  <table class="tool-table" data-testid="tool-table">
    <thead><tr><th>{{ t('table.tool') }}</th><th>{{ t('table.version') }}</th><th>{{ t('table.local_path') }}</th><th>{{ t('table.action') }}</th></tr></thead>
    <tbody><tr v-for="tool in tools" :key="tool.name">
      <td><button class="tool-table__identity" type="button" data-action="detail" :data-tool="tool.name" @click="$emit('open-detail', tool)"><strong>{{ tool.displayName || tool.name }}</strong><small>{{ tool.description }}</small></button></td>
      <td>{{ tool.activeVersion || tool.localVersion || '-' }} <span v-if="isLocal(tool)" class="tool-table__note">{{ t('catalog.local_detected') }}</span></td><td class="tool-table__path">{{ tool.localPath || '-' }}</td>
      <td><div class="tool-table__actions">
        <button v-if="category === 'package-manager' && tool.canInstall" type="button" data-action="package-manager" :disabled="operatingTool === (tool.toolName || tool.name)" @click="$emit(tool.installed ? 'update-package-manager' : 'install-package-manager', tool)">{{ t(tool.installed ? 'btn.update' : 'btn.install') }}</button>
        <button v-else-if="category !== 'package-manager' && canInstall(tool)" type="button" data-action="install" @click="$emit('install', tool)">{{ t('btn.install') }}</button>
        <span v-if="!tool.installed && !tool.canInstall && tool.installHint" class="tool-table__hint">{{ tool.installHint }}</span>
        <button type="button" data-action="favorite" :aria-label="favoriteLabel(tool)" :title="favoriteLabel(tool)" @click="$emit('toggle-favorite', tool)">{{ tool.isFavorite ? '★' : '☆' }}</button>
      </div></td>
    </tr></tbody>
  </table>
</template>

<script setup>
const props = defineProps({ operatingTool: { type: String, default: '' }, tools: { type: Array, required: true }, category: { type: String, default: '' }, t: { type: Function, required: true } });
defineEmits(['open-detail', 'install', 'install-package-manager', 'update-package-manager', 'toggle-favorite']);
function canInstall(tool) { return !tool.installed && tool.canInstall === true; }
function isLocal(tool) { return !tool.installed && Boolean(tool.localVersion || tool.localPath); }
function favoriteLabel(tool) { return props.t(tool.isFavorite ? 'favorites.remove' : 'favorites.add'); }
</script>

<style scoped>
.tool-table { width: 100%; border-collapse: collapse; font-size: 12px; }
th { padding: 8px 10px; color: var(--xdev-text-muted); text-align: left; font-weight: 500; border-bottom: 1px solid var(--xdev-border-strong); }
td { padding: 8px 10px; border-bottom: 1px solid var(--xdev-border-subtle); }
.tool-table__identity { display: grid; padding: 0; border: 0; background: transparent; color: var(--xdev-text-primary); text-align: left; cursor: pointer; }
.tool-table__identity small, .tool-table__path { max-width: 280px; overflow: hidden; color: var(--xdev-text-muted); text-overflow: ellipsis; white-space: nowrap; }
td > button:not(.tool-table__identity) { padding: 5px 9px; border: 1px solid var(--xdev-border-strong); border-radius: var(--xdev-radius-sm); background: transparent; color: var(--xdev-accent); cursor: pointer; }
.tool-table__actions { display: flex; align-items: center; gap: var(--xdev-space-2); }
.tool-table__actions button { padding: 5px 9px; border: 1px solid var(--xdev-border-strong); border-radius: var(--xdev-radius-sm); background: transparent; color: var(--xdev-accent); cursor: pointer; }
.tool-table__hint, .tool-table__note { color: var(--xdev-text-muted); font-size: 11px; }
button:disabled { opacity: .4; cursor: not-allowed; }
</style>

<template>
  <table class="tool-table" data-testid="tool-table">
    <thead><tr><th>{{ t('table.tool') }}</th><th>{{ t('table.version') }}</th><th>{{ t('table.local_path') }}</th><th>{{ t('table.action') }}</th></tr></thead>
    <tbody><tr v-for="tool in tools" :key="tool.name">
      <td><button class="xdev-btn xdev-btn--secondary tool-table__identity" type="button" data-action="detail" :data-tool="tool.name" @click="$emit('open-detail', tool)"><strong>{{ tool.displayName || tool.name }}</strong><span class="form-badges"><span v-for="f in tool.formFactor || []" :key="f" class="form-badge" :data-form="f">{{ t('form.' + f) }}</span></span><small>{{ tool.description }}</small></button></td>
      <td class="tool-table__version">{{ tool.activeVersion || tool.localVersion || '-' }} <span v-if="isLocal(tool)" class="tool-table__note">{{ t('catalog.local_detected') }}</span></td><td class="tool-table__path">{{ tool.localPath || '-' }}</td>
      <td><div class="tool-table__actions">
        <span v-if="!tool.installed && !tool.canInstall && tool.installHint" class="tool-table__hint">{{ tool.installHint }}</span>
        <button class="xdev-btn xdev-btn--secondary" type="button" data-action="favorite" :aria-label="favoriteLabel(tool)" :title="favoriteLabel(tool)" @click="$emit('toggle-favorite', tool)">{{ tool.isFavorite ? '★' : '☆' }}</button>
      </div></td>
    </tr></tbody>
  </table>
</template>

<script setup>
const props = defineProps({ tools: { type: Array, required: true }, t: { type: Function, required: true } });
defineEmits(['open-detail', 'toggle-favorite']);
function isLocal(tool) { return !tool.installed && Boolean(tool.localVersion || tool.localPath); }
function favoriteLabel(tool) { return props.t(tool.isFavorite ? 'favorites.remove' : 'favorites.add'); }
</script>

<style scoped>
.tool-table { width: 100%; border-collapse: collapse; font-size: var(--xdev-font-size-sm); }
th { padding: 8px 10px; color: var(--xdev-text-muted); font-size: 11px; font-weight: 600; letter-spacing: 0.08em; text-transform: uppercase; text-align: left; border-bottom: 1px solid var(--xdev-border-strong); }
td { padding: 10px; border-bottom: 1px solid var(--xdev-border-subtle); }
.tool-table tbody tr:hover { background: var(--xdev-bg-hover); }
.tool-table__identity { display: grid; padding: 0; border: 0; background: transparent; color: var(--xdev-text-primary); text-align: left; cursor: pointer; font: inherit; }
.tool-table__identity strong { font-weight: 600; }
.tool-table__identity:hover strong { color: var(--xdev-accent); }
.tool-table__version { font-family: var(--xdev-font-mono); font-size: 12px; color: var(--xdev-text-secondary); white-space: nowrap; }
.tool-table__identity small, .tool-table__path { max-width: 280px; overflow: hidden; color: var(--xdev-text-muted); text-overflow: ellipsis; white-space: nowrap; font-family: var(--xdev-font-mono); font-size: 11px; }
.tool-table__actions { display: flex; align-items: center; gap: var(--xdev-space-2); }
.tool-table__hint, .tool-table__note { color: var(--xdev-text-muted); font-size: 11px; font-family: var(--xdev-font-ui); }
.form-badges { display: inline-flex; gap: 4px; align-items: center; flex-wrap: wrap; }
.form-badge { padding: 1px 6px; border-radius: var(--xdev-radius-sm); font-size: 10px; font-weight: 600; letter-spacing: 0.04em; border: 1px solid var(--xdev-border-strong); color: var(--xdev-text-muted); justify-self: start; width: max-content; }
.form-badge[data-form="gui"] { color: var(--xdev-accent); border-color: var(--xdev-accent); }
button:disabled { opacity: .4; cursor: not-allowed; }
</style>

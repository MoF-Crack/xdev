<template>
  <table class="tool-table" data-testid="tool-table">
    <colgroup><col class="tool-table__tool-column"><col class="tool-table__version-column"><col class="tool-table__path-column"><col class="tool-table__action-column"></colgroup>
    <thead><tr><th scope="col">{{ t('table.tool') }}</th><th scope="col">{{ t('table.version') }}</th><th scope="col">{{ t('table.local_path') }}</th><th scope="col">{{ t('table.action') }}</th></tr></thead>
    <tbody>
      <tr v-for="tool in tools" :key="tool.name" :class="{ 'tool-table__row--selected': tool.name === selectedName }" :data-tool="tool.name">
        <td class="tool-table__tool">
          <button class="tool-table__identity" type="button" data-action="detail" :data-tool="tool.name" :aria-current="tool.name === selectedName ? 'true' : undefined" @click="$emit('open-detail', tool)">
            <span class="tool-table__monogram" aria-hidden="true">{{ monogram(tool) }}</span>
            <span class="tool-table__copy">
              <span class="tool-table__name"><strong>{{ tool.displayName || tool.name }}</strong><span v-if="tool.formFactor?.length" class="form-badges"><span v-for="f in tool.formFactor" :key="f" class="form-badge" :data-form="f">{{ t('form.' + f) }}</span></span></span>
              <small v-if="tool.description" class="tool-table__description" :title="tool.description">{{ tool.description }}</small>
            </span>
          </button>
        </td>
        <td class="tool-table__version">
          <span class="tool-table__version-value">{{ tool.activeVersion || tool.localVersion || '-' }}</span>
          <span class="tool-table__status" :data-status="statusKind(tool)">
            <svg v-if="statusKind(tool) === 'active'" viewBox="0 0 16 16" fill="none" aria-hidden="true"><circle cx="8" cy="8" r="5.5" /><path d="m5.5 8 1.6 1.6 3.4-3.4" /></svg>
            <svg v-else-if="statusKind(tool) === 'installed' || statusKind(tool) === 'local'" viewBox="0 0 16 16" fill="none" aria-hidden="true"><rect x="2.5" y="3" width="11" height="8" rx="1" /><path d="M5.5 13h5M8 11v2" /></svg>
            <svg v-else viewBox="0 0 16 16" fill="none" aria-hidden="true"><path d="M8 2.5v7m-2.5-2L8 10l2.5-2.5M3 10.5v3h10v-3" /></svg>
            {{ statusLabel(tool) }}
          </span>
        </td>
        <td class="tool-table__path">
          <code :title="tool.localPath || undefined">{{ tool.localPath || '-' }}</code>
          <span v-if="tool.localVersion && tool.installed" class="tool-table__local-note">{{ t('catalog.local_detected') }} {{ tool.localVersion }}</span>
        </td>
        <td class="tool-table__action">
          <div class="tool-table__actions">
            <span v-if="!tool.installed && !tool.canInstall && tool.installHint" class="tool-table__hint">{{ tool.installHint }}</span>
            <button class="tool-table__favorite" :class="{ 'tool-table__favorite--active': tool.isFavorite }" type="button" data-action="favorite" :data-tool="tool.name" :aria-label="favoriteLabel(tool)" :title="favoriteLabel(tool)" :aria-pressed="Boolean(tool.isFavorite)" @click="$emit('toggle-favorite', tool)">
              <svg viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="m10 2.5 2.3 4.66 5.15.75-3.72 3.63.88 5.12L10 14.24l-4.61 2.42.88-5.12L2.55 7.9l5.15-.75L10 2.5Z" /></svg>
            </button>
          </div>
        </td>
      </tr>
    </tbody>
  </table>
</template>

<script setup>
const props = defineProps({ tools: { type: Array, required: true }, selectedName: { type: String, default: '' }, t: { type: Function, required: true } });
defineEmits(['open-detail', 'toggle-favorite']);
const monograms = { node: 'JS', nodejs: 'JS', python: 'Py', go: 'Go', rust: 'Rs', java: 'J', dotnet: '.N', deno: 'De' };
function monogram(tool) { return monograms[tool.name] || Array.from(tool.displayName || tool.name || '').slice(0, 2).join(''); }
function statusKind(tool) {
  if (tool.installed && tool.activeVersion) return 'active';
  if (tool.installed) return 'installed';
  if (tool.localVersion || tool.localPath) return 'local';
  return 'missing';
}
function statusLabel(tool) {
  const keys = { active: 'status.active', installed: 'status.installed', local: 'catalog.local_detected', missing: 'status.not_installed' };
  return props.t(keys[statusKind(tool)]);
}
function favoriteLabel(tool) { return props.t(tool.isFavorite ? 'favorites.remove' : 'favorites.add'); }
</script>

<style scoped>
.tool-table { width: 100%; table-layout: fixed; border-collapse: collapse; font-size: var(--xdev-font-size-sm); }
.tool-table__tool-column { width: 36%; }
.tool-table__version-column { width: 22%; }
.tool-table__path-column { width: 31%; }
.tool-table__action-column { width: 11%; }
.tool-table th { height: 44px; padding: 10px 18px; border-top: 1px solid var(--xdev-border-subtle); border-bottom: 1px solid var(--xdev-border-subtle); background: var(--xdev-bg-muted); color: var(--xdev-text-secondary); font-size: 11px; font-weight: 500; text-align: left; }
.tool-table th:last-child { text-align: right; }
.tool-table td { height: 77px; padding: 14px 18px; border-bottom: 1px solid var(--xdev-border-subtle); vertical-align: middle; }
.tool-table tbody tr:hover { background: var(--xdev-bg-hover); }
.tool-table tbody .tool-table__row--selected,
.tool-table tbody .tool-table__row--selected:hover { background: var(--xdev-accent-soft); }
.tool-table__row--selected .tool-table__tool { box-shadow: inset 3px 0 0 var(--xdev-accent); }
.tool-table__identity { display: flex; align-items: center; gap: 12px; width: 100%; min-width: 0; margin: 0; padding: 0; border: 0; border-radius: 6px; background: transparent; color: var(--xdev-text-primary); text-align: left; cursor: pointer; font: inherit; }
.tool-table__identity:focus-visible,
.tool-table__favorite:focus-visible { outline: 2px solid var(--xdev-accent); outline-offset: 3px; box-shadow: none; }
.tool-table__monogram { display: grid; flex: 0 0 36px; place-items: center; width: 36px; height: 36px; border: 1px solid var(--xdev-border-strong); border-radius: 9px; background: var(--xdev-bg-elevated); color: var(--xdev-text-secondary); font-family: var(--xdev-font-mono); font-size: 12px; font-weight: 500; line-height: 1; }
.tool-table__copy { display: grid; min-width: 0; gap: 6px; }
.tool-table__name { display: flex; min-width: 0; align-items: center; flex-wrap: wrap; gap: 7px; }
.tool-table__name strong { overflow: hidden; font-size: 14px; font-weight: 600; line-height: 1.25; text-overflow: ellipsis; white-space: nowrap; }
.tool-table__identity:hover strong { color: var(--xdev-accent); }
.tool-table__description { display: block; overflow: hidden; color: var(--xdev-text-secondary); font-size: 12px; font-weight: 400; line-height: 1.35; text-overflow: ellipsis; white-space: nowrap; }
.form-badges { display: inline-flex; align-items: center; flex-wrap: wrap; gap: 4px; }
.form-badge { padding: 1px 4px; border: 1px solid var(--xdev-border-subtle); border-radius: 3px; background: var(--xdev-bg-elevated); color: var(--xdev-text-secondary); font-family: var(--xdev-font-mono); font-size: 9px; font-weight: 400; line-height: 1.2; }
.tool-table__version-value { display: block; overflow: hidden; color: var(--xdev-text-primary); font-family: var(--xdev-font-mono); font-size: 13px; line-height: 1.4; text-overflow: ellipsis; white-space: nowrap; }
.tool-table__status { display: flex; align-items: center; gap: 5px; margin-top: 5px; color: var(--xdev-text-muted); font-size: 10px; line-height: 1.4; }
.tool-table__status svg { flex-shrink: 0; width: 12px; height: 12px; stroke: currentColor; stroke-width: 1.2; stroke-linecap: round; stroke-linejoin: round; }
.tool-table__status[data-status="active"],
.tool-table__status[data-status="installed"] { color: var(--xdev-success); }
.tool-table__status[data-status="local"] { color: var(--xdev-warning); }
.tool-table__path code { display: block; overflow: hidden; color: var(--xdev-text-secondary); font-family: var(--xdev-font-mono); font-size: 10px; line-height: 1.5; text-overflow: ellipsis; white-space: nowrap; }
.tool-table__local-note { display: block; margin-top: 5px; color: var(--xdev-text-muted); font-size: 10px; line-height: 1.4; }
.tool-table__actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; }
.tool-table__hint { color: var(--xdev-text-muted); font-size: 10px; line-height: 1.4; overflow-wrap: anywhere; }
.tool-table__favorite { display: grid; flex: 0 0 32px; place-items: center; width: 32px; height: 32px; padding: 7px; border: 0; border-radius: 6px; background: transparent; color: var(--xdev-text-muted); cursor: pointer; }
.tool-table__favorite svg { width: 17px; height: 17px; stroke: currentColor; stroke-width: 1.3; stroke-linejoin: round; }
.tool-table__favorite:hover { background: var(--xdev-bg-elevated); color: var(--xdev-accent); }
.tool-table__favorite--active { color: var(--xdev-info); }
.tool-table__favorite--active svg { fill: var(--xdev-info-soft); }
@media (max-width: 1000px) {
  .tool-table th, .tool-table td { padding-right: 12px; padding-left: 12px; }
  .tool-table__tool-column { width: 39%; }
  .tool-table__path-column { width: 28%; }
  .tool-table__identity { gap: 10px; }
}
</style>

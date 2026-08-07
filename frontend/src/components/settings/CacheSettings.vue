<template>
  <FormSection id="maintenance" :title="t('settings.cache_management')">
    <label class="retention-field" for="archive-retention">
      <span>{{ t('settings.archive_retention') }}</span>
      <select id="archive-retention" data-action="archive-retention" :value="retention" @change="$emit('update:retention', $event.target.value)">
        <option value="immediate">{{ t('settings.retention_immediate') }}</option>
        <option value="7d">{{ t('settings.retention_7d') }}</option>
        <option value="30d">{{ t('settings.retention_30d') }}</option>
        <option value="forever">{{ t('settings.retention_forever') }}</option>
      </select>
    </label>
    <dl><template v-for="(v,k) in breakdown" :key="k"><dt><span>{{ t(labelKey(k)) }}</span><span v-if="tipKey(k)" class="cache-info" role="img" tabindex="0" :aria-label="t(tipKey(k))" :title="t(tipKey(k))" :data-tooltip="t(tipKey(k))">i</span></dt><dd>{{k==='totalCount'?v:formatBytes(v||0)}}</dd></template></dl>
    <template #actions><ConfirmAction title="Clean archives" :confirm-label="t('cache.clean_archives')" :cancel-label="t('btn.cancel')" @confirm="$emit('clean','archives')"><template #trigger><button class="xdev-btn xdev-btn--secondary" type="button">{{t('cache.clean_archives')}}</button></template></ConfirmAction><ConfirmAction title="Clean all cache" :description="t('cache.clean_warning')" :confirm-label="t('cache.clean_all')" :cancel-label="t('btn.cancel')" @confirm="$emit('clean','all')"><template #trigger><button class="xdev-btn xdev-btn--secondary" data-action="cache-clean-all" type="button">{{t('cache.clean_all')}}</button></template></ConfirmAction></template>
  </FormSection>
</template>

<script setup>
import FormSection from '../FormSection.vue';
import ConfirmAction from '../ConfirmAction.vue';
import { cacheCategoryLabelKey } from '../../utils/cacheCategories.js';
import { formatBytes } from '../../utils/format.js';

defineProps({ breakdown: { type: Object, default: () => ({}) }, retention: { type: String, default: 'immediate' }, t: { type: Function, required: true } });
defineEmits(['clean', 'update:retention']);
const tipKeys = { archives: 'cache.archives_tip', shaFiles: 'cache.sha_files_tip', partFiles: 'cache.part_files_tip', scanCache: 'cache.scan_cache_tip', otherFiles: 'cache.other_tip' };
const labelKey = key => key === 'totalCount' ? 'cache.total_count' : cacheCategoryLabelKey[key] || key;
const tipKey = key => tipKeys[key] || '';
</script>

<style scoped>
.retention-field { display: grid; grid-template-columns: 140px minmax(0, 1fr); align-items: center; gap: var(--xdev-space-3); max-width: 520px; color: var(--xdev-text-secondary); font-size: var(--xdev-font-size-sm); }
.retention-field select { width: 100%; min-width: 0; min-height: 34px; padding: 0 var(--xdev-space-3); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-sm); background: var(--xdev-bg-panel); color: var(--xdev-text-primary); font: inherit; font-size: var(--xdev-font-size-md); }
.retention-field select:focus { border-color: var(--xdev-accent); outline: none; }
dl { display: grid; grid-template-columns: minmax(0,1fr) minmax(0,auto); gap: var(--xdev-space-2) var(--xdev-space-5); width: 100%; max-width: 520px; min-width: 0; margin: 0; }
dt, dd { min-width: 0; margin: 0; padding: var(--xdev-space-2) 0; border-bottom: 1px solid var(--xdev-border-subtle); }
dt { display: inline-flex; align-items: center; gap: var(--xdev-space-1); min-width: 0; color: var(--xdev-text-muted); font-size: var(--xdev-font-size-sm); }
dd { min-width: 0; overflow-wrap: anywhere; color: var(--xdev-text-primary); font-variant-numeric: tabular-nums; text-align: right; }
.cache-info { position: relative; display: inline-flex; align-items: center; justify-content: center; width: 15px; height: 15px; flex: 0 0 15px; border: 1px solid var(--xdev-border-strong); border-radius: 50%; color: var(--xdev-text-muted); font-size: 11px; font-weight: 700; line-height: 1; cursor: help; }
.cache-info::after { position: absolute; left: 0; top: calc(100% + var(--xdev-space-2)); z-index: 3; width: max-content; max-width: 260px; padding: var(--xdev-space-2) var(--xdev-space-3); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-sm); background: var(--xdev-bg-elevated); box-shadow: var(--xdev-shadow-md); color: var(--xdev-text-primary); content: attr(data-tooltip); font-size: var(--xdev-font-size-xs); font-weight: 400; line-height: 1.4; opacity: 0; pointer-events: none; transform: translateY(-2px); transition: opacity var(--xdev-motion-fast), transform var(--xdev-motion-fast); white-space: normal; }
.cache-info:hover, .cache-info:focus-visible { border-color: var(--xdev-accent); color: var(--xdev-accent); outline: none; }
.cache-info:hover::after, .cache-info:focus-visible::after { opacity: 1; transform: translateY(0); }
@media (max-width: 560px) { .retention-field { grid-template-columns: 1fr; gap: var(--xdev-space-1); align-items: stretch; } }
</style>

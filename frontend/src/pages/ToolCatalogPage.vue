<template>
  <section class="tool-catalog">
    <header class="tool-catalog__heading"><h1>{{ t('category.' + category) }}</h1><p>{{ t('catalog.description') }}</p></header>
    <div class="tool-catalog__panel"><div class="tool-catalog__filters">
      <input data-testid="tool-search" type="search" :value="searchQuery" :placeholder="t('search.placeholder')" :aria-label="t('search.label')" @input="$emit('update:searchQuery', $event.target.value)">
      <label><input data-testid="hide-unavailable" type="checkbox" :checked="hideUnavailable" @change="$emit('update:hideUnavailable', $event.target.checked)"> {{ t('filter.hide_unavailable') }}</label>
    </div>
    <div class="tool-catalog__tabs" role="group" :aria-label="t('catalog.filters')"><button v-for="tab in tabs" :key="tab.key" type="button" :aria-pressed="statusFilter === tab.key" @click="statusFilter = tab.key">{{ t(tab.label) }} <span>{{ tab.count }}</span></button></div>
    <AsyncState :loading="loading" :error="error" :empty="!displayedTools.length" :retry-label="t('btn.retry')" @retry="$emit('retry')">
      <template #loading>{{ t('catalog.loading') }}</template>
      <template #empty><EmptyState :title="t('no_tools_found')" /></template>
      <div class="tool-catalog__table">
        <ResponsiveDataTable :columns="contractColumns" :data="displayedTools" row-key="name" :breakpoint="760">
          <template #default><ToolTable :tools="displayedTools" :selected-name="selectedName" :t="t" @open-detail="$emit('open-detail', $event)" @toggle-favorite="$emit('toggle-favorite', $event)" /></template>
          <template #card="{ row }"><ToolCard :tool="row" :t="t" @open-detail="$emit('open-detail', $event)" @toggle-favorite="$emit('toggle-favorite', $event)" /></template>
        </ResponsiveDataTable>
      </div>
    </AsyncState><footer class="tool-catalog__footer">{{ t('catalog.tool_count', { n: displayedTools.length }) }}</footer></div>
  </section>
</template>

<script setup>
import { computed, ref, watch } from 'vue';
import AsyncState from '../components/AsyncState.vue';
import EmptyState from '../components/EmptyState.vue';
import ResponsiveDataTable from '../components/ResponsiveDataTable.vue';
import ToolCard from '../components/ToolCard.vue';
import ToolTable from '../components/ToolTable.vue';
const props = defineProps({ selectedName: { type: String, default: '' }, tools: { type: Array, default: () => [] }, category: { type: String, default: '' }, loading: Boolean, error: { type: [String, Error], default: null }, searchQuery: { type: String, default: '' }, hideUnavailable: { type: Boolean, default: true }, t: { type: Function, required: true } });
defineEmits(['update:searchQuery', 'update:hideUnavailable', 'open-detail', 'toggle-favorite', 'retry']);
const statusFilter = ref('all');
const isLocal = (tool) => Boolean(tool.localVersion || tool.localPath);
const tabs = computed(() => [{ key: 'all', label: 'catalog.all', count: props.tools.length }, { key: 'installed', label: 'status.installed', count: props.tools.filter(tool => tool.installed).length }, { key: 'local', label: 'catalog.local_filter', count: props.tools.filter(isLocal).length }]);
const displayedTools = computed(() => props.tools.filter(tool => statusFilter.value === 'all' || (statusFilter.value === 'installed' ? tool.installed : isLocal(tool))));
watch(() => props.category, () => { statusFilter.value = 'all'; });
const contractColumns = [{ key: 'identity', contract: 'identity' }, { key: 'status', contract: 'status' }, { key: 'actions', contract: 'actions' }];
</script>

<style scoped>
.tool-catalog { display: grid; width: min(var(--xdev-table-max-width), 100%); min-width: 0; margin: 0 auto; gap: 24px; }
.tool-catalog__heading h1 { margin: 0; color: var(--xdev-text-primary); font-size: 26px; font-weight: 650; letter-spacing: -.035em; line-height: 1.3; }
.tool-catalog__heading p { margin: 8px 0 0; color: var(--xdev-text-secondary); font-size: 13px; line-height: 1.6; }
.tool-catalog__panel { display: flex; min-width: 0; min-height: min(680px, calc(100vh - 240px)); flex-direction: column; overflow: hidden; border: 1px solid var(--xdev-border-subtle); border-radius: 12px; background: var(--xdev-bg-card); box-shadow: var(--xdev-shadow-sm); }
.tool-catalog__filters { position: relative; display: flex; align-items: center; gap: 16px; padding: 16px 18px 0; }
.tool-catalog__filters::before { position: absolute; top: 29px; left: 32px; width: 11px; height: 11px; border: 1px solid var(--xdev-text-muted); border-radius: 50%; content: ''; pointer-events: none; }
.tool-catalog__filters::after { position: absolute; top: 39px; left: 42px; width: 5px; height: 1px; transform: rotate(45deg); transform-origin: left center; background: var(--xdev-text-muted); content: ''; pointer-events: none; }
.tool-catalog__filters > input { flex: 1; width: 100%; min-width: 0; height: 39px; padding: 9px 12px 9px 36px; border: 1px solid var(--xdev-border-subtle); border-radius: 7px; background: var(--xdev-bg-elevated); color: var(--xdev-text-primary); font: inherit; font-size: 12px; }
.tool-catalog__filters > input::placeholder { color: var(--xdev-text-muted); }
.tool-catalog__filters > input:focus-visible { outline: 2px solid var(--xdev-accent); outline-offset: 2px; border-color: var(--xdev-accent); box-shadow: none; }
.tool-catalog__filters label { display: inline-flex; align-items: center; gap: 6px; color: var(--xdev-text-secondary); font-size: 11px; white-space: nowrap; cursor: pointer; }
.tool-catalog__filters label input { width: 13px; height: 13px; margin: 0; accent-color: var(--xdev-accent); cursor: pointer; }
.tool-catalog__tabs { display: flex; align-items: stretch; gap: 18px; margin: 10px 18px 0; }
.tool-catalog__tabs button { display: inline-flex; align-items: center; justify-content: center; gap: 6px; min-height: 47px; padding: 9px 0 12px; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--xdev-text-secondary); cursor: pointer; font: inherit; font-size: 12px; white-space: nowrap; }
.tool-catalog__tabs button:hover { color: var(--xdev-accent); }
.tool-catalog__tabs button[aria-pressed="true"] { border-bottom-color: var(--xdev-accent); color: var(--xdev-accent); font-weight: 600; }
.tool-catalog__tabs button:focus-visible { outline: 2px solid var(--xdev-accent); outline-offset: -2px; border-radius: 4px; box-shadow: none; }
.tool-catalog__tabs button span { display: inline-grid; min-width: 17px; min-height: 20px; place-items: center; padding: 2px 4px; border-radius: 3px; background: var(--xdev-bg-muted); font-family: var(--xdev-font-mono); font-size: 9px; font-weight: 400; line-height: 1.3; }
.tool-catalog__tabs button[aria-pressed="true"] span { background: var(--xdev-accent-soft); }
.tool-catalog__table { min-width: 0; }
.tool-catalog__footer { margin-top: auto; padding: 28px 18px 14px; color: var(--xdev-text-muted); font-size: 11px; line-height: 1.5; }
@media (max-width: 759px) {
  .tool-catalog { gap: 20px; }
  .tool-catalog__heading h1 { font-size: 23px; }
  .tool-catalog__panel { min-height: 0; }
  .tool-catalog__table { padding: 0 16px; }
  .tool-catalog__footer { padding-top: 20px; }
}
@media (max-width: 560px) {
  .tool-catalog__filters { align-items: stretch; flex-direction: column; gap: 12px; }
  .tool-catalog__filters label { min-height: 24px; align-self: flex-start; }
  .tool-catalog__tabs { gap: 16px; margin-top: 2px; }
}
</style>

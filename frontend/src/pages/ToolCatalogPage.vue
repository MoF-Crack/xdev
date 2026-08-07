<template>
  <section class="tool-catalog">
    <div class="tool-catalog__filters">
      <input data-testid="tool-search" type="search" :value="searchQuery" :placeholder="t('search.placeholder')" :aria-label="t('search.label')" @input="$emit('update:searchQuery', $event.target.value)">
      <label><input data-testid="hide-unavailable" type="checkbox" :checked="hideUnavailable" @change="$emit('update:hideUnavailable', $event.target.checked)"> {{ t('filter.hide_unavailable') }}</label>
    </div>
    <AsyncState :loading="loading" :error="error" :empty="!tools.length" :retry-label="t('btn.retry')" @retry="$emit('retry')">
      <template #loading>{{ t('catalog.loading') }}</template>
      <template #empty><EmptyState :title="t('no_tools_found')" /></template>
      <ResponsiveDataTable :columns="contractColumns" :data="tools" row-key="name" :breakpoint="760">
        <template #default><ToolTable :tools="tools" :category="category" :operating-tool="operatingTool" :t="t" @open-detail="$emit('open-detail', $event)" @install="$emit('install', $event)" @install-package-manager="$emit('install-package-manager', $event)" @update-package-manager="$emit('update-package-manager', $event)" @toggle-favorite="$emit('toggle-favorite', $event)" /></template>
        <template #card="{ row }"><ToolCard :tool="row" :category="category" :operating-tool="operatingTool" :t="t" @open-detail="$emit('open-detail', $event)" @install="$emit('install', $event)" @install-package-manager="$emit('install-package-manager', $event)" @update-package-manager="$emit('update-package-manager', $event)" @toggle-favorite="$emit('toggle-favorite', $event)" /></template>
      </ResponsiveDataTable>
    </AsyncState>
  </section>
</template>

<script setup>
import AsyncState from '../components/AsyncState.vue';
import EmptyState from '../components/EmptyState.vue';
import ResponsiveDataTable from '../components/ResponsiveDataTable.vue';
import ToolCard from '../components/ToolCard.vue';
import ToolTable from '../components/ToolTable.vue';
defineProps({ operatingTool: { type: String, default: '' }, tools: { type: Array, default: () => [] }, category: { type: String, default: '' }, loading: Boolean, error: { type: [String, Error], default: null }, searchQuery: { type: String, default: '' }, hideUnavailable: { type: Boolean, default: true }, t: { type: Function, required: true } });
defineEmits(['update:searchQuery', 'update:hideUnavailable', 'open-detail', 'install', 'install-package-manager', 'update-package-manager', 'toggle-favorite', 'retry']);
const contractColumns = [{ key: 'identity', contract: 'identity' }, { key: 'status', contract: 'status' }, { key: 'actions', contract: 'actions' }];
</script>

<style scoped>
.tool-catalog { width: min(var(--xdev-table-max-width), 100%); margin: 0 auto; display: grid; gap: var(--xdev-section-gap); }
.tool-catalog__filters { display: flex; align-items: center; justify-content: space-between; gap: var(--xdev-space-3); padding: var(--xdev-space-3) var(--xdev-space-4); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-lg); background: color-mix(in srgb, var(--xdev-bg-panel) 72%, transparent); }
.tool-catalog__filters > input { width: min(360px, 100%); padding: 8px 10px; border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-md); background: var(--xdev-bg-panel); color: var(--xdev-text-primary); }
.tool-catalog__filters label { color: var(--xdev-text-muted); font-size: 12px; white-space: nowrap; }
@media (max-width: 560px) { .tool-catalog__filters { align-items: stretch; flex-direction: column; } }
</style>

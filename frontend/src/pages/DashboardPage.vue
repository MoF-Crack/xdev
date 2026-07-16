<template>
  <AsyncState :loading="loading" :error="error" :retry-label="t('btn.retry')" @retry="$emit('retry')">
    <template #loading>{{ t('dashboard.loading') }}</template>
    <section class="dashboard-page">
      <div class="dashboard-page__metrics">
        <MetricCard :label="t('dashboard.total_tools')" :value="stats.totalTools || 0" /><MetricCard :label="t('dashboard.installed')" :value="stats.installedCount || 0" />
        <MetricCard :label="t('dashboard.install_size')" :value="formatBytes(stats.installSize || 0)" /><MetricCard :label="t('dashboard.cache_size')" :value="formatBytes(stats.cacheSize || 0)" />
      </div>
      <div class="dashboard-page__split">
        <SectionCard :title="t('dashboard.paths')"><dl><template v-for="path in paths" :key="path.label"><dt>{{ path.label }}</dt><dd><CopyableValue :value="path.value || '-'" :copy-label="t('btn.copy')" /></dd></template></dl></SectionCard>
        <SectionCard :title="t('dashboard.quick_actions')"><div class="dashboard-page__actions"><button data-action="scan-local" type="button" @click="$emit('scan-local')">{{ t('dashboard.scan_local') }}</button><button data-action="check-update" type="button" @click="$emit('check-update')">{{ t('dashboard.check_update') }}</button></div></SectionCard>
      </div>
      <SectionCard :title="t('dashboard.installed_tools')"><table v-if="stats.installedTools?.length"><thead><tr><th>{{ t('table.tool') }}</th><th>{{ t('table.version') }}</th><th>{{ t('detail.category') }}</th></tr></thead><tbody><tr v-for="tool in stats.installedTools" :key="`${tool.displayName}-${tool.version}`"><th scope="row">{{ tool.displayName }}</th><td>{{ tool.version }}</td><td>{{ tool.category }}</td></tr></tbody></table><EmptyState v-else :title="t('env.no_installed_tools')" /></SectionCard>
    </section>
  </AsyncState>
</template>

<script setup>
import { computed } from 'vue';
import AsyncState from '../components/AsyncState.vue'; import CopyableValue from '../components/CopyableValue.vue'; import EmptyState from '../components/EmptyState.vue'; import MetricCard from '../components/MetricCard.vue'; import SectionCard from '../components/SectionCard.vue';
const props = defineProps({ stats: { type: Object, default: () => ({}) }, loading: Boolean, error: { type: [String, Error], default: null }, t: { type: Function, required: true } });
defineEmits(['scan-local', 'check-update', 'retry']);
const paths = computed(() => [{ label: props.t('dashboard.paths_home'), value: props.stats.xmdevDir }, { label: props.t('dashboard.paths_install'), value: props.stats.installDir }, { label: props.t('dashboard.paths_shims'), value: props.stats.shimDir }, { label: props.t('dashboard.paths_cache'), value: props.stats.cacheDir }]);
function formatBytes(bytes) { if (!bytes) return '0 B'; const units = ['B', 'KB', 'MB', 'GB', 'TB']; const index = Math.floor(Math.log(bytes) / Math.log(1024)); return `${parseFloat((bytes / (1024 ** index)).toFixed(1))} ${units[index]}`; }
</script>

<style scoped>
.dashboard-page { display: grid; gap: var(--xdev-space-4); }
.dashboard-page__metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--xdev-space-3); }
.dashboard-page__split { display: grid; grid-template-columns: minmax(0, 3fr) minmax(220px, 2fr); gap: var(--xdev-space-4); }
dl { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: var(--xdev-space-3); margin: 0; } dt { color: var(--xdev-text-muted); font-size: 12px; } dd { min-width: 0; margin: 0; }
.dashboard-page__actions { display: grid; gap: var(--xdev-space-2); } .dashboard-page__actions button { padding: 9px 12px; border: 1px solid var(--xdev-border-strong); border-radius: var(--xdev-radius-md); background: transparent; color: var(--xdev-accent); text-align: left; cursor: pointer; }
table { width: 100%; border-collapse: collapse; } th, td { padding: 8px; border-bottom: 1px solid var(--xdev-border-subtle); text-align: left; }
@media (max-width: 900px) { .dashboard-page__metrics { grid-template-columns: repeat(2, 1fr); } .dashboard-page__split { grid-template-columns: 1fr; } }
</style>

<template>
  <AsyncState :loading="loading" :error="error" :retry-label="t('btn.retry')" @retry="$emit('retry')">
    <template #loading>{{ t('dashboard.loading') }}</template>
    <section class="dashboard-page">
      <div class="dashboard-page__metrics">
        <MetricCard :label="t('dashboard.total_tools')" :value="stats.totalTools || 0" />
        <MetricCard :label="t('dashboard.installed')" :value="stats.installedCount || 0" />
        <MetricCard clickable :hint="t('dashboard.size_card_hint')" :label="t('dashboard.install_size')" :value="formatBytes(stats.installSize || 0)" @click="openInstallSize" />
        <MetricCard clickable :hint="t('dashboard.size_card_hint')" :label="t('dashboard.cache_size')" :value="formatBytes(stats.cacheSize || 0)" @click="openCacheSize" />
      </div>
      <div class="dashboard-page__workspace">
        <div class="dashboard-page__main">
          <SectionCard class="dashboard-toolchain" :title="t('dashboard.current_toolchain')">
            <template #actions><span class="dashboard-count">{{ stats.installedTools?.length || 0 }}</span></template>
            <ul v-if="stats.installedTools?.length" class="toolchain-grid">
              <li v-for="tool in stats.installedTools.slice(0, 3)" :key="tool.name || tool.displayName" class="toolchain-card">
                <div class="toolchain-card__identity">
                  <span class="tool-mark" aria-hidden="true">{{ (tool.displayName || tool.name || '?').slice(0, 2) }}</span>
                  <strong>{{ tool.displayName || tool.name }}</strong>
                </div>
                <span class="toolchain-card__version">{{ tool.version || '-' }}</span>
                <span class="toolchain-card__status"><span aria-hidden="true">✓</span>{{ t('status.active') }}</span>
                <span class="toolchain-card__name">{{ tool.name || tool.category }}</span>
              </li>
            </ul>
            <EmptyState v-else :title="t('env.no_installed_tools')" />
          </SectionCard>
          <SectionCard class="dashboard-installed dashboard-table-card" :title="t('dashboard.installed_tools')">
            <template #actions><span class="dashboard-count">{{ stats.installedTools?.length || 0 }}</span></template>
            <div v-if="stats.installedTools?.length" class="dashboard-table-wrap">
              <table>
                <thead><tr><th>{{ t('table.tool') }}</th><th>{{ t('table.version') }}</th><th>{{ t('detail.category') }}</th><th>{{ t('table.action') }}</th></tr></thead>
                <tbody>
                  <template v-for="tool in stats.installedTools" :key="`${tool.name || tool.displayName}-${tool.version}`">
                    <tr>
                      <th scope="row"><span class="installed-tool"><span class="tool-mark" aria-hidden="true">{{ (tool.displayName || tool.name || '?').slice(0, 2) }}</span><span>{{ tool.displayName || tool.name }}</span></span></th>
                      <td class="version-cell">{{ tool.version || '-' }}</td><td>{{ tool.category }}</td>
                      <td><button class="xdev-btn xdev-btn--secondary xdev-btn--sm" type="button" @click="$emit('manage-versions', tool)">{{ t('favorites.manage_versions') }}</button></td>
                    </tr>
                    <tr v-if="expandedName === tool.name" class="version-manager-row"><td colspan="4"><div class="version-manager"><strong>{{ t('detail.installed_versions') }}</strong><VersionList :versions="installedVersions" mode="installed" :t="t" @use="v => $emit('use-version', tool.name, v)" @uninstall="v => $emit('uninstall-version', tool.name, v)" @repair="v => $emit('repair', tool.name, v)"/><strong>{{ t('detail.available_versions') }}</strong><VersionList :versions="availableVersions" mode="available" :limit="5" :t="t" @install="v => $emit('install', tool.name, v)"/></div></td></tr>
                  </template>
                </tbody>
              </table>
            </div>
            <EmptyState v-else :title="t('env.no_installed_tools')" />
          </SectionCard>
          <SectionCard class="dashboard-updates dashboard-table-card" :title="t('updates.title')">
            <template #actions><span v-if="updates?.length" class="dashboard-count dashboard-count--warning">{{ updates.length }}</span><button class="xdev-btn xdev-btn--primary xdev-btn--sm" data-action="install-all-updates" type="button" :disabled="!updates?.length || batchInstalling" @click="$emit('install-all-updates')">{{ t('updates.install_all') }}</button><button class="xdev-btn xdev-btn--secondary xdev-btn--sm" type="button" :disabled="updatesLoading" @click="$emit('refresh-updates')">{{ t('btn.refresh') }}</button></template>
            <n-empty v-if="!updates?.length" :description="t('updates.empty')" />
            <div v-else class="dashboard-table-wrap"><table><thead><tr><th>{{ t('table.tool') }}</th><th>{{ t('table.version') }}</th><th>{{ t('ai.command') }}</th><th>{{ t('table.action') }}</th></tr></thead><tbody><tr v-for="u in updates" :key="u.tool"><th scope="row">{{ u.tool }}</th><td class="version-cell">{{ u.currentVersion }} <span class="version-arrow">→</span> {{ u.latestVersion }}<div v-if="u.stale"><small>{{ t('updates.cached_refresh', { reason: u.refreshError || u.refreshStatus }) }}</small></div></td><td><code>{{ u.command }}</code></td><td><button class="xdev-btn xdev-btn--primary xdev-btn--sm" type="button" @click="$emit('install-update', u)">{{ t('updates.install') }}</button></td></tr></tbody></table></div>
          </SectionCard>
          <SectionCard id="dashboard-scan-results" ref="scanResultsCard" class="dashboard-scan-results dashboard-table-card" :title="t('scan.results_title')" tabindex="-1">
            <template #actions><span class="dashboard-count">{{ scanResults.length }}</span></template>
            <n-empty v-if="!scanResults.length" :description="scanAttempted ? t('scan.empty') : t('dashboard.scan_pending')" />
            <div v-else class="dashboard-table-wrap"><table><thead><tr><th>{{ t('table.tool') }}</th><th>{{ t('table.version') }}</th><th>{{ t('table.local_path') }}</th><th>{{ t('table.action') }}</th></tr></thead><tbody><tr v-for="item in scanResults" :key="`${item.name}-${item.localPath || item.path}`"><th scope="row">{{ item.displayName || item.name }}</th><td class="version-cell">{{ item.localVersion || item.version || '-' }}</td><td class="path-cell">{{ item.localPath || item.path || '-' }}</td><td><div class="table-actions"><button class="xdev-btn xdev-btn--primary xdev-btn--sm" type="button" data-action="scan-migrate" :disabled="migrationBusy" @click="$emit('scan-migrate', item)">{{ t('scan.migrate') }}</button><button class="xdev-btn xdev-btn--secondary xdev-btn--sm" type="button" @click="$emit('scan-ignore', item)">{{ t('scan.ignore') }}</button></div></td></tr></tbody></table></div>
          </SectionCard>
          <SectionCard class="dashboard-ignored dashboard-table-card" :title="t('ignore.list_title')">
            <template #actions><span class="dashboard-count">{{ ignoredTools.length }}</span></template>
            <n-empty v-if="!ignoredTools.length" :description="t('ignore.empty')" />
            <div v-else class="dashboard-table-wrap"><table><thead><tr><th>{{ t('table.tool') }}</th><th>{{ t('ignore.kind') }}</th><th>{{ t('table.local_path') }}</th><th>{{ t('ignore.ignored_at') }}</th><th>{{ t('table.action') }}</th></tr></thead><tbody><tr v-for="item in ignoredTools" :key="`${item.name}-${item.kind}-${item.path}`"><th scope="row">{{ item.name }}</th><td>{{ item.kind }}</td><td class="path-cell">{{ item.path || '-' }}</td><td>{{ item.ignoredAt || '-' }}</td><td><button class="xdev-btn xdev-btn--secondary xdev-btn--sm" type="button" @click="$emit('unignore', item)">{{ t('ignore.unignore') }}</button></td></tr></tbody></table></div>
          </SectionCard>
        </div>
        <aside class="dashboard-page__sidebar">
          <SectionCard class="dashboard-quick-actions" :title="t('dashboard.quick_actions')">
            <div class="dashboard-page__actions">
              <button class="xdev-btn xdev-btn--primary" data-action="scan-local" type="button" :disabled="scanOperating" @click="$emit('scan-local')"><span v-if="scanOperating" class="loading-spinner" aria-hidden="true" /><svg v-else class="action-icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><circle cx="8.5" cy="8.5" r="5.5"/><path d="m13 13 4 4"/></svg>{{ scanOperating ? t('dashboard.scanning') : t('dashboard.scan_local') }}</button>
              <button class="xdev-btn xdev-btn--secondary" data-action="check-update" type="button" :disabled="updateOperating" @click="$emit('check-update')"><span v-if="updateOperating" class="loading-spinner" aria-hidden="true" /><svg v-else class="action-icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M16 8a6 6 0 0 0-10-3L3 8m0-5v5h5m-4 4a6 6 0 0 0 10 3l3-3m0 5v-5h-5"/></svg>{{ updateOperating ? t('dashboard.checking_update') : t('dashboard.check_update') }}</button>
              <button class="xdev-btn xdev-btn--secondary sniff-button" type="button" :disabled="sniffing" @click="$emit('sniff-all')"><span v-if="sniffing" class="loading-spinner" aria-hidden="true" /><svg v-else class="action-icon" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 15H5a3.5 3.5 0 0 1-.3-7A5.5 5.5 0 0 1 15 6.5a4.3 4.3 0 0 1 .2 8.5H14M10 9v8m-3-3 3 3 3-3"/></svg>{{ sniffing ? t('updates.sniffing') : t('updates.sniff_all') }}</button>
            </div>
          </SectionCard>
          <SectionCard class="dashboard-paths" :title="t('dashboard.paths')">
            <dl><div v-for="path in paths" :key="path.label" class="dashboard-path"><dt>{{ path.label }}</dt><dd><CopyableValue :value="path.value || '-'" :copy-label="t('btn.copy')" /></dd></div></dl>
          </SectionCard>
          <SectionCard class="dashboard-scan-summary" :title="t('dashboard.scan_summary')">
            <div class="scan-summary" :aria-busy="scanOperating">
              <span class="scan-summary__count" aria-hidden="true">{{ scanResults.length }}</span>
              <div class="scan-summary__content"><strong role="status">{{ scanOperating ? t('dashboard.scanning') : scanResults.length ? t('dashboard.local_found', { count: scanResults.length }) : scanAttempted ? t('dashboard.local_none') : t('dashboard.scan_pending') }}</strong><p v-if="scanResults.length">{{ scanResults.map(item => item.displayName || item.name).join(' · ') }}</p></div>
            </div>
            <button class="xdev-btn xdev-btn--secondary xdev-btn--sm scan-summary__link" type="button" data-action="view-scan-results" aria-controls="dashboard-scan-results" @click="showScanResults">{{ t('scan.results_title') }} <span aria-hidden="true">↓</span></button>
          </SectionCard>
        </aside>
      </div>
    </section>
  </AsyncState>
  <n-drawer v-model:show="installSizeOpen" placement="right" :width="drawerWidth">
    <n-drawer-content closable :title="installCategoryOpen || t('dashboard.install_size_detail')">
      <div class="size-drawer">
        <div v-if="installCategoryOpen" class="size-drawer__summary size-drawer__summary--category"><button type="button" class="xdev-btn xdev-btn--secondary xdev-btn--sm" data-action="install-size-back" :aria-label="t('dashboard.size_back')" @click="installCategoryOpen = null">&larr; {{ t('dashboard.size_back') }}</button><div class="size-drawer__total"><strong>{{ installCategoryOpen }}</strong><span>{{ formatBytes(totalSize(installCategoryEntries)) }}</span></div><div class="size-sort" role="group"><button type="button" class="xdev-btn xdev-btn--secondary xdev-btn--sm size-sort__button" :aria-pressed="installSortMode === 'size'" @click="installSortMode = 'size'">{{ t('dashboard.sort_by_size') }}</button><button type="button" class="xdev-btn xdev-btn--secondary xdev-btn--sm size-sort__button" :aria-pressed="false" disabled>{{ t('dashboard.sort_by_tool') }}</button></div></div>
        <div v-else class="size-drawer__summary"><div class="size-drawer__total"><strong>{{ t('dashboard.size_total') }}</strong><span>{{ formatBytes(totalSize(installSizeEntries)) }}</span></div><div class="size-sort" role="group"><button type="button" class="xdev-btn xdev-btn--secondary xdev-btn--sm size-sort__button" :aria-pressed="installSortMode === 'size'" @click="installSortMode = 'size'">{{ t('dashboard.sort_by_size') }}</button><button type="button" class="xdev-btn xdev-btn--secondary xdev-btn--sm size-sort__button" :aria-pressed="installSortMode === 'tool'" @click="installSortMode = 'tool'">{{ t('dashboard.sort_by_tool') }}</button></div></div>
        <n-spin :show="installSizeLoading">
          <template v-if="installCategoryOpen">
            <n-empty v-if="!installSizeLoading && !installCategoryEntries.length" :description="t('updates.empty')" />
            <div v-else-if="installCategoryEntries.length" class="size-table-wrap"><table><thead><tr><th>{{ t('table.tool') }}</th><th>{{ t('table.version') }}</th><th>{{ t('table.local_path') }}</th><th>{{ t('table.size') }}</th></tr></thead><tbody><tr v-for="entry in installCategoryEntries" :key="`${entry.tool}-${entry.version}-${entry.path}`"><td>{{ entry.tool }}</td><td>{{ entry.version || '-' }}</td><td class="path-cell">{{ entry.path || '-' }}</td><td class="size-cell">{{ formatBytes(entry.size || 0) }}</td></tr></tbody></table></div>
          </template>
          <template v-else>
            <n-empty v-if="!installSizeLoading && !installCategories.length" :description="t('updates.empty')" />
            <div v-else-if="installCategories.length" class="cache-category-list install-category-list" role="table" :aria-label="t('dashboard.install_size_detail')"><div class="cache-category-list__header" role="row"><span role="columnheader">{{ t('table.tool') }}</span><span role="columnheader">{{ t('dashboard.size_version_count') }}</span><span role="columnheader">{{ t('table.size') }}</span></div><button v-for="category in installCategories" :key="category.tool" type="button" class="cache-category-row" data-action="install-size-category" :aria-label="category.tool" @click="openInstallCategory(category.tool)"><span>{{ category.tool }}</span><span>{{ category.count }}</span><span>{{ formatBytes(category.size) }}</span></button></div>
          </template>
        </n-spin>
      </div>
    </n-drawer-content>
  </n-drawer>
  <n-drawer v-model:show="cacheSizeOpen" placement="right" :width="drawerWidth">
    <n-drawer-content closable :title="cacheCategoryOpen ? t(cacheCategoryLabelKey[cacheCategoryOpen] || 'cache.other') : t('dashboard.cache_size_detail')">
      <div class="size-drawer">
        <div v-if="cacheCategoryOpen" class="size-drawer__summary size-drawer__summary--category"><button type="button" class="xdev-btn xdev-btn--secondary xdev-btn--sm" data-action="cache-size-back" :aria-label="t('dashboard.size_back')" @click="cacheCategoryOpen = null">&larr; {{ t('dashboard.size_back') }}</button><div class="size-drawer__total"><strong>{{ t(cacheCategoryLabelKey[cacheCategoryOpen] || 'cache.other') }}</strong><span>{{ formatBytes(totalSize(cacheCategoryEntries)) }}</span></div><div class="size-sort" role="group"><button type="button" class="xdev-btn xdev-btn--secondary xdev-btn--sm size-sort__button" :aria-pressed="cacheSortMode === 'size'" @click="cacheSortMode = 'size'">{{ t('dashboard.sort_by_size') }}</button><button type="button" class="xdev-btn xdev-btn--secondary xdev-btn--sm size-sort__button" :aria-pressed="cacheSortMode === 'tool'" @click="cacheSortMode = 'tool'">{{ t('dashboard.sort_by_tool') }}</button></div></div>
        <div v-else class="size-drawer__summary"><div class="size-drawer__total"><strong>{{ t('dashboard.size_total') }}</strong><span>{{ formatBytes(totalSize(cacheSizeEntries)) }}</span></div></div>
        <n-spin :show="cacheSizeLoading">
          <template v-if="cacheCategoryOpen">
            <n-empty v-if="!cacheSizeLoading && !cacheCategoryEntries.length" :description="t('updates.empty')" />
            <div v-else class="size-table-wrap"><table><thead><tr><th>{{ t('table.tool') }}</th><th>{{ t('table.version') }}</th><th>{{ t('table.local_path') }}</th><th>{{ t('table.size') }}</th><th>{{ t('table.action') }}</th></tr></thead><tbody><tr v-for="entry in cacheCategoryEntries" :key="`${entry.tool}-${entry.version}-${entry.path}`"><td>{{ entry.tool }}</td><td>{{ entry.version || '-' }} <n-tag v-if="entry.isStale" type="warning" size="small">{{ t('dashboard.size_stale') }}</n-tag></td><td class="path-cell">{{ entry.path || '-' }}</td><td class="size-cell">{{ formatBytes(entry.size || 0) }}</td><td><ConfirmAction :title="t('cache.delete_confirm_title')" :description="t('cache.delete_confirm_content')" :confirm-label="t('cache.delete_file')" :cancel-label="t('btn.cancel')" @confirm="$emit('delete-cache-file', entry)"><template #trigger><button type="button" class="xdev-btn xdev-btn--danger xdev-btn--sm" data-action="delete-cache-file" :aria-label="t('cache.delete_file')">{{ t('cache.delete_file') }}</button></template></ConfirmAction></td></tr></tbody></table></div>
          </template>
          <template v-else>
            <n-empty v-if="!cacheSizeLoading && !cacheCategories.length" :description="t('updates.empty')" />
            <div v-else-if="cacheCategories.length" class="cache-category-list" role="table" :aria-label="t('dashboard.cache_size_detail')"><div class="cache-category-list__header" role="row"><span role="columnheader">{{ t('cache.category') }}</span><span role="columnheader">{{ t('cache.total_count') }}</span><span role="columnheader">{{ t('table.size') }}</span><span role="columnheader">{{ t('table.action') }}</span></div><div v-for="category in cacheCategories" :key="category.key" class="cache-category-row" role="row"><button type="button" class="cache-category-row__open" data-action="cache-size-category" :aria-label="t(cacheCategoryLabelKey[category.key] || 'cache.other')" @click="openCacheCategory(category.key)"><span>{{ t(cacheCategoryLabelKey[category.key] || 'cache.other') }}</span><span>{{ category.count }}</span><span>{{ formatBytes(category.size) }}</span></button><ConfirmAction :title="t('cache.clean_category_confirm_title')" :description="t('cache.clean_category_confirm_content')" :confirm-label="t('cache.clean_category')" :cancel-label="t('btn.cancel')" @confirm="$emit('clean-cache-category', category.key)"><template #trigger><button type="button" class="xdev-btn xdev-btn--secondary xdev-btn--sm" data-action="clean-cache-category" :aria-label="t('cache.clean_category')">{{ t('cache.clean_category') }}</button></template></ConfirmAction></div></div>
          </template>
        </n-spin>
      </div>
    </n-drawer-content>
  </n-drawer>
</template>
<script setup>
import { computed, ref } from 'vue'; import { NDrawer, NDrawerContent, NEmpty, NSpin, NTag } from 'naive-ui'; import AsyncState from '../components/AsyncState.vue'; import ConfirmAction from '../components/ConfirmAction.vue'; import CopyableValue from '../components/CopyableValue.vue'; import EmptyState from '../components/EmptyState.vue'; import MetricCard from '../components/MetricCard.vue'; import SectionCard from '../components/SectionCard.vue'; import VersionList from '../components/VersionList.vue'; import { cacheCategoryLabelKey, cacheCategoryOrder } from '../utils/cacheCategories.js'; import { formatBytes } from '../utils/format.js';
const props = defineProps({ stats: { type: Object, default: () => ({}) }, loading: Boolean, error: { type: [String, Error], default: null }, t: { type: Function, required: true }, updates: { type: Array, default: () => [] }, updatesLoading: Boolean, sniffing: Boolean, scanOperating: Boolean, updateOperating: Boolean, batchInstalling: Boolean, expandedName: { type: String, default: '' }, installedVersions: { type: Array, default: () => [] }, availableVersions: { type: Array, default: () => [] }, scanResults: { type: Array, default: () => [] }, scanAttempted: Boolean, ignoredTools: { type: Array, default: () => [] }, installSizeEntries: { type: Array, default: () => [] }, cacheSizeEntries: { type: Array, default: () => [] }, installSizeLoading: Boolean, cacheSizeLoading: Boolean,
  // R8-02（第八轮审计，P1）：迁移进行中标志。扫描结果行的「迁移」按钮此前**无任何
  // 进行中约束**（唯一的迁移守卫只覆盖 EnvironmentVariablesPage），双击会对同一目录
  // 发起两次并发迁移（Go 侧无工具锁）。与 F-40 对 env-vars 页的做法保持一致。
  migrationBusy: { type: Boolean, default: false } });
const emit = defineEmits(['scan-local', 'check-update', 'retry', 'refresh-updates', 'install-update', 'install-all-updates', 'sniff-all', 'manage-versions', 'use-version', 'uninstall-version', 'install', 'repair', 'scan-migrate', 'scan-ignore', 'unignore', 'show-install-size', 'show-cache-size', 'clean-cache-category', 'delete-cache-file']);
const scanResultsCard = ref(null);
function showScanResults() {
  const section = scanResultsCard.value?.$el;
  section?.scrollIntoView?.({ block: 'start', behavior: 'auto' });
  section?.focus?.({ preventScroll: true });
}
const drawerWidth = 'min(640px, 90vw)'; const installSizeOpen = ref(false); const cacheSizeOpen = ref(false); const installSortMode = ref('size'); const cacheSortMode = ref('size'); const installCategoryOpen = ref(null); const cacheCategoryOpen = ref(null);
const totalSize = entries => entries.reduce((total, entry) => total + (Number(entry.size) || 0), 0);
const installCategories = computed(() => {
  const groups = new Map();
  for (const entry of props.installSizeEntries) {
    const tool = entry.tool || '-';
    const group = groups.get(tool) || { tool, count: 0, size: 0 };
    group.count += 1;
    group.size += Number(entry.size) || 0;
    groups.set(tool, group);
  }
  return [...groups.values()].sort((a, b) => installSortMode.value === 'tool' ? a.tool.localeCompare(b.tool) || b.size - a.size : b.size - a.size || a.tool.localeCompare(b.tool));
});
const cacheEntryCategory = entry => cacheCategoryOrder.includes(entry.category) ? entry.category : 'otherFiles';
const sortedInstall = computed(() => {
  const list = [...props.installSizeEntries];
  if (installSortMode.value === 'tool') list.sort((a, b) => (a.tool || '').localeCompare(b.tool || '') || (Number(b.size) || 0) - (Number(a.size) || 0));
  else list.sort((a, b) => (Number(b.size) || 0) - (Number(a.size) || 0));
  return list;
});
const installCategoryEntries = computed(() => sortedInstall.value.filter(entry => (entry.tool || '-') === installCategoryOpen.value));
const sortedCache = computed(() => {
  const list = [...props.cacheSizeEntries];
  if (cacheSortMode.value === 'tool') list.sort((a, b) => (a.tool || '').localeCompare(b.tool || '') || (Number(b.size) || 0) - (Number(a.size) || 0));
  else list.sort((a, b) => (Number(b.size) || 0) - (Number(a.size) || 0));
  return list;
});
const cacheCategoryEntries = computed(() => sortedCache.value.filter(entry => cacheEntryCategory(entry) === cacheCategoryOpen.value));
const cacheCategories = computed(() => cacheCategoryOrder.map(key => {
  const entries = props.cacheSizeEntries.filter(entry => cacheEntryCategory(entry) === key);
  return { key, count: entries.length, size: totalSize(entries) };
}).filter(category => category.count));
function openInstallSize() { installCategoryOpen.value = null; installSortMode.value = 'size'; installSizeOpen.value = true; emit('show-install-size'); }
function openInstallCategory(tool) { installCategoryOpen.value = tool; installSortMode.value = 'size'; }
function openCacheSize() { cacheCategoryOpen.value = null; cacheSortMode.value = 'size'; cacheSizeOpen.value = true; emit('show-cache-size'); }
function openCacheCategory(key) { cacheCategoryOpen.value = key; cacheSortMode.value = 'size'; }
const paths = computed(() => [{ label: props.t('dashboard.paths_home'), value: props.stats.xmdevDir }, { label: props.t('dashboard.paths_install'), value: props.stats.installDir }, { label: props.t('dashboard.paths_shims'), value: props.stats.shimDir }, { label: props.t('dashboard.paths_cache'), value: props.stats.cacheDir }]);
</script>
<style scoped>
.dashboard-page { width: 100%; max-width: var(--xdev-table-max-width); min-width: 0; margin: 0 auto; display: grid; gap: var(--xdev-space-5); }
.dashboard-page__metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--xdev-space-4); }
.dashboard-page__metrics :deep(.metric-card) { display: grid; align-content: start; min-height: 96px; padding: var(--xdev-space-3) var(--xdev-space-5); border-radius: var(--xdev-radius-lg); }
.dashboard-page__metrics :deep(.metric-card__label) { margin-bottom: var(--xdev-space-1); letter-spacing: 0; font-size: var(--xdev-font-size-xs); text-transform: none; }
.dashboard-page__metrics :deep(.metric-card__value) { font-size: clamp(24px, 2vw, 28px); line-height: 1.25; }
.dashboard-page__metrics :deep(.metric-card__hint) { margin-top: var(--xdev-space-1); font-size: 11px; line-height: 1.2; }
.dashboard-page__workspace { display: grid; grid-template-columns: minmax(0, 1fr) minmax(260px, 300px); align-items: start; gap: var(--xdev-space-5); }
.dashboard-page__main, .dashboard-page__sidebar { display: grid; align-content: start; gap: var(--xdev-space-5); min-width: 0; }
.dashboard-page :deep(.section-card) { min-width: 0; }
.dashboard-page :deep(.section-card__header) { align-items: center; flex-wrap: wrap; gap: var(--xdev-space-2); padding: var(--xdev-space-3) var(--xdev-space-5); min-height: 50px; box-sizing: border-box; }
.dashboard-page :deep(.section-card__header > div) { min-width: 0; }
.dashboard-page :deep(.section-card__header h2) { overflow-wrap: anywhere; }
.dashboard-page :deep(.section-card__actions) { align-items: center; flex-wrap: wrap; }
.dashboard-page :deep(.section-card__body) { min-width: 0; padding: var(--xdev-space-4) var(--xdev-space-5); }
.dashboard-count { display: inline-flex; align-items: center; justify-content: center; min-width: 22px; min-height: 22px; padding: 0 var(--xdev-space-1); box-sizing: border-box; border-radius: var(--xdev-radius-sm); background: var(--xdev-bg-muted); color: var(--xdev-text-secondary); font-family: var(--xdev-font-mono); font-size: var(--xdev-font-size-xs); font-variant-numeric: tabular-nums; }
.dashboard-count--warning { color: var(--xdev-warning-text); background: var(--xdev-warning-bg); }
.toolchain-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--xdev-space-3); list-style: none; margin: 0; padding: 0; }
.toolchain-card { display: grid; align-content: start; gap: var(--xdev-space-2); min-width: 0; padding: var(--xdev-space-3); border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-md); background: var(--xdev-bg-elevated); }
.toolchain-card__identity, .installed-tool { display: flex; align-items: center; gap: var(--xdev-space-2); min-width: 0; }
.toolchain-card__identity strong { font-size: var(--xdev-font-size-sm); overflow-wrap: anywhere; }
.tool-mark { display: inline-flex; flex: 0 0 26px; width: 26px; height: 26px; align-items: center; justify-content: center; border: 1px solid var(--xdev-border-strong); border-radius: var(--xdev-radius-md); color: var(--xdev-accent); background: var(--xdev-accent-soft); font: 10px var(--xdev-font-mono); }
.toolchain-card__version { color: var(--xdev-text-primary); font-family: var(--xdev-font-mono); font-size: 21px; font-weight: 650; line-height: 1.3; letter-spacing: -0.04em; overflow-wrap: anywhere; }
.toolchain-card__status { display: flex; align-items: center; gap: var(--xdev-space-1); color: var(--xdev-success); font-size: 11px; }
.toolchain-card__name { color: var(--xdev-text-muted); font-family: var(--xdev-font-mono); font-size: 11px; overflow-wrap: anywhere; }
.dashboard-table-card :deep(.section-card__body):has(> .dashboard-table-wrap) { padding: 0; }
.dashboard-table-wrap { min-width: 0; max-width: 100%; overflow-x: auto; }
.dashboard-table-wrap table { min-width: 480px; }
.dashboard-table-card :deep(.n-empty) { padding: var(--xdev-space-5) 0; }
.dashboard-page__actions { display: grid; gap: var(--xdev-space-2); }
.dashboard-page__actions .xdev-btn { justify-content: flex-start; gap: var(--xdev-space-2); width: 100%; min-height: 40px; height: auto; padding-top: var(--xdev-space-2); padding-bottom: var(--xdev-space-2); white-space: normal; text-align: left; }
.action-icon { width: 16px; height: 16px; flex: 0 0 16px; }
.loading-spinner { width: 14px; height: 14px; flex: 0 0 14px; border: 2px solid currentColor; border-right-color: transparent; border-radius: 50%; animation: spin .7s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
dl { display: grid; gap: var(--xdev-space-4); margin: 0; }
.dashboard-path { display: grid; min-width: 0; gap: var(--xdev-space-1); }
dt { color: var(--xdev-text-muted); font-size: var(--xdev-font-size-xs); }
dd { min-width: 0; margin: 0; }
.dashboard-paths :deep(.copyable-value) { width: 100%; }
.dashboard-paths :deep(.copyable-value__path) { font-size: var(--xdev-font-size-xs); line-height: 1.6; }
.scan-summary { display: flex; align-items: flex-start; gap: var(--xdev-space-3); }
.scan-summary__count { display: grid; place-items: center; flex: 0 0 36px; width: 36px; height: 36px; background: var(--xdev-accent-soft); color: var(--xdev-accent); border-radius: var(--xdev-radius-md); font: 18px var(--xdev-font-mono); }
.scan-summary__content { min-width: 0; color: var(--xdev-text-primary); font-size: var(--xdev-font-size-sm); line-height: 1.5; overflow-wrap: anywhere; }
.scan-summary__content p { margin: var(--xdev-space-1) 0 0; color: var(--xdev-text-muted); font-size: var(--xdev-font-size-xs); }
.scan-summary__link { margin-top: var(--xdev-space-4); gap: var(--xdev-space-2); max-width: 100%; height: auto; white-space: normal; }
.dashboard-scan-results { scroll-margin-block-start: var(--xdev-space-5); }
.dashboard-scan-results:focus-visible { outline: none; box-shadow: var(--xdev-focus-ring); }
table { width: 100%; border-collapse: collapse; font-size: var(--xdev-font-size-sm); }
th, td { padding: var(--xdev-space-3) var(--xdev-space-5); border-bottom: 1px solid var(--xdev-border-subtle); text-align: left; vertical-align: middle; }
thead th { color: var(--xdev-text-muted); background: var(--xdev-bg-elevated); font-size: var(--xdev-font-size-xs); font-weight: 500; }
tbody th { color: var(--xdev-text-primary); font-size: var(--xdev-font-size-sm); font-weight: 600; }
tbody tr:last-child > th, tbody tr:last-child > td { border-bottom: 0; }
table tbody tr:hover { background: var(--xdev-bg-hover); }
.version-cell { font-family: var(--xdev-font-mono); font-size: var(--xdev-font-size-xs); font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.version-arrow { margin: 0 var(--xdev-space-1); color: var(--xdev-text-muted); }
.path-cell { max-width: 280px; min-width: 150px; overflow-wrap: anywhere; color: var(--xdev-text-muted); font-family: var(--xdev-font-mono); font-size: var(--xdev-font-size-xs); }
.table-actions { display: flex; flex-wrap: wrap; gap: var(--xdev-space-2); }
code { display: inline-block; overflow-wrap: anywhere; background: var(--xdev-bg-muted); padding: 2px 6px; border-radius: var(--xdev-radius-sm); font-family: var(--xdev-font-mono); font-size: var(--xdev-font-size-xs); }
.version-manager-row:hover { background: transparent !important; }
.version-manager-row td { padding: 0 var(--xdev-space-3) var(--xdev-space-4); }
.version-manager { display: grid; gap: var(--xdev-space-2); padding: var(--xdev-space-3); background: var(--xdev-bg-panel); border-left: 2px solid var(--xdev-accent); } .version-manager strong { color: var(--xdev-text-muted); font-size: 11px; font-weight: 600; letter-spacing: 0.08em; text-transform: uppercase; }
.size-drawer { display: grid; gap: var(--xdev-space-4); min-width: 0; } .size-drawer__summary { display: flex; justify-content: space-between; gap: var(--xdev-space-3); padding: var(--xdev-space-3) 0; border-bottom: 1px solid var(--xdev-border-subtle); color: var(--xdev-text-secondary); } .size-drawer__summary span { color: var(--xdev-text-primary); font-weight: 600; font-variant-numeric: tabular-nums; } .size-table-wrap { max-width: 100%; overflow-x: auto; } .size-table-wrap table { min-width: 560px; } .size-table-wrap th, .size-table-wrap td { padding: var(--xdev-space-2) var(--xdev-space-3); } .size-table-wrap .path-cell { min-width: 180px; } .size-cell { white-space: nowrap; font-variant-numeric: tabular-nums; }
@media (max-width: 1100px) { .dashboard-page__workspace { grid-template-columns: minmax(0, 1fr) minmax(230px, 270px); } th, td { padding-inline: var(--xdev-space-3); } }
@media (max-width: 900px) { .dashboard-page__metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); } .dashboard-page__workspace { grid-template-columns: minmax(0, 1fr); } .dashboard-page__sidebar { grid-template-columns: repeat(2, minmax(0, 1fr)); } .dashboard-scan-summary { grid-column: 1 / -1; } }
@media (max-width: 640px) { .dashboard-page, .dashboard-page__main, .dashboard-page__sidebar { gap: var(--xdev-space-3); } .dashboard-page__metrics { gap: var(--xdev-space-2); } .dashboard-page__metrics :deep(.metric-card) { padding: var(--xdev-space-3); } .dashboard-page :deep(.section-card__header), .dashboard-page :deep(.section-card__body) { padding-inline: var(--xdev-space-3); } .dashboard-table-card :deep(.section-card__body):has(> .dashboard-table-wrap) { padding-inline: 0; } .toolchain-grid { grid-template-columns: minmax(0, 1fr); } .toolchain-card { grid-template-columns: minmax(0, 1fr) auto; align-items: center; } .toolchain-card__version { font-size: 18px; } .toolchain-card__name { text-align: right; } .dashboard-page__sidebar { grid-template-columns: minmax(0, 1fr); } .size-sort { flex-wrap: wrap; } }
@media (prefers-reduced-motion: reduce) { .loading-spinner { animation: none; } }
.size-drawer__summary { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--xdev-space-3); }
.size-drawer__total { display: flex; align-items: baseline; gap: var(--xdev-space-2); }
.size-sort { display: flex; gap: var(--xdev-space-1); margin-left: auto; }
.size-sort__button { white-space: nowrap; }
.size-sort__button[aria-pressed="true"] { border-color: var(--xdev-accent); color: var(--xdev-accent); background: var(--xdev-accent-soft); }
.size-drawer__summary--category { align-items: center; }
.cache-category-list { width: 100%; min-width: 0; overflow: hidden; border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-md); }
.cache-category-list__header, .cache-category-row { display: grid; grid-template-columns: minmax(0,1fr) 90px 110px; align-items: center; gap: var(--xdev-space-3); width: 100%; min-width: 0; padding: var(--xdev-space-3); text-align: left; }
.cache-category-list__header { color: var(--xdev-text-muted); font-size: 11px; font-weight: 600; letter-spacing: 0.08em; text-transform: uppercase; }
.cache-category-row { border: 0; border-top: 1px solid var(--xdev-border-subtle); background: transparent; color: var(--xdev-text-primary); font: inherit; }
.cache-category-list > .cache-category-row { grid-template-columns: minmax(0,1fr) auto; }
.cache-category-row__open { display: grid; grid-template-columns: minmax(0,1fr) 90px 110px; align-items: center; gap: var(--xdev-space-3); min-width: 0; flex: 1; padding: 0; border: 0; background: transparent; color: inherit; cursor: pointer; font: inherit; text-align: left; }
.cache-category-row__open span:nth-child(2), .cache-category-row__open span:nth-child(3) { text-align: right; }
.install-category-list .cache-category-list__header, .install-category-list .cache-category-row { grid-template-columns: minmax(0,1fr) 90px 110px; }
.size-table-wrap th:last-child, .size-table-wrap td:last-child { min-width: 72px; white-space: nowrap; }
.cache-category-row:hover { background: var(--xdev-bg-hover); }
.cache-category-row:focus-visible { outline: none; box-shadow: inset var(--xdev-focus-ring); }
.cache-category-row span:nth-child(2), .cache-category-row span:nth-child(3), .cache-category-list__header span:nth-child(2), .cache-category-list__header span:nth-child(3) { text-align: right; }
</style>

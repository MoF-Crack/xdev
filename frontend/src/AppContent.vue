<template>
  <AppShell :selected-view="selectedView" :navigation-groups="localizedNavigationGroups" :t="t"
    :minimize="minimizeWindow" :toggle-maximize="toggleMaximize" :close="closeWindow"
    :minimize-label="t('window.minimize')" :maximize-label="isMaximized ? t('window.restore') : t('window.maximize')" :close-label="t('window.close')"
    @select-view="onMenuSelect" @open-command-palette="openCommandPalette" @escape="() => { /* G-05: Escape 只归命令面板所有——无条件调用 setCommandPaletteVisible(false) 会把焦点抢到 Ctrl+K 触发器上，破坏自行处理 Escape 的抽屉/对话框的焦点返还契约。 */ if (showCommandPalette) setCommandPaletteVisible(false); }">
    <template #toolbar>
      <PageToolbar :title="headerTitle">
        <n-space>
          <!-- 后台安装进展指示器：安装转入后台（showProgress=false 但仍 active）时显示，点击重新打开进度框 -->
          <!-- Spec 037：批量并行时显示项数而非单项百分比 -->
          <n-button v-if="batchInstalling && !showProgress" size="small" type="primary" :aria-label="t('install.parallel_background', { n: batchActiveCount })" data-action="show-install-progress" @click="showProgress = true">
            <template #icon><n-icon aria-hidden="true"><cloud-download-outline /></n-icon></template>
            {{ t('install.parallel_background', { n: batchActiveCount }) }}
          </n-button>
          <n-button v-else-if="installing && !showProgress" size="small" type="primary" :aria-label="t('install.background_progress')" data-action="show-install-progress" @click="showProgress = true">
            <template #icon><n-icon aria-hidden="true"><cloud-download-outline /></n-icon></template>
            {{ t('install.background_progress') }} · {{ installProgress.percent || 0 }}%
          </n-button>
          <UpdateBadge :t="t" :count="availableUpdates.length" :updates="availableUpdates" :loading="updatesLoading" :sniffing="sniffOperating" :installing="updateInstalling" @refresh="loadUpdates" @install="installUpdate" @ignore="ignoreUpdate" @sniff-all="sniffAll" />
          <NotificationCenter :t="t" :notifications="notifications" @clear="clearNotifications" @dismiss="dismissNotification" />
          <n-button ref="commandPaletteTrigger" size="small" secondary :aria-label="t('command_palette.open')" @click="openCommandPalette">
            <template #icon><n-icon aria-hidden="true"><search-outline /></n-icon></template>
            Ctrl+K
          </n-button>
        </n-space>
      </PageToolbar>
    </template>
        <!-- Dashboard -->
        <DashboardPage v-if="selectedView === 'dashboard'" :stats="stats" :loading="dashboardLoading" :error="dashboardError" :t="t" :updates="availableUpdates" :updates-loading="updatesLoading" :sniffing="sniffOperating" :scan-operating="scanOperating" :update-operating="updateOperating" :batch-installing="batchInstalling" :expanded-name="dashVersionExpanded" :installed-versions="dashInstalledVers" :available-versions="dashAvailableVers" :scan-results="scanResults" :scan-attempted="scanAttempted" :ignored-tools="ignoredTools" :install-size-entries="installSizeEntries" :cache-size-entries="cacheSizeEntries" :install-size-loading="installSizeLoading" :cache-size-loading="cacheSizeLoading" :migration-busy="!!migratingAll || !!migratingSingle" @scan-local="scanLocal" @check-update="checkUpdate" @retry="loadDashboard" @refresh-updates="loadUpdates" @install-update="installUpdate" @install-all-updates="installAllUpdates" @sniff-all="sniffAll" @manage-versions="openDashVersionManager" @use-version="useVersion" @uninstall-version="confirmUninstall" @install="installDashVersion" @repair="repairDashVersion" @scan-migrate="migrateScanTool" @scan-ignore="ignoreScanTool" @unignore="unignoreTool" @show-install-size="showInstallSize" @show-cache-size="showCacheSize" @clean-cache-category="cleanCacheCategory" @delete-cache-file="deleteCacheFile" />

        <!-- Favorites Page -->
        <FavoritesPage v-else-if="selectedView === 'favorites'" :favorites="favorites" :expanded-name="favVersionExpanded" :installed-versions="favInstalledVers" :available-versions="favAvailableVers" :t="t" :migration-busy="!!migratingAll || !!migratingSingle" @open-detail="openDetailFromFav" @remove-favorite="removeFavorite" @install-latest="installFavLatest" @migrate="migrateTool" @manage-versions="openFavVersionManager" @switch-local="switchToLocal" @use-version="useVersion" @uninstall-version="confirmUninstall" @install="doInstall" @repair="repairInstall" />
        <!-- Settings -->
        <SettingsPage v-else-if="selectedView === 'settings'" :t="t" :theme="themeName" :locale="locale" :path-mode="pathMode" :migration-mode="migrationMode" :mirror-config="mirrorConfig" :mirror-test-results="mirrorTestResults" :proxy="proxyModel" :proxy-test-result="proxyTestResult" :proxy-probes="proxyProbes" :import-result="importResult" :update-info="updateInfo" :diag-results="diagResults" :cache-breakdown="cacheBreakdown" :archive-retention="archiveRetention" :sniff-config="sniffConfig" :service-status="serviceStatus" :custom-tools="customTools" :custom-tool-submitting="customToolSubmitting" :custom-tool-preview="customToolPreview" @update:theme="setTheme" @update:locale="setLocale" @update:path-mode="switchPathMode" @update:migration-mode="switchMigrationMode" @mirror-update="updateMirror" @mirror-test="testMirror" @mirror-remove="removeMirror" @mirror-add="addMirrorValues" @mirror-save="saveMirrors" @update:proxy="updateProxy" @proxy-test="testProxy" @proxy-save="saveProxy" @proxy-probe="probeLocalProxies" @apply-proxy-probe="applyProxyProbe" @profile-export="exportProfile" @profile-import="importProfile" @check-update="checkUpdate" @run-diagnostics="runDiag" @cache-clean="cleanCache" @update:archive-retention="saveArchiveRetention" @update:sniff-config="sniffConfig.value = $event" @save-sniff-strategy="saveSniffStrategy" @install-sniff-service="installSniffService" @uninstall-sniff-service="uninstallSniffService" @refresh-sniff-status="loadSniffServiceStatus" @preview-custom-tool="previewCustomToolVersions" @add-custom-tool="submitAddCustomTool" @remove-custom-tool="removeCustomTool" />
        <!-- Environment Variables Page -->
        <EnvironmentVariablesPage v-else-if="selectedView === 'env-vars'" :t="t" :all-env-vars="allEnvVars" :all-level="envAllLevel" :validation-stats="envValidationStats" :migration-mode="migrationMode" :migration-tools="migrationTools" :migration-results="migrationResults" :migration-busy="!!migratingAll || !!migratingSingle" @update:all-level="setEnvAllLevel" @validate="validateAllEnvVars" @new="newEnvVar" @edit="editEnvVar" @delete="deleteEnvRow" @clean="cleanEnvRow" @clean-all-invalid="cleanAllInvalidEnvVars" @migration-scan="scanForMigration" @migration-single="migrateSingle" @migration-all="migrateAll" />

        <IsolationPage v-else-if="selectedView === 'isolate'" :t="t" :status="isolateStatus" :loading="isolateLoading" :error="isolateError" :output="isolateExecOutput" :operating="isolateOperating" @setup="isolateSetup" @exec="isolateExec" @teardown="isolateTeardown" />

        <RemoteDevelopmentPage v-else-if="selectedView === 'remote'" :t="t" :instances="remoteInstances" :loading="remoteLoading" :error="remoteError" :creating="remoteCreating" :operating="remoteOperating" :outputs="remoteOutputs" :browse-file="browseRemoteKey" :test-ssh="testSSHConnection" @list="loadRemoteInstances" @create="createRemoteInstance" @copy-address="copyRemoteAddress" @exec="execRemoteInstance" @destroy="destroyRemoteInstance" />

        <AboutPage v-else-if="selectedView === 'about'" :t="t" :info="aboutInfo" @check-update="checkUpdate" />

        <!-- Tool list (table style) -->
        <ToolCatalogPage v-else :tools="filteredTools" :category="activeCategory" :loading="toolsLoading" :error="toolsError" v-model:search-query="searchQuery" v-model:hide-unavailable="hideUnavailable" :t="t" @open-detail="openDetail" @toggle-favorite="toggleCatalogFavorite" @retry="retryTools" />
  </AppShell>

  <ToolDetailDrawer v-model:show="showDrawer" :tool="selectedTool" :installed-versions="installedVersions" :available-versions="availableVersions" :show-count="showCount" :operating-tool="pmOperating" :return-focus-to="drawerTrigger" :deps="toolDeps" :t="t" :migration-busy="!!migratingAll || !!migratingSingle" @toggle-favorite="toggleFavorite" @migrate="migrateTool" @switch-local="switchToLocal" @switch-xdev="switchToXdev" @use-version="useVersion" @uninstall-version="confirmUninstall" @install="doInstall" @repair="repairInstall" @show-more="showCount += 20" @launch="launchTool" @copy-command="copyToolCommand" @migrate-pm="migratePackageManager" @install-package-manager="installPackageManager" @update-package-manager="updatePackageManager" />
  <!-- Install progress modal -->
  <InstallProgressDialog v-model:show="showProgress" :active="installing" :paused="downloadPaused" :phase="installPhase" :progress="installProgress" :error="installError" :return-focus-to="installTrigger" :items="batchActive ? batchItems : []" :t="t" @pause="pauseDownload" @resume="resumeDownload" @cancel="cancelDownload" @cancel-item="cancelBatchItem" @cancel-all="cancelAllBatch" />
  <FormSelectorDialog v-model:show="showFormSelector" :tool="formSelectorTool" :t="t" @confirm="confirmFormInstall" />
  <EnvVarEditorDialog :show="showEnvEditor" :editing="editingEnvVar" :browse-directory="browseEnvDir" :t="t" @update:show="showEnvEditor = $event" @save="saveEnvDraft" />
  <!-- Spec 034: 启动自检更新三选对话框 -->
  <SelfUpdateDialog :show="showSelfUpdate" :current="selfUpdateInfo.current" :latest="selfUpdateInfo.latest" :updating="selfUpdating" :t="t" @update="applySelfUpdate" @ignore="ignoreSelfUpdate" @quit="quitSelfUpdate" />
  <CommandPalette
    v-model:show="showCommandPalette"
    :options="commandPaletteOptions"
    :title="t('command_palette.title')"
    :search-label="t('command_palette.search')"
    :empty-label="t('command_palette.empty')"
    @update:show="setCommandPaletteVisible"
    @select="onCommandSelect"
  />
</template>

<script setup>
import { h, ref, computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, watch } from 'vue';
import { NIcon, NButton, NSpace, useMessage, useDialog } from 'naive-ui';
import {
  SearchOutline,
  HomeOutline, SettingsOutline, CodeOutline, PhonePortraitOutline, ConstructOutline,
  RocketOutline, DesktopOutline, CloudDownloadOutline,
} from '@vicons/ionicons5';
import { useI18n } from './i18n/index';
import CommandPalette from './components/CommandPalette.vue';
import DashboardPage from './pages/DashboardPage.vue';
import ToolCatalogPage from './pages/ToolCatalogPage.vue';
import UpdateBadge from './components/UpdateBadge.vue';
import NotificationCenter from './components/NotificationCenter.vue';
import ToolDetailDrawer from './components/ToolDetailDrawer.vue';
import InstallProgressDialog from './components/InstallProgressDialog.vue';
import FormSelectorDialog from './components/FormSelectorDialog.vue';
import EnvVarEditorDialog from './components/EnvVarEditorDialog.vue';
import SelfUpdateDialog from './components/SelfUpdateDialog.vue';
const FavoritesPage = defineAsyncComponent(() => import('./pages/FavoritesPage.vue'));
const SettingsPage = defineAsyncComponent(() => import('./pages/SettingsPage.vue'));
const EnvironmentVariablesPage = defineAsyncComponent(() => import('./pages/EnvironmentVariablesPage.vue'));
const IsolationPage = defineAsyncComponent(() => import('./pages/IsolationPage.vue'));
const RemoteDevelopmentPage = defineAsyncComponent(() => import('./pages/RemoteDevelopmentPage.vue'));
const AboutPage = defineAsyncComponent(() => import('./pages/AboutPage.vue'));
import { useInstall } from './composables/useInstall.js';
import { useBatchUpdates } from './composables/useBatchUpdates.js';
import { useParallelBatch } from './composables/useParallelBatch.js';
import { useTools } from './composables/useTools.js';
import { useNotifications } from './composables/useNotifications.js';
import { usePackageManagers } from './composables/usePackageManagers.js';
import { useWindowChrome } from './composables/useWindowChrome.js';
import { useSniffSession } from './composables/useSniffSession.js';
import AppShell from './layouts/AppShell.vue';
import PageToolbar from './layouts/PageToolbar.vue';
import { commandPaletteEntries, createNavigationGroups, findPageDefinition } from './navigation/navigation.js';
import { DEFAULT_THEME, isKnownTheme, THEMES } from './theme/themes.js';
import { sameEnvIdentity } from './pages/envIdentity.js';
import { createLatestRequestController } from './pages/settingsState.js';

const { t, locale, setLocale } = useI18n();

const message = useMessage();
const dialog = useDialog();
const { isMaximized, minimizeWindow, toggleMaximize, closeWindow } = useWindowChrome();

onUnmounted(() => {
  window.runtime?.EventsOff?.('dashboardStatsUpdated');
  window.runtime?.EventsOff?.('localCacheUpdated');
  // These two are registered in onMounted below. App.vue swaps
  // <AppContent> for <SetupWizard> on first run and back again —
  // without EventsOff every remount stacks another listener and the
  // TOFU warning dialog / self-update toast fires duplicated.
  window.runtime?.EventsOff?.('xdev:tofu-warning');
  window.runtime?.EventsOff?.('self-update');
  // F-41: 同样在 onMounted 注册，必须一并注销（否则重挂载后重复提示）。
  window.runtime?.EventsOff?.('xdev:activation-warning');
});

// R8-01（第八轮审计，P1）：事件注册必须全部落在**同步前缀**，即任何 await 之前。
//
// 缺陷现场：这 4 个 EventsOn 原本写在 `onMounted(async () => {...})` 的**多个 await
// 之后**（GetBootstrapState → loadUpdates → … → loadMigrationMode），而 onUnmounted
// 的 EventsOff 是同步的。真实时序窗口：App.vue 的 `checkInit()` 在 bootstrap 未决时
// 返回 false → <AppContent> 被 <SetupWizard> 替换 → 卸载执行 EventsOff（此时还没有
// 任何监听者）→ 挂起的 await 恢复 → **卸载之后**才 EventsOn 注册。
// Wails 的 EventsOn 按事件名把回调追加进一个 Set，EventsOff(name) 只在注册之后调用
// 才有效，因此这些监听者**永不注销**：每次卸载/重挂载泄漏 4 个，且它们会在组件已经
// 不在时继续跑 message.warning(...) / loadDashboard() —— 用户看到「幽灵」激活失败
// toast，并触发无意义的仪表盘重算。
//
// 修复：把注册提到 onMounted 的同步前缀。回调只引用函数声明（提升）与在同步前缀处
// 已初始化的 ref（showSelfUpdate/selfUpdateInfo 在 <script setup> 顶层、早于
// onMounted 回调执行时求值，故安全）；回调体内对其它 ref 的访问发生在事件到达时，
// 与注册位置无关。
function registerRuntimeListeners() {
  // Listen for TOFU warning events from the backend
  window.runtime?.EventsOn?.('xdev:tofu-warning', (data) => {
    if (!data || !data.tool) return;
    dialog.warning({
      title: t('install.tofu_warning_title'),
      content: t('install.tofu_warning_content', { tool: data.tool, version: data.version }),
      positiveText: t('install.tofu_warning_confirm'),
      closable: false,
      maskClosable: false,
    });
  });

  // 第七轮审计 F-41：安装成功但**激活失败**的软告警。
  // Go 侧在 ExtractAndActivate 返回激活类错误时会 emit 'xdev:activation-warning'
  //（internal/gui/app_install.go），但前端从无监听者 —— 事件被静默丢弃，用户看到
  // 的是「Completed」，而工具其实**没有**切到新版本（版本装上了、current 链接没动）。
  // 这里补上监听，明确告知「已安装但未激活」以及如何修复。
  window.runtime?.EventsOn?.('xdev:activation-warning', (data) => {
    if (!data || !data.tool) return;
    message.warning(
      t('install.activation_warning', { tool: data.tool, version: data.version, message: data.message }),
      { duration: 10000 },
    );
    // 激活失败通常意味着 shim/链接状态异常，刷新详情以便用户看到真实状态。
    loadDashboard();
  });

  // Listen for dashboard stats refresh events from the backend
  window.runtime?.EventsOn?.('dashboardStatsUpdated', () => {
    loadDashboard();
  });

  // Spec 034: 启动自检更新事件（ask → 三选对话框；notify/applied/applied-error → 提示）
  window.runtime?.EventsOn?.('self-update', (data) => {
    if (!data) return;
    if (data.action === 'ask') {
      selfUpdateInfo.value = { current: data.current, latest: data.latest };
      showSelfUpdate.value = true;
    } else if (data.action === 'notify') {
      message.info(t('update_available_short', { latest: data.latest }));
    } else if (data.action === 'applied') {
      showSelfUpdate.value = false;
      message.success(t('self_update.applied', { latest: data.latest }));
    } else if (data.action === 'applied-error') {
      showSelfUpdate.value = false;
      message.error(t('update_check_failed'));
    }
  });
}

onMounted(async () => {
  // R8-01：先同步注册（第一个 await 之前），保证「注册先于任何挂起点」；
  // 即使本组件在 bootstrap 未决期间被卸载，onUnmounted 的 EventsOff 也能清干净。
  registerRuntimeListeners();

  // Spec 032: one bootstrap round-trip replaces the categories +
  // dashboard + favorites waterfall.
  try {
    const boot = await window.go.gui.App.GetBootstrapState();
    if (boot?.categories) categories.value = boot.categories;
    if (boot?.stats) stats.value = boot.stats;
    if (boot?.favorites) favorites.value = boot.favorites;
  } catch (e) {
    console.error('Bootstrap failed, falling back to individual loads:', e);
    await loadCategories();
    await loadDashboard();
    await loadFavorites();
  }
  await loadUpdates();
  await loadIgnoredTools();
  await loadSniffConfig();
  await loadSniffServiceStatus();
  await loadMigrationMode();
  // 扫描旧版目录，发现后弹出迁移确认对话框
  scanAndPromptLegacyMigration();
});

const selectedView = ref('dashboard');
const storedTheme = localStorage.getItem('xdev-theme');
const themeName = ref(storedTheme === 'true' ? 'dark' : storedTheme === 'false' ? 'light' : isKnownTheme(storedTheme) ? storedTheme : DEFAULT_THEME);

function applyThemeToDocument() {
  document.documentElement.setAttribute('data-theme', themeName.value);
}

function setTheme(name) {
  if (!isKnownTheme(name)) return;
  themeName.value = name;
  localStorage.setItem('xdev-theme', name);
  applyThemeToDocument();
  // Notify parent (App.vue) to switch Naive UI theme
  window.dispatchEvent(new CustomEvent('xdev-theme-change', { detail: name }));
}
const showCommandPalette = ref(false);
const commandPaletteTrigger = ref(null);
function openCommandPalette() { showCommandPalette.value = true; }
async function setCommandPaletteVisible(visible) {
  showCommandPalette.value = visible;
  if (!visible) {
    await nextTick();
    commandPaletteTrigger.value?.$el?.focus();
  }
}
const categories = ref([]);
const { tools, loading: toolsLoading, error: toolsError, searchQuery, hideUnavailable, activeCategory, filteredTools, loadTools, retry: retryTools } = useTools();
onMounted(() => {
  window.runtime?.EventsOn?.('localCacheUpdated', () => {
    if (activeCategory.value) retryTools();
  });
});
const selectedTool = ref(null);
const showDrawer = ref(false);
const drawerTrigger = ref(null);
// Spec 023: 当前工具详情页的依赖环境检查结果（CheckToolDependencies）。
const toolDeps = ref([]);
const installedVersions = ref([]);
const availableVersions = ref([]);
const showCount = ref(10);
const { install: runInstall, installConfirmed: runInstallConfirmed, repair: runRepair, pause: runPause, resume: runResume, cancel: runCancel, waitForCompletion, phase: installPhase, progress: installProgress, error: installError, active: installing, paused: downloadPaused } = useInstall({ onComplete: handleInstallComplete, onError: handleInstallError, onCancel: handleInstallCancel });
const showProgress = ref(false);
const installTrigger = ref(null);
const showFormSelector = ref(false);
const formSelectorTool = ref(null);
const formSelectorRequest = ref(null);

// ---- Spec 037 阶段 1：并行批量安装 ----
// 每项一个独立 useInstall 会话：事件按 taskId 路由（belongsToCurrentTask），
// 会话状态镜像到行对象供 InstallProgressDialog 多项模式渲染。
// 会话回调不触碰全局 showProgress（批量进度框由 useBatchUpdates 管理）。
function createBatchSession(row) {
  const session = useInstall({
    onComplete: () => { row.active = false; row.paused = false; },
    onError: (data) => { row.error = { message: data?.error || data?.message || String(data) }; row.active = false; },
    onCancel: () => { row.active = false; row.paused = false; },
  });
  // Mirror session reactive state onto the row (dialog renders rows).
  watch(() => session.phase.value, (v) => { row.phase = v; }, { immediate: true });
  watch(() => session.progress.value, (v) => { row.percent = v?.percent || 0; row.bytes = v?.bytes || 0; row.totalBytes = v?.totalBytes || 0; row.speed = v?.speed || ''; row.eta = v?.eta || ''; }, { immediate: true });
  watch(() => session.active.value, (v) => { row.active = v; }, { immediate: true });
  watch(() => session.paused.value, (v) => { row.paused = v; }, { immediate: true });
  watch(() => session.error.value, (v) => { if (v) row.error = v; }, { immediate: true });
  return session;
}
const parallelBatch = useParallelBatch({ sessionFactory: createBatchSession, message, t });
const batchItems = parallelBatch.items;
const batchInstalling = parallelBatch.running;
const batchActiveCount = computed(() => batchItems.filter((r) => r.active).length);
async function cancelBatchItem(row) { await (row.session?.cancel?.() || Promise.resolve()).catch?.(() => {}); }
async function cancelAllBatch() { await parallelBatch.cancelAll(); }

// Dashboard state
const stats = ref({});
const dashboardLoading = ref(false);
const dashboardError = ref(null);
const installSizeEntries = ref([]);
const cacheSizeEntries = ref([]);
const installSizeLoading = ref(false);
const cacheSizeLoading = ref(false);
const dashVersionExpanded = ref('');
const dashInstalledVers = ref([]);
const dashAvailableVers = ref([]);

// Settings state
const mirrorConfig = ref({});
const mirrorTestResults = ref({});
const mirrorRequests = createLatestRequestController();
const updateInfo = ref(null);
const diagResults = ref([]);
const cacheBreakdown = ref({});
const archiveRetention = ref('immediate');

// Custom tools state
const customTools = ref([]);
const customToolSubmitting = ref(false);
const customToolPreview = ref([]);

// 代理设置状态
const proxyEnabled = ref(false);
const proxyHTTP = ref('');
const proxyHTTPS = ref('');
const proxyNoProxy = ref('');
const proxyTestResult = ref(null);
const proxyProbes = ref([]);
const proxyRequests = createLatestRequestController();
const proxyModel = computed(() => ({ enabled: proxyEnabled.value, http: proxyHTTP.value, https: proxyHTTPS.value, noProxy: proxyNoProxy.value }));
function updateProxy(value) { proxyEnabled.value = value.enabled; proxyHTTP.value = value.http; proxyHTTPS.value = value.https; proxyNoProxy.value = value.noProxy; }
function updateMirror(tool, url) { mirrorConfig.value[tool] = url; delete mirrorTestResults.value[tool]; }
function removeMirror(tool) { delete mirrorConfig.value[tool]; delete mirrorTestResults.value[tool]; }
function addMirrorValues(tool, url) { mirrorConfig.value[tool] = url; }

// 导出/导入状态
const importResult = ref(null);

// Environment variables & version switching state
const editingEnvVar = ref(null);
const showEnvEditor = ref(false);
const envEditorTrigger = ref(null);

// 环境变量视图切换和验证状态
const envAllLevel = ref('user'); // 环境变量级别
const allEnvVars = ref([]); // 全部环境变量
const envValidationStats = ref(null); // { valid: number, partial: number, invalid: number }

// 旧版目录迁移状态
const migratingLegacy = ref(false);
const legacyMigrateProgress = ref('');

// 环境迁移状态
const migrationTools = ref([]);
const migrationResults = ref([]);
const migratingAll = ref(false);
const migratingSingle = ref('');
const migrationMode = ref('junction');

// About page state
const aboutInfo = ref({});

const scanOperating = ref(false);
const updateOperating = ref(false);
const { notifications, addNotification, clearNotifications, dismissNotification } = useNotifications();
const scanResults = ref([]);
const scanAttempted = ref(false);
const ignoredTools = ref([]);
const {
  availableUpdates, updatesLoading, sniffOperating, updateInstalling, sniffConfig, serviceStatus,
  loadUpdates, loadSniffConfig, loadSniffServiceStatus, sniffAll, installUpdate,
  saveSniffStrategy, installSniffService, uninstallSniffService,
} = useSniffSession({ t, message, addNotification, doInstall: (...args) => doInstall(...args) });

// 一键更新全部（Spec 035 F2b）：批量循环提取到 useBatchUpdates（依赖
// 全注入可测）。逐项确认 unverified（FR-018）、事件驱动等待
// （waitForCompletion）、取消中止整批、真实汇总文案。
// Spec 036：active 供安装互斥守卫（N2）；batchActive 供完成/取消回调
// 跳过 showProgress 复位（N4 防闪烁）与取消单文案（N5）。
const batchActive = ref(false);
const { installAllUpdates } = useBatchUpdates({
  previewInstall: (tool, version) => window.go.gui.App.PreviewInstall(tool, version),
  // Spec 037 阶段 1：确认后的清单交 parallelBatch 并行执行（每项独立会话）。
  parallelBatch,
  dialog,
  message,
  t,
  loadUpdates,
  availableUpdates,
  setShowProgress: (v) => { showProgress.value = v; },
  active: installing,
  batchActive,
});
const { pmOperating, installPackageManager, updatePackageManager, migratePackageManager } = usePackageManagers({
  t, message, dialog, refreshTools: (...args) => refreshTools(...args), selectedTool,
});


async function loadIgnoredTools() {
  try { ignoredTools.value = await window.go.gui.App.GetIgnoredTools() || []; } catch (e) { console.error("Failed to load ignored tools:", e); }
}

async function ignoreUpdate(update) {
  try { await window.go.gui.App.IgnoreTool(update.tool, "update", ""); message.success(t("updates.ignored_ok")); await loadUpdates(); await loadIgnoredTools(); } catch (e) { message.error(t("ignore.add_failed") + ": " + e); }
}

// Isolate state
const isolateStatus = ref(null);
const isolateLoading = ref(false);
const isolateError = ref(null);
const isolateSetupLoading = ref(false);
const isolateExecLoading = ref(false);
const isolateExecOutput = ref('');
const isolateTeardownLoading = ref(false);
const isolateOperating = computed(() => isolateSetupLoading.value || isolateExecLoading.value || isolateTeardownLoading.value);

// Remote state
const remoteInstances = ref([]);
const remoteLoading = ref(false);
const remoteError = ref(null);
const remoteCreating = ref(false);
const remoteOperating = ref({});
const remoteOutputs = ref({});

// Favorites state
const favorites = ref([]);
const favoriteNames = computed(() => new Set(favorites.value.map(f => f.name)));
const favVersionExpanded = ref('');
const favInstalledVers = ref([]);
const favAvailableVers = ref([]);

async function loadFavorites() {
  favVersionExpanded.value = '';
  try {
    favorites.value = await window.go.gui.App.GetFavorites();
  } catch (e) {
    console.error('Failed to load favorites:', e);
  }
}

async function addFavorite(toolName) {
  try {
    await window.go.gui.App.AddFavorite(toolName);
    await loadFavorites();
    message.success(t('favorites.added'));
  } catch (e) {
    message.error(t('favorites.add_failed') + ': ' + e);
  }
}

async function removeFavorite(toolName) {
  try {
    await window.go.gui.App.RemoveFavorite(toolName);
    await loadFavorites();
    message.success(t('favorites.removed'));
  } catch (e) {
    message.error(t('favorites.remove_failed') + ': ' + e);
  }
}

async function toggleFavorite(toolName) {
  try {
    const isFav = await window.go.gui.App.IsFavorite(toolName);
    if (isFav) {
      await removeFavorite(toolName);
    } else {
      await addFavorite(toolName);
    }
    // 更新当前选中工具的收藏状态
    if (selectedTool.value && selectedTool.value.name === toolName) {
      selectedTool.value = { ...selectedTool.value, isFavorite: !isFav };
    }
  } catch (e) {
    message.error(t('favorites.query_failed') + ': ' + e);
  }
}

// ── Favorites version management ──

async function openFavVersionManager(fav, force = false) {
  if (!force && favVersionExpanded.value === fav.name) {
    favVersionExpanded.value = '';
    return;
  }
  favVersionExpanded.value = fav.name;
  try {
    const [installed, available] = await Promise.all([
      window.go.gui.App.GetInstalledVersions(fav.name),
      window.go.gui.App.GetAvailableVersions(fav.name),
    ]);
    // 票 25 (P1-22) 竞态守卫：慢响应期间用户可能已展开另一个收藏项，
    // 无条件落盘会用旧条目覆盖新条目的版本列表（交错污染）。守卫失败
    // 静默丢弃（对照本文件 toolDeps 的既有守卫写法）。
    if (favVersionExpanded.value !== fav.name) {
      console.debug('[AppContent] discarding stale fav versions response for', fav.name);
      return;
    }
    favInstalledVers.value = installed || [];
    favAvailableVers.value = available || [];
  } catch (e) {
    console.error('Failed to load fav versions:', e);
    // 同上：失败清零也必须落在当前展开的条目上。
    if (favVersionExpanded.value !== fav.name) return;
    favInstalledVers.value = [];
    favAvailableVers.value = [];
  }
}

async function openDashVersionManager(tool, force = false) {
  if (!force && dashVersionExpanded.value === tool.name) {
    dashVersionExpanded.value = '';
    return;
  }
  dashVersionExpanded.value = tool.name;
  try {
    const [installed, available] = await Promise.all([
      window.go.gui.App.GetInstalledVersions(tool.name),
      window.go.gui.App.GetAvailableVersions(tool.name),
    ]);
    // 票 25 (P1-22) 竞态守卫：同 openFavVersionManager，比对仪表盘当前
    // 展开的工具名，避免旧响应覆盖新工具的版本列表。
    if (dashVersionExpanded.value !== tool.name) {
      console.debug('[AppContent] discarding stale dashboard versions response for', tool.name);
      return;
    }
    dashInstalledVers.value = installed || [];
    dashAvailableVers.value = available || [];
  } catch (e) {
    console.error('Failed to load dashboard versions:', e);
    if (dashVersionExpanded.value !== tool.name) return;
    dashInstalledVers.value = [];
    dashAvailableVers.value = [];
  }
}

async function installDashVersion(toolName, version) {
  await doInstall(toolName, version);
  await loadDashboard();
}

async function repairDashVersion(toolName, version) {
  await repairInstall(toolName, version);
  await loadDashboard();
}

async function installFavLatest(fav) {
  try {
    const versions = await window.go.gui.App.GetAvailableVersions(fav.name);
    if (versions && versions.length > 0) {
      await doInstall(fav.name, versions[0].version);
    } else {
      message.warning(t('favorites.no_available_version'));
    }
  } catch (e) {
    message.error(t('favorites.install_failed') + ': ' + e);
  }
}

async function openDetailFromFav(fav) {
  // 构造一个 ToolInfo 对象来打开详情抽屉
  const toolInfo = {
    name: fav.name,
    displayName: fav.displayName,
    category: fav.category,
    description: '',
    defaultVersion: '',
    activeVersion: fav.activeVersion || '',
    installed: fav.installed || false,
    localVersion: fav.localVersion || '',
    localPath: fav.localPath || '',
    canInstall: true,
    source: fav.source || '',
    isFavorite: true,
    formFactor: fav.formFactor || [],
  };
  await openDetail(toolInfo);
  // 切换到收藏页面时刷新收藏列表
  await loadFavorites();
}

// ── Isolate ──
async function loadIsolateStatus() {
  isolateLoading.value = true;
  isolateError.value = null;
  try {
    isolateStatus.value = await window.go.gui.App.GetIsolateStatus();
  } catch (e) {
    isolateError.value = e;
    console.error('Failed to load isolate status:', e);
    message.error(t('isolate.load_failed') + ': ' + e);
  } finally {
    isolateLoading.value = false;
  }
}

async function isolateSetup(mode) {
  if (isolateOperating.value) return;
  isolateSetupLoading.value = true;
  try {
    const result = await window.go.gui.App.IsolateSetup(mode);
    message.success(result);
    await loadIsolateStatus();
  } catch (e) {
    message.error(t('isolate.setup_failed') + ': ' + e);
  } finally {
    isolateSetupLoading.value = false;
  }
}

async function isolateExec(command) {
  if (!command || isolateOperating.value) return;
  isolateExecLoading.value = true;
  isolateExecOutput.value = '';
  try {
    const cmdParts = command.split(/\s+/);
    isolateExecOutput.value = await window.go.gui.App.IsolateExec(cmdParts);
  } catch (e) {
    isolateExecOutput.value = t('isolate.exec_failed') + ': ' + e;
  } finally {
    isolateExecLoading.value = false;
  }
}

async function isolateTeardown() {
  if (isolateOperating.value) return;
  isolateTeardownLoading.value = true;
  try {
    await window.go.gui.App.IsolateTeardown();
    message.success(t('isolate.teardown_success'));
    await loadIsolateStatus();
  } catch (e) {
    message.error(t('isolate.teardown_failed') + ': ' + e);
  } finally {
    isolateTeardownLoading.value = false;
  }
}

// ── Remote ──
async function loadRemoteInstances() {
  remoteLoading.value = true;
  remoteError.value = null;
  try {
    remoteInstances.value = await window.go.gui.App.ListRemoteInstances();
  } catch (e) {
    remoteError.value = e;
    console.error('Failed to load remote instances:', e);
  } finally {
    remoteLoading.value = false;
  }
}

async function createRemoteInstance(config) {
  if (remoteCreating.value) return;
  remoteCreating.value = true;
  try {
    const result = await window.go.gui.App.CreateRemoteInstance(
      config.adapter, config.image, config.sshHost, config.sshPort, config.sshUser, config.sshKeyPath, config.sshPassword || '',
    );
    message.success(result.message);
    await loadRemoteInstances();
  } catch (e) {
    message.error(t('remote.create_failed') + ': ' + e);
  } finally {
    remoteCreating.value = false;
  }
}

async function browseRemoteKey() {
  try {
    return await window.go.gui.App.BrowseFile(t('remote.select_key'), '*.pem;*.key;*.id_rsa;*.id_ed25519;*.id_ecdsa;*.id_dsa');
  } catch (e) {
    console.error('Browse key failed:', e);
    return '';
  }
}

async function testSSHConnection(host, port, user, keyPath, password) {
  try {
    return await window.go.gui.App.TestSSHConnection(host, port, user, keyPath, password);
  } catch (e) {
    return { success: false, message: String(e) };
  }
}

async function destroyRemoteInstance(adapter, id) {
  if (remoteOperating.value[id]?.destroy) return;
  remoteOperating.value = { ...remoteOperating.value, [id]: { ...remoteOperating.value[id], destroy: true } };
  try {
    await window.go.gui.App.DestroyRemoteInstance(adapter, id);
    message.success(t('remote.destroyed'));
    await loadRemoteInstances();
  } catch (e) {
    message.error(t('remote.destroy_failed') + ': ' + e);
  } finally { remoteOperating.value = { ...remoteOperating.value, [id]: { ...remoteOperating.value[id], destroy: false } }; }
}

async function copyRemoteAddress(address) {
  try {
    await navigator.clipboard.writeText(address);
    message.success(t('remote.address_copied'));
  } catch (error) {
    message.error(t('remote.copy_address_failed') + ': ' + error);
  }
}

async function execRemoteInstance(adapter, id, command) {
  if (!id || !command || remoteOperating.value[id]?.exec) return;
  remoteOperating.value = { ...remoteOperating.value, [id]: { ...remoteOperating.value[id], exec: true } };
  remoteOutputs.value = { ...remoteOutputs.value, [id]: '' };
  try {
    const cmdParts = command.split(/\s+/);
    remoteOutputs.value = { ...remoteOutputs.value, [id]: await window.go.gui.App.ExecRemoteInstance(adapter, id, cmdParts) };
  } catch (e) {
    remoteOutputs.value = { ...remoteOutputs.value, [id]: t('remote.exec_failed') + ': ' + e };
  } finally {
    remoteOperating.value = { ...remoteOperating.value, [id]: { ...remoteOperating.value[id], exec: false } };
  }
}


const categoryIcons = {
  language: CodeOutline,
  mobile: PhonePortraitOutline,
  build: ConstructOutline,
  system: SettingsOutline,
  local: DesktopOutline,
  'package-manager': RocketOutline,
};

const pathMode = ref('shim');

async function switchPathMode(mode) {
  try {
    await window.go.gui.App.SetPathMode(mode);
    pathMode.value = mode;
    message.success(t('settings.path_mode_switched'));
  } catch (e) {
    console.error('Failed to switch path mode:', e);
    message.error(t('settings.path_mode_switch_failed') + ': ' + e);
  }
}

async function loadMigrationMode() {
  try {
    migrationMode.value = await window.go.gui.App.GetMigrationMode() || 'junction';
  } catch (e) {
    console.error('Failed to load migration mode:', e);
    migrationMode.value = 'junction';
  }
}

async function switchMigrationMode(mode) {
  try {
    await window.go.gui.App.SetMigrationMode(mode);
    migrationMode.value = mode;
    message.success(t('settings.migration_mode_switched'));
  } catch (e) {
    console.error('Failed to switch migration mode:', e);
    message.error(t('settings.migration_mode_switch_failed') + ': ' + e);
  }
}

const dynamicToolPages = computed(() => categories.value.map((category) => {
  const labelKey = `category.${category.name}`;
  const translated = t(labelKey);
  return {
    key: category.name,
    labelKey,
    label: translated === labelKey ? category.name : translated,
    icon: categoryIcons[category.name] || CodeOutline,
    searchable: true,
  };
}));
const mergedNavigationGroups = computed(() => createNavigationGroups(dynamicToolPages.value));
const localizedNavigationGroups = computed(() => mergedNavigationGroups.value.map((group) => ({
  ...group,
  label: t(group.labelKey),
  items: group.items.map((page) => ({ ...page, label: page.label ?? t(page.labelKey) })),
})));
const currentPage = computed(() => findPageDefinition(selectedView.value, localizedNavigationGroups.value));
const headerTitle = computed(() => currentPage.value?.label ?? selectedView.value);
const commandPaletteOptions = computed(() => [
  ...commandPaletteEntries(localizedNavigationGroups.value, t),
  // Spec 040: quick theme switching from the palette (Ctrl+K).
  ...THEMES.map((theme) => ({
    key: `theme:${theme.value}`,
    label: t(theme.labelKey),
    group: t('settings.theme'),
  })),
]);

async function onCommandSelect(key) {
  if (key.startsWith('theme:')) {
    const name = key.slice('theme:'.length);
    if (isKnownTheme(name)) setTheme(name);
    return;
  }
  await onMenuSelect(key);
}

async function onMenuSelect(key) {
  if ((selectedView.value === 'settings' || selectedView.value === 'env-vars') && key !== 'settings' && key !== 'env-vars') {
    editingEnvVar.value = null;
    showEnvEditor.value = false;




    allEnvVars.value = [];
    envValidationStats.value = null;
  }
  selectedView.value = key;
  if (key === 'dashboard') {
    await loadDashboard();
  } else if (key === 'favorites') {
    await loadFavorites();
  } else if (key === 'settings') {
    await loadSettings();
    await loadCustomTools();
    await loadSniffConfig();
    await loadSniffServiceStatus();
  } else if (key === 'env-vars') {
    await loadAllEnvVars();
  } else if (key === 'about') {
    await loadAboutInfo();
  } else if (key === 'isolate') {
    await loadIsolateStatus();
  } else if (key === 'remote') {
    await loadRemoteInstances();
  } else {
    await onCategorySelect(key);
  }
}

async function onCategorySelect(key) {
  await loadTools(key);
}

async function loadDashboard() {
  dashboardLoading.value = true;
  dashboardError.value = null;
  let loaded = false;
  try {
    stats.value = await window.go.gui.App.GetDashboardStats();
    loaded = true;
  } catch (e) {
    dashboardError.value = e;
    console.error('Failed to load dashboard:', e);
  } finally {
    dashboardLoading.value = false;
  }
  return loaded;
}

async function showInstallSize() {
  installSizeLoading.value = true;
  try {
    installSizeEntries.value = await window.go.gui.App.GetInstallSizeBreakdown() || [];
  } catch (e) {
    message.error(t('dashboard.size_load_failed') + ': ' + e);
  } finally {
    installSizeLoading.value = false;
  }
}

async function showCacheSize() {
  cacheSizeLoading.value = true;
  try {
    cacheSizeEntries.value = await window.go.gui.App.GetCacheSizeBreakdown() || [];
  } catch (e) {
    message.error(t('dashboard.size_load_failed') + ': ' + e);
  } finally {
    cacheSizeLoading.value = false;
  }
}

async function cleanCacheCategory(category) {
  try {
    await window.go.gui.App.CleanCacheCategory(category);
    message.success(t('cache.clean_success'));
    await showCacheSize();
    await loadDashboard();
  } catch (e) {
    message.error(t('cache.clean_failed') + ': ' + e);
  }
}

async function deleteCacheFile(entry) {
  try {
    await window.go.gui.App.DeleteCacheFile(entry.path);
    message.success(t('cache.clean_success'));
    await showCacheSize();
    await loadDashboard();
  } catch (e) {
    message.error(t('cache.clean_failed') + ': ' + e);
  }
}

async function loadAboutInfo() {
  try {
    aboutInfo.value = await window.go.gui.App.GetAboutInfo();
  } catch (e) {
    console.error('Failed to load about info:', e);
  }
}

async function loadSettings() {
  try {
    mirrorConfig.value = await window.go.gui.App.GetMirrorConfig();
    pathMode.value = await window.go.gui.App.GetPathMode();
    await loadMigrationMode();
    // 加载代理配置
    const proxy = await window.go.gui.App.GetProxyConfig();
    proxyEnabled.value = proxy.enabled || false;
    proxyHTTP.value = proxy.http || '';
    proxyHTTPS.value = proxy.https || '';
    proxyNoProxy.value = proxy.noProxy || '';
    await loadCacheBreakdown();
    try {
      archiveRetention.value = await window.go.gui.App.GetArchiveRetention() || 'immediate';
    } catch (e) {
      console.error('Failed to load archive retention:', e);
    }
  } catch (e) {
    console.error('Failed to load settings:', e);
    message.error(t('settings.load_failed') + ': ' + e);
  }
}

async function openDetail(tool) {
  drawerTrigger.value = document.activeElement;
  selectedTool.value = tool;
  showDrawer.value = true;
  showCount.value = 10;
  // Spec 023: 竞态防护——打开前立即清空旧依赖，避免闪烁显示上一工具的数据。
  toolDeps.value = [];
  await loadVersions(tool.name);
  // Spec 023: 拉取依赖环境检查结果（竞态防护：仅当选中工具未变时才更新）。
  const requestedName = tool.name;
  try {
    const deps = await window.go.gui.App.CheckToolDependencies(requestedName) || [];
    if (selectedTool.value?.name === requestedName) toolDeps.value = deps;
  } catch (e) {
    if (selectedTool.value?.name === requestedName) toolDeps.value = [];
    console.error('Failed to load tool dependencies:', e);
  }
}

// Spec 023: 启动工具的 GUI 主程序（仅 form_factor 含 gui 时可用）。
// pending guard 防止连续点击启动多个进程。
const launchPending = ref(false);
async function launchTool(name) {
  if (launchPending.value) return;
  launchPending.value = true;
  try {
    await window.go.gui.App.LaunchTool(name);
    message.success(t('detail.launched', { name }));
  } catch (e) {
    message.error(t('detail.launch_failed', { name }) + ': ' + e);
  } finally {
    launchPending.value = false;
  }
}

// Spec 023: 复制工具的 CLI 启动命令（取首个 shim）。
const copyPending = ref(false);
async function copyToolCommand(name) {
  if (copyPending.value) return;
  copyPending.value = true;
  try {
    const cmd = await window.go.gui.App.ToolCommand(name);
    if (!cmd) {
      message.warning(t('detail.no_command'));
      return;
    }
    await navigator.clipboard.writeText(cmd);
    message.success(t('detail.copied'));
  } catch (e) {
    message.error(t('detail.copy_failed') + ': ' + e);
  } finally {
    copyPending.value = false;
  }
}

async function toggleCatalogFavorite(tool) {
  if (!tool?.name) return;
  if (tool.isFavorite) await removeFavorite(tool.name);
  else await addFavorite(tool.name);
  await retryTools();
}

async function loadVersions(toolName) {
  try {
    const [installed, available] = await Promise.all([
      window.go.gui.App.GetInstalledVersions(toolName),
      window.go.gui.App.GetAvailableVersions(toolName),
    ]);
    // 票 25 (P1-22) 竞态守卫：两个入口是异步的，慢响应期间用户可能已切换
    // 选中工具；此时无条件落盘会把旧工具的版本列表写进抽屉，造成交错污染
    // （对照 openDetail 里 toolDeps 的同型守卫）。
    if (selectedTool.value?.name !== toolName) {
      console.debug('[AppContent] discarding stale versions response for', toolName);
      return;
    }
    installedVersions.value = installed || [];
    availableVersions.value = available || [];
  } catch (e) {
    console.error('Failed to load versions:', e);
  }
}

// showInstallError（Spec 036 冒烟反馈 #3）：安装失败的错误 toast——
// ① 存留时间延长到 10s（Naive UI 默认 3s，来不及看清/排查）；
// ② 内嵌「复制错误」一键复制完整错误文本（与进度框 error alert 的
// 复制按钮同语义；clipboard 不可用时静默降级为普通长时 toast）。
function showInstallError(text) {
  if (!text) return;
  const copyErrorText = async () => {
    try { await navigator.clipboard.writeText(text); } catch { /* clipboard unavailable */ }
  };
  message.error(() => h('span', null, [
    text,
    h('a', {
      style: 'margin-left:10px;cursor:pointer;text-decoration:underline;white-space:nowrap;',
      onClick: copyErrorText,
    }, t('install.copy_error')),
  ]), { duration: 10000 });
}

function mergeInstallTool(toolName, fallback = null) {
  const current = selectedTool.value?.name === toolName ? selectedTool.value : null;
  const local = tools.value.find(item => item.name === toolName || item.toolName === toolName);
  return { ...(fallback || {}), ...(current || {}), ...(local || {}) };
}

async function doInstall(toolName, version, tool = null, formFilter = '') {
  if (!version) return;
  // Spec 036 N2 补遗：此处原为静默早退——安装进行中点击任何安装入口
  // （更新列表行/徽标弹层/抽屉/版本管理器）无任何反馈。改为 busy 提示。
  if (installing.value || batchActive.value) { message.warning(t('install.busy')); return; }
  tool = mergeInstallTool(toolName, tool);
  installTrigger.value = showDrawer.value ? drawerTrigger.value : document.activeElement;

  let preview = null;
  try {
    preview = await window.go.gui.App.PreviewInstall(toolName, version);
  } catch (e) {
    // Older builds may lack PreviewInstall — fall back to local metadata.
  }
  if (preview) {
    tool = {
      ...tool,
      hasFormSpecificArchives: preview.hasFormSpecificArchives,
      availableForms: preview.availableForms || tool.availableForms,
    };
  }

  const formChoices = (Array.isArray(tool?.availableForms) && tool.availableForms.length)
    ? tool.availableForms
    : (Array.isArray(tool?.formFactor) ? tool.formFactor : []);
  if (!formFilter && tool?.hasFormSpecificArchives && formChoices.length === 1) {
    formFilter = formChoices[0];
  }
  if (!formFilter && tool?.hasFormSpecificArchives && formChoices.length > 1) {
    showDrawer.value = false;
    formSelectorTool.value = tool;
    formSelectorRequest.value = { toolName, version, tool };
    showFormSelector.value = true;
    return;
  }
  showDrawer.value = false;

  const integrity = preview?.integrity;
  if (integrity && integrity.path && !integrity.ok && integrity.reason !== 'not installed') {
    const missing = (integrity.missing || []).length ? ` (${(integrity.missing || []).join(', ')})` : '';
    dialog.warning({
      title: t('confirm_repair_title'),
      content: `${t('confirm_repair_content', { tool: toolName, version })}${missing}`,
      positiveText: t('detail.repair'),
      negativeText: t('btn.cancel'),
      onPositiveClick: () => startRepairConfirmed(toolName, version),
    });
    return;
  }

  if (preview?.unverified) {
    dialog.warning({
      title: t('install.unverified_title') || 'Unverified Version',
      content: t('install.unverified_content', { tool: toolName, version }) || `Version ${toolName}@${version} was discovered by sniffing. The archive URL has not been verified — downloads may fail. Continue?`,
      positiveText: t('btn.install'),
      negativeText: t('btn.cancel'),
      onPositiveClick: () => startInstallConfirmed(toolName, version, true, formFilter),
    });
    return;
  }
  startInstallConfirmed(toolName, version, false, formFilter);
}

function confirmFormInstall(formFilter) {
  const request = formSelectorRequest.value;
  showFormSelector.value = false;
  formSelectorTool.value = null;
  formSelectorRequest.value = null;
  if (request) doInstall(request.toolName, request.version, request.tool, formFilter);
}

async function startInstallConfirmed(toolName, version, confirmed, formFilter = '') {
  // Spec 036 N2: install mutex — every secondary install entry (row
  // buttons, update badge popover, drawer, version manager, form
  // selector, unverified confirm) converges here. The batch adapter
  // never trips this: its loop waits for active=false between items.
  if (installing.value || batchActive.value) {
    message.warning(t('install.busy'));
    return;
  }
  showProgress.value = true;
  const started = confirmed
    ? await runInstallConfirmed(toolName, version, formFilter)
    : await runInstall(toolName, version, formFilter);
  if (!started && installError.value) showInstallError(t('install.failed', { error: installError.value.message }));
  // 冒烟反馈 #2 根因修复：必须把 started 布尔值返回给调用方——批量更新
  // 适配器据 `if (!started)` 区分「启动失败」与「已启动（等待事件）」。
  // 此前 void 返回 undefined 被当失败计，且 active 仍 true 导致下一项
  // 预检 break——批量在首个成功启动的项之后即中止（成功0 失败1）。
  return started;
}

// repairInstall force-reinstalls an installed (possibly incomplete) version.
// Confirmation uses Naive UI so it is not hidden behind the Wails window.
async function repairInstall(toolName, version) {
  if (!version) return;
  // Spec 036 N2 补遗：同 doInstall——静默早退改 busy 提示。
  if (installing.value || batchActive.value) { message.warning(t('install.busy')); return; }
  installTrigger.value = showDrawer.value ? drawerTrigger.value : document.activeElement;
  showDrawer.value = false;
  dialog.warning({
    title: t('confirm_repair_title'),
    content: t('confirm_repair_content', { tool: toolName, version }),
    positiveText: t('detail.repair'),
    negativeText: t('btn.cancel'),
    onPositiveClick: () => startRepairConfirmed(toolName, version),
  });
}

async function startRepairConfirmed(toolName, version) {
  // Spec 036 N2: install mutex (converges both repair entries —
  // repairInstall dialog and doInstall integrity-check dialog).
  if (installing.value) {
    message.warning(t('install.busy'));
    return;
  }
  showProgress.value = true;
  const started = await runRepair(toolName, version);
  if (!started && installError.value) showInstallError(t('install.failed', { error: installError.value.message }));
  return started; // 冒烟反馈 #2：与 startInstallConfirmed 同契约（调用方需要布尔返回值）
}

async function refreshInstallViews(toolName) {
  const refreshExpandedFavorite = favVersionExpanded.value === toolName;
  const refreshExpandedDashboard = dashVersionExpanded.value === toolName;
  await Promise.all([loadVersions(toolName), refreshTools(), loadDashboard(), loadFavorites(), loadUpdates()]);
  if (refreshExpandedFavorite) await openFavVersionManager({ name: toolName }, true);
  if (refreshExpandedDashboard) await openDashVersionManager({ name: toolName }, true);
}

async function handleInstallComplete(data) {
  // Spec 036 N4: during a batch the loop owns the progress dialog —
  // resetting here would flicker it closed between items.
  if (!batchActive.value) showProgress.value = false;
  message.success(t('install.success', { tool: data.tool, version: data.version }));
  await refreshInstallViews(data.tool);
}

async function handleInstallError(data) {
  showInstallError(t('install.failed', { error: data.error || data.message }));
  await refreshInstallViews(data.tool || selectedTool.value?.name || '');
}

async function handleInstallCancel(data) {
  await refreshInstallViews(data.tool);
  await nextTick();
  installTrigger.value = document.querySelector(`[data-action="detail"][data-tool="${CSS.escape(data.tool)}"]`) || installTrigger.value;
  // Spec 036 N4: during a batch the loop owns the progress dialog.
  if (!batchActive.value) showProgress.value = false;
}

async function pauseDownload() {
  if (await runPause()) message.info(t('download_paused'));
}

async function resumeDownload() {
  if (await runResume()) message.info(t('download_resumed'));
}

async function cancelDownload() {
  // Spec 036 N5: during a batch the summary toast already reports the
  // cancellation — skip the per-item toast to avoid double messaging.
  if (await runCancel() && !batchActive.value) message.warning(t('download_cancelled'));
}

async function useVersion(toolName, version) {
  try {
    await window.go.gui.App.UseVersion(toolName, version);
    message.success(t('detail.switched_version', { name: toolName, version }));
    await loadVersions(toolName);
    await refreshTools();
    await loadDashboard();
  } catch (e) {
    message.error(t('detail.switch_failed') + ': ' + e);
  }
}

function confirmUninstall(toolName, version) {
  dialog.warning({
    title: t('confirm_uninstall_title'),
    content: t('confirm_uninstall_content', { tool: toolName, version }),
    positiveText: t('btn.remove'),
    negativeText: t('btn.cancel'),
    onPositiveClick: async () => {
      try {
        await window.go.gui.App.UninstallVersion(toolName, version);
        message.success(t('detail.uninstall_success', { name: toolName, version }));
        await loadVersions(toolName);
        await refreshTools();
        await loadDashboard();
      } catch (e) {
        message.error(t('detail.uninstall_failed') + ': ' + e);
      }
    },
  });
}

async function scanLocal() {
  scanOperating.value = true;
  try {
    const localTools = await window.go.gui.App.ScanLocalEnvironments();
    scanResults.value = localTools || [];
    scanAttempted.value = true;
    if (localTools && localTools.length > 0) {
      message.info(t('dashboard.local_found', { count: localTools.length }));
    } else {
      message.info(t('dashboard.local_none'));
    }
    await refreshTools();
  } catch (e) {
    console.error('Scan failed:', e);
    message.error(t('dashboard.scan_failed'));
  } finally {
    scanOperating.value = false;
  }
}


async function ignoreScanTool(item) {
  try { await window.go.gui.App.IgnoreTool(item.name, "local-scan", item.localPath || item.path || ""); scanResults.value = scanResults.value.filter(tool => tool !== item); await loadIgnoredTools(); } catch (e) { message.error(t("ignore.add_failed") + ": " + e); }
}

async function migrateScanTool(item) {
  try {
    await migrateTool(item.name, item.localPath || item.path || "");
    scanResults.value = await window.go.gui.App.ScanLocalEnvironments() || [];
    await loadDashboard();
  } catch (e) {
    message.error(t('dashboard.scan_failed') + ': ' + e);
  }
}

async function unignoreTool(item) {
  try {
    await window.go.gui.App.UnignoreTool(item.name, item.kind, item.path || "");
    await loadIgnoredTools();
    await loadUpdates();
    if (item.kind === "local-scan" && scanAttempted.value) {
      scanResults.value = await window.go.gui.App.ScanLocalEnvironments() || [];
    }
  } catch (e) {
    message.error(t("ignore.remove_failed") + ": " + e);
  }
}
async function checkUpdate() {
  updateOperating.value = true;
  try {
    updateInfo.value = await window.go.gui.App.CheckForUpdate();
    if (updateInfo.value && updateInfo.value.hasUpdate) {
      message.info(t('update_available_short', { latest: updateInfo.value.latest }));
    } else if (updateInfo.value && !updateInfo.value.error) {
      message.success(t('up_to_date'));
    }
  } catch (e) {
    message.error(t('update_check_failed') + ': ' + e);
  } finally {
    updateOperating.value = false;
  }
}

// Spec 034: 启动自检三选对话框状态与动作
const showSelfUpdate = ref(false);
const selfUpdateInfo = ref({ current: '', latest: '' });
const selfUpdating = ref(false);

async function applySelfUpdate() {
  selfUpdating.value = true;
  try {
    const res = await window.go.gui.App.ApplyUpdate();
    if (res?.error) {
      message.error(res.error);
    } else {
      showSelfUpdate.value = false;
      message.success(t('self_update.applied', { latest: res.latest }));
    }
  } catch (e) {
    message.error(t('update_check_failed') + ': ' + e);
  } finally {
    selfUpdating.value = false;
  }
}

async function ignoreSelfUpdate() {
  showSelfUpdate.value = false;
  message.info(t('self_update.ignored', { latest: selfUpdateInfo.value.latest }));
  try {
    await window.go.gui.App.IgnoreSelfUpdate(selfUpdateInfo.value.latest);
  } catch (e) {
    console.error('IgnoreSelfUpdate failed:', e);
  }
}

function quitSelfUpdate() {
  showSelfUpdate.value = false;
  window.runtime?.WindowClose?.();
}

async function saveMirrors() {
  try {
    await window.go.gui.App.SetMirrorConfig(mirrorConfig.value);
    message.success(t('settings.mirror_saved'));
  } catch (e) {
    message.error(t('btn.save_failed') + ': ' + e);
  }
}

async function testMirror(tool, url) {
  if (!url) return;
  const outcome = await mirrorRequests.run(tool, { url }, snapshot => window.go.gui.App.TestMirrorConnectivity(snapshot.url));
  if (!outcome.current || mirrorConfig.value[tool] !== outcome.input.url) return;
  if (outcome.error) {
    mirrorTestResults.value[tool] = { success: false, message: String(outcome.error), url: outcome.input.url, lastChecked: new Date().toISOString() };
    message.error(`${tool} ${t('mirror.test_failed')}: ${outcome.error}`);
  } else {
    const result = outcome.value;
    mirrorTestResults.value[tool] = { ...result, url: outcome.input.url, lastChecked: new Date().toISOString() };
    if (result.success) {
      message.success(`${tool}: ${t('mirror.test_connected')} (${result.latency}ms)`);
    } else {
      message.error(`${tool}: ${result.message}`);
    }
  }
}

async function saveProxy() {
  try {
    await window.go.gui.App.SetProxyConfig(proxyHTTP.value, proxyHTTPS.value, proxyNoProxy.value, proxyEnabled.value);
    message.success(t('settings.proxy_saved'));
  } catch (e) {
    message.error(t('settings.proxy_save_failed') + ': ' + e);
  }
}

async function testProxy() {
  if (!proxyHTTP.value && !proxyHTTPS.value) {
    message.warning(t('settings.proxy_no_url'));
    return;
  }
  proxyTestResult.value = null;
  const input = { url: proxyHTTPS.value || proxyHTTP.value, noProxy: proxyNoProxy.value };
  const outcome = await proxyRequests.run('proxy', input, snapshot => window.go.gui.App.TestProxy(snapshot.url, 'https://api.github.com'));
  if (!outcome.current) return;
  proxyTestResult.value = outcome.error ? { success: false, message: String(outcome.error), input: outcome.input } : { ...outcome.value, input: outcome.input };
}

// probeLocalProxies scans common local proxy ports (Clash Verge mixed-port
// 7897 etc.) and lists the ones that actually proxy HTTPS.
async function probeLocalProxies() {
  proxyProbes.value = [];
  try {
    proxyProbes.value = (await window.go.gui.App.ProbeLocalProxies()) || [];
  } catch (e) {
    console.error('ProbeLocalProxies failed:', e);
    message.error(t('settings.proxy_probe_failed'));
  }
}

// applyProxyProbe fills the HTTP/HTTPS fields with the probed proxy URL.
function applyProxyProbe(url) {
  if (!url) return;
  updateProxy({ enabled: true, http: url, https: url, noProxy: proxyNoProxy.value });
}

async function exportProfile() {
  try {
    const dir = await window.go.gui.App.BrowseDirectory(t('settings.profile_export_dir'));
    if (!dir) {
      return;
    }
    // Separator-aware join: the picked dir already uses the native
    // separator (Windows backslash, Unix forward slash). A hardcoded
    // backslash becomes a literal filename character on Linux/macOS and
    // the profile lands outside the chosen directory.
    const sep = dir.includes('\\') ? '\\' : '/';
    const fullPath = dir + sep + 'xdev-profile.json';
    await window.go.gui.App.ExportProfile(fullPath);
    message.success(t('settings.profile_export_success') + ': ' + fullPath);
  } catch (e) {
    message.error(t('settings.profile_export_failed') + ': ' + e);
  }
}

async function importProfile() {
  importResult.value = null;
  try {
    const dir = await window.go.gui.App.BrowseDirectory(t('settings.profile_select_dir'));
    if (!dir) {
      return;
    }
    const sep = dir.includes('\\') ? '\\' : '/';
    const filePath = dir + sep + 'xdev-profile.json';
    const result = await window.go.gui.App.ImportProfile(filePath);
    importResult.value = result;
    if (result.success) {
      message.success(result.message);
      await refreshTools();
    } else {
      message.error(result.message);
    }
  } catch (e) {
    message.error(t('settings.profile_import_failed') + ': ' + e);
  }
}

async function runDiag() {
  try {
    diagResults.value = await window.go.gui.App.RunDiagnostics();
  } catch (e) {
    message.error(t('settings.diagnostics_failed') + ': ' + e);
  }
}

async function loadCacheBreakdown() {
  try {
    cacheBreakdown.value = await window.go.gui.App.GetCacheBreakdown();
  } catch (e) {
    console.error('Failed to load cache breakdown:', e);
  }
}

// ── Custom tools ──

async function loadCustomTools() {
  try {
    customTools.value = await window.go.gui.App.ListCustomTools() || [];
  } catch (e) {
    console.error('Failed to load custom tools:', e);
  }
}

async function previewCustomToolVersions(formData) {
  customToolSubmitting.value = true;
  customToolPreview.value = [];
  try {
    // F-2（前端复审）：Go 侧契约已改为 {success, message, versions}——
    // 失败不再被 `|| []` 吞成静默空列表，原因直达 toast。
    const result = await window.go.gui.App.PreviewCustomToolVersions(
      formData.sourceURL, formData.sourceType, formData.pattern || '',
    );
    if (result && result.success) {
      customToolPreview.value = result.versions || [];
    } else {
      customToolPreview.value = [];
      message.error(t('custom_tools.preview_failed') + ': ' + ((result && result.message) || ''));
    }
  } catch (e) {
    console.error('Preview failed:', e);
    message.error(t('custom_tools.preview_failed') + ': ' + e);
  } finally {
    customToolSubmitting.value = false;
  }
}

async function submitAddCustomTool(formData) {
  customToolSubmitting.value = true;
  try {
    const result = await window.go.gui.App.AddCustomTool(
      formData.name, formData.displayName || '', formData.sourceURL,
      formData.sourceType, formData.pattern || '', formData.shim || '', formData.envVar || '',
    );
    if (result.success) {
      message.success(t('custom_tools.added'));
      customToolPreview.value = result.versions || [];
      await loadCustomTools();
    } else {
      message.error(t('custom_tools.add_failed') + ': ' + (result.message || ''));
    }
  } catch (e) {
    console.error('Add custom tool failed:', e);
    message.error(t('custom_tools.add_failed') + ': ' + e);
  } finally {
    customToolSubmitting.value = false;
  }
}

async function removeCustomTool(name) {
  try {
    await window.go.gui.App.RemoveCustomTool(name);
    message.success(t('custom_tools.removed'));
    await loadCustomTools();
  } catch (e) {
    console.error('Remove custom tool failed:', e);
    message.error(t('custom_tools.remove_failed') + ': ' + e);
  }
}

async function cleanCache(category) {
  try {
    await window.go.gui.App.CleanCache(category);
    message.success(t('cache.clean_success'));
    await loadCacheBreakdown();
    await loadDashboard();
  } catch (e) {
    message.error(t('cache.clean_failed') + ': ' + e);
  }
}

async function saveArchiveRetention(policy) {
  try {
    await window.go.gui.App.SetArchiveRetention(policy);
    archiveRetention.value = policy;
    message.success(t('settings.retention_saved'));
  } catch (e) {
    message.error(t('settings.save_failed') + ': ' + e);
  }
}

async function migrateTool(toolName, localPath) {
  // R8-02（第八轮审计，P1）：F-40 的重入守卫**只覆盖了 migrateSingle/migrateAll**，
  // 而同一时刻可用的另外两条迁移入口都走本函数：
  //   - DashboardPage 扫描结果行「迁移」→ @scan-migrate → migrateScanTool → migrateTool；
  //   - FavoritesPage / ToolDetailDrawer 的「迁移到 xdev」→ @migrate → migrateTool。
  // 本函数既不读也不写 migratingAll/migratingSingle，Go 侧
  // MigrateLocalTool / migrateLocalToolByJunction / migrateLocalToolByMove 也都不取
  // 工具锁，因此双击 = 两次并发 MigrateLocalTool，对同一目录并发
  // CopyDir/os.Rename/CreateJunction；move 模式下第二个流程还会对已被移走的源路径做
  // os.RemoveAll。这里复用同一对标志（并在 finally 复位）。
  if (migratingAll.value || migratingSingle.value) return;
  migratingSingle.value = toolName;
  try {
    await window.go.gui.App.MigrateLocalTool(toolName, localPath);
    message.success(t('migration.migrated_to_xdev', { name: toolName }));
    await loadVersions(toolName);
    await refreshTools();
  } catch (e) {
    message.error(t('migration.migrate_failed') + ': ' + e);
  } finally {
    migratingSingle.value = '';
  }
}

async function switchToLocal(toolName, localPath = '') {
  try {
    await window.go.gui.App.SwitchToLocalVersion(toolName, localPath || '');
    message.success(t('detail.switched_to_local', { name: toolName }));
    await refreshTools();
  } catch (e) {
    message.error(t('detail.switch_failed') + ': ' + e);
  }
}

async function switchToXdev(toolName, version) {
  try {
    await window.go.gui.App.SwitchToXdevVersion(toolName, version);
    message.success(t('detail.switched_to_xdev', { name: toolName, version }));
    await refreshTools();
  } catch (e) {
    message.error(t('detail.switch_failed') + ': ' + e);
  }
}

// ---- 环境迁移功能 ----

async function scanForMigration() {
  migrationResults.value = [];
  try {
    const tools = await window.go.gui.App.ScanLocalEnvironmentsForMigration();
    migrationTools.value = tools || [];
    if (tools && tools.length > 0) {
      message.info(t('migration.found_count', { count: tools.length }));
    } else {
      message.info(t('migration.no_tools'));
    }
  } catch (e) {
    console.error(t('migration.scan_failed') + ':', e);
    message.error(t('migration.scan_failed') + ': ' + e);
  }
}

async function migrateSingle(tool) {
  // 第七轮审计 F-40：重入守卫。migratingSingle/migratingAll 此前**只被写、从不被读**
  // （模板零次引用），按钮唯一约束是 `:disabled="!migrationTools.length"`，不随进行中
  // 状态禁用。迁移是长耗时复制/移动，快速双击会让同一个工具被两个并发迁移流程同时
  // 处理（Go 侧也无工具锁），造成重复移动 / 半迁移状态。
  if (migratingAll.value || migratingSingle.value) return;
  migratingSingle.value = tool.name;
  try {
    const toolJson = JSON.stringify(tool);
    if (migrationMode.value === 'move') {
      await window.go.gui.App.MigrateLocalToolByMove(toolJson);
    } else {
      await window.go.gui.App.MigrateLocalToolByJunction(toolJson);
    }
    message.success(t('migration.migrated_to_xdev', { name: tool.name }));
    migrationTools.value = migrationTools.value.filter(t => t.name !== tool.name);
    await refreshTools();
  } catch (e) {
    message.error(t('migration.migrate_tool_failed', { name: tool.name }) + ': ' + e);
  } finally {
    migratingSingle.value = '';
  }
}

async function migrateAll() {
  // 第七轮审计 F-40：同上，批量迁移也必须防重入（否则两轮循环会并发处理同一批工具）。
  if (migratingAll.value || migratingSingle.value) return;
  const mode = migrationMode.value;
  migratingAll.value = true;
  migrationResults.value = [];
  try {
    const results = [];
    for (const tool of [...migrationTools.value]) {
      try {
        const payload = JSON.stringify(tool);
        if (mode === 'move') await window.go.gui.App.MigrateLocalToolByMove(payload);
        else await window.go.gui.App.MigrateLocalToolByJunction(payload);
        results.push({ name: tool.name, status: 'success', version: tool.version, path: tool.path });
      } catch (error) { results.push({ name: tool.name, status: 'failed', error: String(error) }); }
    }
    migrationResults.value = results || [];
    // 从列表中移除已成功迁移的工具
    const successNames = (results || []).filter(r => r.status === 'success').map(r => r.name);
    migrationTools.value = migrationTools.value.filter(t => !successNames.includes(t.name));
    const successCount = successNames.length;
    const failCount = (results || []).filter(r => r.status === 'failed').length;
    if (successCount > 0) {
      message.success(t('migration.migrate_all_success', { count: successCount }) + (failCount > 0 ? `，${failCount} ${t('migration.status_failed')}` : ''));
    } else {
      message.warning(t('migration.no_tools_migrated'));
    }
    await refreshTools();
  } catch (e) {
    message.error(t('migration.migrate_all_failed') + ': ' + e);
  } finally {
    migratingAll.value = false;
  }
}

// 加载全部环境变量
async function loadAllEnvVars() {
  try {
    allEnvVars.value = await window.go.gui.App.GetAllEnvVars(envAllLevel.value || 'user');
  } catch (e) {
    console.error('Failed to load all env vars:', e);
    message.error(t('env.load_all_failed') + ': ' + e);
  }
}

// 视图级别切换
async function setEnvAllLevel(level) { envAllLevel.value = level; await loadAllEnvVars(); }
async function refreshCurrentEnvVars() { await loadAllEnvVars(); }
async function saveEnvDraft(draft) { await setEnvVar(draft.toolName, draft.name, draft.value, draft.level, draft.original); }
async function deleteEnvRow(row) { await deleteEnvVar(row.name, row.level); if (sameEnvIdentity(editingEnvVar.value, row)) cancelEditEnvVar(); }
async function cleanEnvRow(row) { await cleanEnvVar(row.name, row.level); }

// 检测所有环境变量的有效性
async function validateAllEnvVars() {
  try {
    // 重新拉取最新数据，保证统计新鲜
    await loadAllEnvVars();
    const vars = allEnvVars.value || [];
    let valid = 0;
    let partial = 0;
    let invalid = 0;
    const issues = [];
    for (const v of vars) {
      const status = v.validationStatus || (v.isValid ? 'valid' : v.invalidReason ? 'invalid' : 'valid');
      if (status === 'partial') {
        partial++;
        issues.push({ name: v.name, reason: v.invalidReason || t('env.partial_tip') });
      } else if (status === 'invalid') {
        invalid++;
        issues.push({ name: v.name, reason: v.invalidReason || t('env.invalid_tip') });
      } else {
        valid++;
      }
    }
    envValidationStats.value = { valid, partial, invalid };
    if (invalid > 0 || partial > 0) {
      const list = issues.slice(0, 20).map(item => `${item.name}: ${item.reason}`).join('\n');
      const extra = issues.length > 20 ? `\n${t('env.more_count', { count: issues.length - 20 })}` : '';
      dialog.warning({
        title: t('env.validate_title'),
        content: `${t('env.invalid_list_hint', { invalid, partial })}\n${list}${extra}`,
        positiveText: t('btn.close'),
      });
      message.warning(t('env.invalid_list_hint', { invalid, partial }));
    } else {
      message.success(t('env.all_valid'));
    }
  } catch (e) {
    console.error('Validate env vars failed:', e);
    message.error(t('env.detect_failed') + ': ' + e);
  }
}

// 清理无效环境变量
async function cleanEnvVar(name, level) {
  try { await window.go.gui.App.CleanEnvVarPath(name, level || 'user'); message.success(t('env.cleaned', { name })); await refreshCurrentEnvVars(); }
  catch (e) { message.error(t('env.clean_failed') + ': ' + e); }
}

// 一键清理所有无效环境变量
async function cleanAllInvalidEnvVars() {
  const level = envAllLevel.value || 'user';
  dialog.warning({
    title: t('env.clean_all_confirm_title'),
    content: t('env.clean_all_confirm_content'),
    positiveText: t('btn.confirm_save'),
    negativeText: t('btn.cancel'),
    onPositiveClick: async () => {
      try {
        const results = await window.go.gui.App.CleanAllInvalidEnvVars(level);
        let cleaned = 0, deleted = 0, skipped = 0;
        const lines = [];
        for (const r of results || []) {
          if (r.action === 'cleaned') { cleaned++; lines.push(`${r.name}: ${t('env.clean_action_cleaned')} — ${r.detail}`); }
          else if (r.action === 'deleted') { deleted++; lines.push(`${r.name}: ${t('env.clean_action_deleted')} — ${r.detail}`); }
          else { skipped++; lines.push(`${r.name}: ${t('env.clean_action_skipped')} — ${r.detail}`); }
        }
        message.success(t('env.clean_all_done', { cleaned, deleted, skipped }));
        if (lines.length > 0) {
          dialog.info({
            title: t('env.clean_all_results'),
            content: lines.join('\n'),
            positiveText: t('btn.close'),
          });
        }
        await refreshCurrentEnvVars();
      } catch (e) {
        message.error(t('env.clean_failed') + ': ' + e);
      }
    },
  });
}

async function setEnvVar(toolName, name, value, level, original = null) {
  // 检查同级是否存在同名变量
  const isEditing = !!original;
  if (!isEditing) {
    try {
      const exists = await window.go.gui.App.CheckEnvVarExists(name, level || 'user');
      if (exists) {
        return new Promise((resolve) => {
          dialog.warning({
            title: t('env.var_exists'),
            content: t('env.var_exists_content', { level: level || 'user', name }),
            positiveText: t('env.overwrite'),
            negativeText: t('btn.cancel'),
            onPositiveClick: async () => {
              await doSetEnvVar(toolName, name, value, level);
              resolve();
            },
            onNegativeClick: () => {
              resolve();
            },
          });
        });
      }
    } catch (e) {
      // 检查失败，继续设置
    }
  }
  await doSetEnvVar(toolName, name, value, level);
}

async function doSetEnvVar(toolName, name, value, level) {
  try {
    await window.go.gui.App.SetEnvVar(toolName, name, value, level || 'user');
    message.success(t('env.var_set_success', { name, tool: toolName, level: level || 'user' }));
    editingEnvVar.value = null;
    showEnvEditor.value = false;
    await refreshCurrentEnvVars();
  } catch (e) {
    message.error(t('env.set_failed') + ': ' + e);
  }
}

function editEnvVar(row) {
  editingEnvVar.value = row;
  showEnvEditor.value = true;
}

function newEnvVar() {
  editingEnvVar.value = null;
  showEnvEditor.value = true;
}

function cancelEditEnvVar() {
  editingEnvVar.value = null;
  showEnvEditor.value = false;
}

async function deleteEnvVar(name, level) {
  try {
    await window.go.gui.App.DeleteEnvVarEntry(name, level || 'user');
    message.success(t('env.var_deleted', { name }));
    await loadAllEnvVars();
  } catch (e) {
    message.error(t('env.delete_failed') + ': ' + e);
  }
}

async function browseEnvDir() {
  try {
    const dir = await window.go.gui.App.BrowseDirectory(t('env.select_dir'));
    return dir || '';
  } catch (e) {
    console.error('Browse directory failed:', e);
    return '';
  }
}

async function refreshTools() {
  if (selectedView.value && selectedView.value !== 'dashboard' && selectedView.value !== 'settings') {
    try {
      await loadTools(selectedView.value);
    } catch (e) {
      console.error('Refresh failed:', e);
    }
  }
}

async function loadCategories() {
  try {
    categories.value = await window.go.gui.App.GetCategories();
  } catch (e) {
    console.error('Failed to load categories:', e);
  }
}

// 扫描旧版目录并弹出迁移确认对话框
async function scanAndPromptLegacyMigration() {
  try {
    const legacyDirs = await window.go.gui.App.ScanLegacyDir();
    if (!legacyDirs || legacyDirs.length === 0) {
      return;
    }

    // 为每个发现的旧目录弹出确认对话框
    for (const dir of legacyDirs) {
      dialog.info({
        title: t('legacy.title'),
        content: t('legacy.found_content', { path: dir.path, newPath: dir.newPath }),
        positiveText: t('legacy.migrate'),
        negativeText: t('btn.cancel'),
        onPositiveClick: async () => {
          migratingLegacy.value = true;
          legacyMigrateProgress.value = t('legacy.migrating');
          try {
            await window.go.gui.App.MigrateLegacyDir(dir.path);
            message.success(t('legacy.success', { newPath: dir.newPath }));
            // 刷新界面数据
            await loadDashboard();
            await refreshTools();
          } catch (e) {
            message.error(t('legacy.failed') + ': ' + e);
          } finally {
            migratingLegacy.value = false;
            legacyMigrateProgress.value = '';
          }
        },
      });
    }
  } catch (e) {
    console.error('Legacy scan failed:', e);
  }
}
</script>

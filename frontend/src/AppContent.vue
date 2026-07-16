<template>
  <AppShell :selected-view="selectedView" :navigation-groups="localizedNavigationGroups"
    :minimize="minimizeWindow" :toggle-maximize="toggleMaximize" :close="closeWindow"
    :minimize-label="t('window.minimize')" :maximize-label="isMaximized ? t('window.restore') : t('window.maximize')" :close-label="t('window.close')"
    @select-view="onMenuSelect" @open-command-palette="openCommandPalette" @escape="setCommandPaletteVisible(false)">
    <template #toolbar>
      <PageToolbar :title="headerTitle">
          <n-button ref="commandPaletteTrigger" size="small" secondary :aria-label="t('command_palette.open')" @click="openCommandPalette">
            <template #icon><n-icon aria-hidden="true"><search-outline /></n-icon></template>
            Ctrl+K
          </n-button>
      </PageToolbar>
    </template>
        <!-- Dashboard -->
        <DashboardPage v-if="selectedView === 'dashboard'" :stats="stats" :loading="dashboardLoading" :error="dashboardError" :t="t" @scan-local="scanLocal" @check-update="checkUpdate" @retry="loadDashboard" />

        <!-- Favorites Page -->
        <FavoritesPage v-else-if="selectedView === 'favorites'" :favorites="favorites" :expanded-name="favVersionExpanded" :installed-versions="favInstalledVers" :available-versions="favAvailableVers" :t="t" @open-detail="openDetailFromFav" @remove-favorite="removeFavorite" @install-latest="installFavLatest" @migrate="migrateTool" @manage-versions="openFavVersionManager" @switch-local="switchToLocal" @use-version="useVersion" @uninstall-version="confirmUninstall" @install="doInstall" />
        <!-- Settings -->
        <SettingsPage v-else-if="selectedView === 'settings'" :t="t" :is-dark="isDark" :locale="locale" :path-mode="pathMode" :mirror-config="mirrorConfig" :mirror-test-results="mirrorTestResults" :proxy="proxyModel" :proxy-test-result="proxyTestResult" :import-result="importResult" :update-info="updateInfo" :diag-results="diagResults" :cache-breakdown="cacheBreakdown" @update:is-dark="toggleTheme" @update:locale="setLocale" @update:path-mode="switchPathMode" @mirror-update="updateMirror" @mirror-test="testMirror" @mirror-remove="removeMirror" @mirror-add="addMirrorValues" @mirror-save="saveMirrors" @update:proxy="updateProxy" @proxy-test="testProxy" @proxy-save="saveProxy" @profile-export="exportProfile" @profile-import="importProfile" @check-update="checkUpdate" @run-diagnostics="runDiag" @cache-clean="cleanCache" />
        <!-- Environment Variables Page -->
        <EnvironmentVariablesPage v-else-if="selectedView === 'env-vars'" :t="t" :browse-directory="browseEnvDir" :env-vars="envVars" :all-env-vars="allEnvVars" :view-mode="envViewMode" :all-level="envAllLevel" :validation-stats="envValidationStats" :editing="editingEnvVar" :migration-mode="migrationMode" :migration-tools="migrationTools" :migration-results="migrationResults" @update:view-mode="setEnvViewMode" @update:all-level="setEnvAllLevel" @validate="validateAllEnvVars" @edit="editEnvVar" @save="saveEnvDraft" @cancel="cancelEditEnvVar" @delete="deleteEnvRow" @clean="cleanEnvRow" @update:migration-mode="migrationMode = $event" @migration-scan="scanForMigration" @migration-single="migrateSingle" @migration-all="migrateAll" />
        <CustomToolsPage v-else-if="selectedView === 'custom-tools'" :t="t" :tools="customTools" :operating="customToolOperating" :submitting="customToolSubmitting" @add="submitAddCustomTool" @install="installCustomTool" @remove="removeCustomTool" />

        <ProjectPage v-else-if="selectedView === 'project'" :t="t" :config="projectConfig" :lockfile="lockfileInfo" :task-states="taskStates" :loading="projectLoading" :error="projectError" :lockfile-loading="lockfileLoading" :lockfile-installing="lockfileInstalling" @set-tool="setProjectTool" @unset-tool="unsetProjectTool" @run-task="runProjectTask" @refresh-lockfile="loadLockfile" @install-lockfile="installFromLockfile" />
        <TasksPage v-else-if="selectedView === 'tasks'" :t="t" :tasks="projectConfig?.tasks" :loading="projectLoading" :error="projectError" :task-states="taskStates" @run="runProjectTask" />
        <PluginsPage v-else-if="selectedView === 'plugins'" :t="t" :plugins="plugins" :loading="pluginsLoading" :error="pluginsError" :creating="pluginCreating" @create="createPlugin" @retry="loadPlugins" />
        <ServicesPage v-else-if="selectedView === 'services'" :t="t" :services="services" :loading="servicesLoading" :error="servicesError" :operating="serviceOperating" @refresh="loadServices" @retry="loadServices" @start="startService" @stop="stopService" />
        <AIAssistantPage v-else-if="selectedView === 'ai'" :t="t" :status="aiStatus" :loading="aiLoading" :error="aiError" :suggesting="aiSuggesting" :diagnosing="aiDiagnosing" :chatting="aiChatting" :suggestion-result="aiSuggestionResult" :suggestion-error="aiSuggestionError" :diagnose-result="aiDiagnoseResult" :diagnose-error="aiDiagnoseError" :chat-result="aiChatResult" :chat-error="aiChatError" @suggest="aiSuggest" @diagnose="aiDiagnose" @chat="aiChat" />

        <IsolationPage v-else-if="selectedView === 'isolate'" :t="t" :status="isolateStatus" :loading="isolateLoading" :error="isolateError" :output="isolateExecOutput" :operating="isolateOperating" @setup="isolateSetup" @exec="isolateExec" @teardown="isolateTeardown" />

        <RemoteDevelopmentPage v-else-if="selectedView === 'remote'" :t="t" :instances="remoteInstances" :loading="remoteLoading" :error="remoteError" :creating="remoteCreating" :operating="remoteOperating" :outputs="remoteOutputs" @list="loadRemoteInstances" @create="createRemoteInstance" @copy-address="copyRemoteAddress" @exec="execRemoteInstance" @destroy="destroyRemoteInstance" />

        <AboutPage v-else-if="selectedView === 'about'" :t="t" :info="aboutInfo" @check-update="checkUpdate" />

        <!-- Tool list (table style) -->
        <ToolCatalogPage v-else :tools="filteredTools" :category="activeCategory" :loading="toolsLoading" :error="toolsError" :operating-tool="pmOperating" v-model:search-query="searchQuery" v-model:hide-unavailable="hideUnavailable" :t="t" @open-detail="openDetail" @install="installLatest" @install-package-manager="installPackageManager" @update-package-manager="updatePackageManager" @toggle-favorite="toggleCatalogFavorite" @retry="retryTools" />
  </AppShell>

  <ToolDetailDrawer v-model:show="showDrawer" :tool="selectedTool" :installed-versions="installedVersions" :available-versions="availableVersions" :show-count="showCount" :return-focus-to="drawerTrigger" :t="t" @toggle-favorite="toggleFavorite" @migrate="migrateTool" @switch-local="switchToLocal" @use-version="useVersion" @uninstall-version="confirmUninstall" @install="doInstall" @show-more="showCount += 20" />
  <!-- Install progress modal -->
  <InstallProgressDialog v-model:show="showProgress" :active="installing" :paused="downloadPaused" :phase="installPhase" :progress="installProgress" :error="installError" :return-focus-to="installTrigger" :t="t" @pause="pauseDownload" @resume="resumeDownload" @cancel="cancelDownload" />
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
import { ref, computed, nextTick, onMounted, onUnmounted } from 'vue';
import { NIcon, NButton, useMessage, useDialog } from 'naive-ui';
import {
  SearchOutline,
  HomeOutline, SettingsOutline, CodeOutline, PhonePortraitOutline, ConstructOutline,
  RocketOutline, DesktopOutline,
} from '@vicons/ionicons5';
import { useI18n } from './i18n/index';
import CommandPalette from './components/CommandPalette.vue';
import DashboardPage from './pages/DashboardPage.vue';
import ToolCatalogPage from './pages/ToolCatalogPage.vue';
import FavoritesPage from './pages/FavoritesPage.vue';
import SettingsPage from './pages/SettingsPage.vue';
import EnvironmentVariablesPage from './pages/EnvironmentVariablesPage.vue';
import ProjectPage from './pages/ProjectPage.vue';
import TasksPage from './pages/TasksPage.vue';
import PluginsPage from './pages/PluginsPage.vue';
import ServicesPage from './pages/ServicesPage.vue';
import AIAssistantPage from './pages/AIAssistantPage.vue';
import IsolationPage from './pages/IsolationPage.vue';
import RemoteDevelopmentPage from './pages/RemoteDevelopmentPage.vue';
import CustomToolsPage from './pages/CustomToolsPage.vue';
import AboutPage from './pages/AboutPage.vue';
import ToolDetailDrawer from './components/ToolDetailDrawer.vue';
import InstallProgressDialog from './components/InstallProgressDialog.vue';
import { useInstall } from './composables/useInstall.js';
import { useTools } from './composables/useTools.js';
import AppShell from './layouts/AppShell.vue';
import PageToolbar from './layouts/PageToolbar.vue';
import { commandPaletteEntries, createNavigationGroups, findPageDefinition } from './navigation/navigation.js';
import { sameEnvIdentity } from './pages/envIdentity.js';
import { createLatestRequestController } from './pages/settingsState.js';
import { WindowMinimise, WindowToggleMaximise, WindowIsMaximised, Quit } from '../wailsjs/runtime/runtime';

const { t, locale, setLocale } = useI18n();

const message = useMessage();
const dialog = useDialog();

// Window control for frameless mode
const isMaximized = ref(false);

function minimizeWindow() {
  WindowMinimise();
}

async function toggleMaximize() {
  WindowToggleMaximise();
  try {
    isMaximized.value = await WindowIsMaximised();
  } catch (error) {
    console.warn('[XDev] Failed to read maximized window state:', error);
  }
}

function closeWindow() {
  Quit();
}

// Poll maximized state (Wails v2 doesn't have event for this)
let maximizePollTimer = null;
onMounted(() => {
  maximizePollTimer = setInterval(() => {
    WindowIsMaximised()
      .then(v => { isMaximized.value = v; })
      .catch(error => console.warn('[XDev] Failed to poll maximized window state:', error));
  }, 500);
});
onUnmounted(() => {
  if (maximizePollTimer) clearInterval(maximizePollTimer);
});

onMounted(async () => {
  await loadCategories();
  await loadDashboard();
  await loadFavorites();
  // Auto-select the first category so tools are visible immediately.
  if (categories.value.length > 0) {
    selectedView.value = categories.value[0].name;
    await onCategorySelect(categories.value[0].name);
  }
  // 扫描旧版目录，发现后弹出迁移确认对话框
  scanAndPromptLegacyMigration();
});

onMounted(() => {
  const stored = localStorage.getItem('xdev-theme');
  if (stored === 'light') {
    isDark.value = false;
    applyThemeToDocument();
  }
});

const selectedView = ref('dashboard');
const isDark = ref(true);

function applyThemeToDocument() {
  document.documentElement.setAttribute('data-theme', isDark.value ? 'dark' : 'light');
}

function toggleTheme(dark) {
  isDark.value = dark;
  localStorage.setItem('xdev-theme', dark ? 'dark' : 'light');
  applyThemeToDocument();
  // Notify parent (App.vue) to switch Naive UI theme
  window.dispatchEvent(new CustomEvent('xdev-theme-change', { detail: dark }));
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
const selectedTool = ref(null);
const showDrawer = ref(false);
const drawerTrigger = ref(null);
const installedVersions = ref([]);
const availableVersions = ref([]);
const showCount = ref(10);
const { install: runInstall, pause: runPause, resume: runResume, cancel: runCancel, phase: installPhase, progress: installProgress, error: installError, active: installing, paused: downloadPaused } = useInstall({ onComplete: handleInstallComplete, onError: handleInstallError, onCancel: handleInstallCancel });
const showProgress = ref(false);
const installTrigger = ref(null);

// Dashboard state
const stats = ref({});
const dashboardLoading = ref(false);
const dashboardError = ref(null);

// Settings state
const mirrorConfig = ref({});
const mirrorTestResults = ref({});
const mirrorRequests = createLatestRequestController();
const updateInfo = ref(null);
const diagResults = ref([]);
const cacheBreakdown = ref({});

// 代理设置状态
const proxyEnabled = ref(false);
const proxyHTTP = ref('');
const proxyHTTPS = ref('');
const proxyNoProxy = ref('');
const proxyTestResult = ref(null);
const proxyRequests = createLatestRequestController();
const proxyModel = computed(() => ({ enabled: proxyEnabled.value, http: proxyHTTP.value, https: proxyHTTPS.value, noProxy: proxyNoProxy.value }));
function updateProxy(value) { proxyEnabled.value = value.enabled; proxyHTTP.value = value.http; proxyHTTPS.value = value.https; proxyNoProxy.value = value.noProxy; }
function updateMirror(tool, url) { mirrorConfig.value[tool] = url; delete mirrorTestResults.value[tool]; }
function removeMirror(tool) { delete mirrorConfig.value[tool]; delete mirrorTestResults.value[tool]; }
function addMirrorValues(tool, url) { mirrorConfig.value[tool] = url; }

// 导出/导入状态
const importResult = ref(null);

// Environment variables & version switching state
const envVars = ref([]);
const editingEnvVar = ref(null);
const newEnvName = ref('');
const newEnvValue = ref('');
const newEnvTool = ref('');
const newEnvLevel = ref('user');

// 环境变量视图切换和验证状态
const envViewMode = ref('dev'); // 'dev' 或 'all'
const envAllLevel = ref('user'); // 全部环境变量的级别
const allEnvVars = ref([]); // 全部环境变量
const envValidationStats = ref(null); // { valid: number, invalid: number }

// Package manager install/update state
const pmOperating = ref('');

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

// Project config state
const projectConfig = ref(null);
const projectLoading = ref(false);
const projectError = ref(null);
const taskStates = ref({});

// Plugins state
const plugins = ref([]);
const pluginsLoading = ref(false);
const pluginsError = ref(null);
const pluginCreating = ref(false);

// Services state
const services = ref([]);
const servicesLoading = ref(false);
const servicesError = ref(null);
const serviceOperating = ref({});

// AI state
const aiStatus = ref(null);
const aiLoading = ref(false);
const aiError = ref(null);
const aiSuggesting = ref(false);
const aiSuggestionResult = ref('');
const aiSuggestionError = ref('');
const aiDiagnosing = ref(false);
const aiDiagnoseResult = ref('');
const aiDiagnoseError = ref('');
const aiChatting = ref(false);
const aiChatResult = ref('');
const aiChatError = ref('');

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

// Lockfile state
const lockfileInfo = ref(null);
const lockfileLoading = ref(false);
const lockfileInstalling = ref(false);

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
    favInstalledVers.value = installed || [];
    favAvailableVers.value = available || [];
  } catch (e) {
    console.error('Failed to load fav versions:', e);
    favInstalledVers.value = [];
    favAvailableVers.value = [];
  }
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
  };
  await openDetail(toolInfo);
  // 切换到收藏页面时刷新收藏列表
  await loadFavorites();
}

// Custom tools state
const customTools = ref([]);
const customToolSubmitting = ref(false);
const customToolOperating = ref({});

async function loadCustomTools() {
  try {
    customTools.value = await window.go.gui.App.GetCustomTools();
  } catch (e) {
    console.error('Failed to load custom tools:', e);
  }
}

async function submitAddCustomTool(tool) {
  if (customToolSubmitting.value) return;
  customToolSubmitting.value = true;
  try {
    const toolJson = JSON.stringify(tool);
    await window.go.gui.App.AddCustomTool(toolJson);
    await loadCustomTools();
    message.success(t('custom_tools.added'));
  } catch (e) {
    message.error(t('custom_tools.add_failed') + ': ' + e);
  } finally { customToolSubmitting.value = false; }
}

async function removeCustomTool(toolName) {
  if (customToolOperating.value[toolName]) return;
  customToolOperating.value = { ...customToolOperating.value, [toolName]: true };
  try {
    await window.go.gui.App.RemoveCustomTool(toolName);
    await loadCustomTools();
    message.success(t('custom_tools.removed'));
  } catch (e) {
    message.error(t('custom_tools.remove_failed') + ': ' + e);
  } finally { const { [toolName]: _, ...rest } = customToolOperating.value; customToolOperating.value = rest; }
}

async function installCustomTool(toolName) {
  if (customToolOperating.value[toolName]) return;
  customToolOperating.value = { ...customToolOperating.value, [toolName]: true };
  try {
    message.info(t('custom_tools.installing'));
    await window.go.gui.App.InstallCustomTool(toolName);
    await loadCustomTools();
    message.success(t('custom_tools.install_success'));
  } catch (e) {
    message.error(t('custom_tools.install_failed') + ': ' + e);
  } finally { const { [toolName]: _, ...rest } = customToolOperating.value; customToolOperating.value = rest; }
}

// ── Project Config ──
async function loadProjectConfig() {
  projectLoading.value = true;
  projectError.value = null;
  try {
    projectConfig.value = await window.go.gui.App.GetProjectConfig();
  } catch (e) {
    projectError.value = e;
    console.error('Failed to load project config:', e);
    message.error(t('project.load_failed') + ': ' + e);
  } finally {
    projectLoading.value = false;
  }
}

async function setProjectTool(tool, version) {
  if (!tool || !version) return;
  try {
    await window.go.gui.App.SetProjectTool(tool, version);
    await loadProjectConfig();
    message.success(t('project.tool_set'));
  } catch (e) {
    message.error(t('project.tool_set_failed') + ': ' + e);
  }
}

async function unsetProjectTool(toolName) {
  try {
    await window.go.gui.App.UnsetProjectTool(toolName);
    await loadProjectConfig();
    message.success(t('project.tool_unset'));
  } catch (e) {
    message.error(t('project.tool_unset_failed') + ': ' + e);
  }
}

async function runProjectTask(taskName) {
  if (taskStates.value[taskName] === 'running') return;
  taskStates.value = { ...taskStates.value, [taskName]: 'running' };
  try {
    await window.go.gui.App.RunProjectTask(taskName);
    taskStates.value = { ...taskStates.value, [taskName]: 'success' };
    message.success(t('project.task_run_success', { name: taskName }));
  } catch (e) {
    taskStates.value = { ...taskStates.value, [taskName]: 'error' };
    message.error(t('project.task_run_failed') + ': ' + e);
  }
}

// ── Plugins ──
async function loadPlugins() {
  pluginsLoading.value = true;
  pluginsError.value = null;
  try {
    plugins.value = await window.go.gui.App.ListPlugins();
  } catch (e) {
    pluginsError.value = e;
    console.error('Failed to load plugins:', e);
    message.error(t('plugins.load_failed') + ': ' + e);
  } finally {
    pluginsLoading.value = false;
  }
}

async function createPlugin(name) {
  if (!name) return;
  pluginCreating.value = true;
  try {
    await window.go.gui.App.CreatePlugin(name);
    await loadPlugins();
    message.success(t('plugins.created'));
  } catch (e) {
    message.error(t('plugins.create_failed') + ': ' + e);
  } finally {
    pluginCreating.value = false;
  }
}

// ── Services ──
async function loadServices() {
  servicesLoading.value = true;
  servicesError.value = null;
  try {
    services.value = await window.go.gui.App.ListServices();
  } catch (e) {
    servicesError.value = e;
    console.error('Failed to load services:', e);
    message.error(t('services.load_failed') + ': ' + e);
  } finally {
    servicesLoading.value = false;
  }
}

async function startService(name) {
  if (serviceOperating.value[name]) return;
  serviceOperating.value = { ...serviceOperating.value, [name]: true };
  try {
    await window.go.gui.App.StartService(name);
    await loadServices();
    message.success(t('services.started', { name }));
  } catch (e) {
    message.error(t('services.start_failed') + ': ' + e);
  } finally {
    const { [name]: _, ...rest } = serviceOperating.value;
    serviceOperating.value = rest;
  }
}

async function stopService(name) {
  if (serviceOperating.value[name]) return;
  serviceOperating.value = { ...serviceOperating.value, [name]: true };
  try {
    await window.go.gui.App.StopService(name);
    await loadServices();
    message.success(t('services.stopped', { name }));
  } catch (e) {
    message.error(t('services.stop_failed') + ': ' + e);
  } finally {
    const { [name]: _, ...rest } = serviceOperating.value;
    serviceOperating.value = rest;
  }
}

// ── AI Assistant ──
async function loadAIStatus() {
  aiLoading.value = true;
  aiError.value = null;
  try {
    aiStatus.value = await window.go.gui.App.GetAIStatus();
  } catch (e) {
    aiError.value = e;
    console.error('Failed to load AI status:', e);
  } finally {
    aiLoading.value = false;
  }
}

async function aiSuggest(tool) {
  if (!tool || aiSuggesting.value) return;
  aiSuggesting.value = true;
  aiSuggestionResult.value = '';
  aiSuggestionError.value = '';
  try {
    aiSuggestionResult.value = await window.go.gui.App.AISuggest(tool);
  } catch (e) {
    aiSuggestionError.value = t('ai.suggest_failed') + ': ' + e;
  } finally {
    aiSuggesting.value = false;
  }
}

async function aiDiagnose(problem) {
  if (!problem || aiDiagnosing.value) return;
  aiDiagnosing.value = true;
  aiDiagnoseResult.value = '';
  aiDiagnoseError.value = '';
  try {
    aiDiagnoseResult.value = await window.go.gui.App.AIDiagnose(problem);
  } catch (e) {
    aiDiagnoseError.value = t('ai.diagnose_failed') + ': ' + e;
  } finally {
    aiDiagnosing.value = false;
  }
}

async function aiChat(chatMessage) {
  if (!chatMessage || aiChatting.value) return;
  aiChatting.value = true;
  aiChatResult.value = '';
  aiChatError.value = '';
  try {
    aiChatResult.value = await window.go.gui.App.AIChat(chatMessage);
  } catch (e) {
    aiChatError.value = t('ai.chat_failed') + ': ' + e;
  } finally {
    aiChatting.value = false;
  }
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
      config.adapter, config.image, config.sshHost, config.sshPort, config.sshUser, config.sshKeyPath,
    );
    message.success(result.message);
    await loadRemoteInstances();
  } catch (e) {
    message.error(t('remote.create_failed') + ': ' + e);
  } finally {
    remoteCreating.value = false;
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

// ── Lockfile ──
async function loadLockfile() {
  lockfileLoading.value = true;
  try {
    lockfileInfo.value = await window.go.gui.App.GetLockfile();
  } catch (e) {
    console.error('Failed to load lockfile:', e);
  } finally {
    lockfileLoading.value = false;
  }
}

async function installFromLockfile() {
  lockfileInstalling.value = true;
  try {
    const result = await window.go.gui.App.InstallFromLockfile();
    message.info(result);
    await loadLockfile();
  } catch (e) {
    message.error(t('lockfile.install_failed') + ': ' + e);
  } finally {
    lockfileInstalling.value = false;
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
const commandPaletteOptions = computed(() => commandPaletteEntries(localizedNavigationGroups.value, t));

async function onCommandSelect(key) {
  await onMenuSelect(key);
}

async function onMenuSelect(key) {
  // 离开设置或环境变量页面时清空环境变量编辑状态
  if ((selectedView.value === 'settings' || selectedView.value === 'env-vars') && key !== 'settings' && key !== 'env-vars') {
    editingEnvVar.value = null;
    newEnvTool.value = '';
    newEnvName.value = '';
    newEnvValue.value = '';
    newEnvLevel.value = 'user';
    envViewMode.value = 'dev';
    allEnvVars.value = [];
    envValidationStats.value = null;
  }
  selectedView.value = key;
  if (key === 'dashboard') {
    await loadDashboard();
  } else if (key === 'favorites') {
    await loadFavorites();
  } else if (key === 'custom-tools') {
    await loadCustomTools();
  } else if (key === 'settings') {
    await loadSettings();
  } else if (key === 'env-vars') {
    await loadEnvVars();
  } else if (key === 'about') {
    await loadAboutInfo();
  } else if (key === 'project') {
    await Promise.all([loadProjectConfig(), loadLockfile()]);
  } else if (key === 'tasks') {
    await loadProjectConfig(); // tasks are part of project config
  } else if (key === 'plugins') {
    await loadPlugins();
  } else if (key === 'services') {
    await loadServices();
  } else if (key === 'ai') {
    await loadAIStatus();
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
  try {
    stats.value = await window.go.gui.App.GetDashboardStats();
  } catch (e) {
    dashboardError.value = e;
    console.error('Failed to load dashboard:', e);
  } finally {
    dashboardLoading.value = false;
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
    // 加载代理配置
    const proxy = await window.go.gui.App.GetProxyConfig();
    proxyEnabled.value = proxy.enabled || false;
    proxyHTTP.value = proxy.http || '';
    proxyHTTPS.value = proxy.https || '';
    proxyNoProxy.value = proxy.noProxy || '';
    await loadCacheBreakdown();
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
  await loadVersions(tool.name);
}

function installLatest(tool) {
  if (tool?.name) doInstall(tool.name, 'latest');
}

function installPackageManager(tool) {
  installPm(tool?.toolName || tool?.name, tool?.displayName || tool?.name);
}

function updatePackageManager(tool) {
  updatePm(tool?.toolName || tool?.name, tool?.displayName || tool?.name);
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
    installedVersions.value = installed || [];
    availableVersions.value = available || [];
  } catch (e) {
    console.error('Failed to load versions:', e);
  }
}

async function doInstall(toolName, version) {
  if (!version || installing.value) return;
  installTrigger.value = showDrawer.value ? drawerTrigger.value : document.activeElement;
  showDrawer.value = false;
  showProgress.value = true;
  const started = await runInstall(toolName, version);
  if (!started && installError.value) message.error(t('install.failed', { error: installError.value.message }));
}

async function refreshInstallViews(toolName) {
  const refreshExpandedFavorite = favVersionExpanded.value === toolName;
  await Promise.all([loadVersions(toolName), refreshTools(), loadDashboard(), loadFavorites()]);
  if (refreshExpandedFavorite) await openFavVersionManager({ name: toolName }, true);
}

async function handleInstallComplete(data) {
  showProgress.value = false;
  message.success(t('install.success', { tool: data.tool, version: data.version }));
  await refreshInstallViews(data.tool);
}

async function handleInstallError(data) {
  message.error(t('install.failed', { error: data.error || data.message }));
  await refreshInstallViews(data.tool || selectedTool.value?.name || '');
}

async function handleInstallCancel(data) {
  await refreshInstallViews(data.tool);
  await nextTick();
  installTrigger.value = document.querySelector(`[data-action="detail"][data-tool="${CSS.escape(data.tool)}"]`) || installTrigger.value;
  showProgress.value = false;
}

async function pauseDownload() {
  if (await runPause()) message.info(t('download_paused'));
}

async function resumeDownload() {
  if (await runResume()) message.info(t('download_resumed'));
}

async function cancelDownload() {
  if (await runCancel()) message.warning(t('download_cancelled'));
}

async function useVersion(toolName, version) {
  try {
    await window.go.gui.App.UseVersion(toolName, version);
    message.success(t('detail.switched_version', { name: toolName, version }));
    await loadVersions(toolName);
    await refreshTools();
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
      } catch (e) {
        message.error(t('detail.uninstall_failed') + ': ' + e);
      }
    },
  });
}

async function scanLocal() {
  try {
    const localTools = await window.go.gui.App.ScanLocalEnvironments();
    if (localTools && localTools.length > 0) {
      message.info(t('dashboard.local_found', { count: localTools.length }));
    } else {
      message.info(t('dashboard.local_none'));
    }
    await refreshTools();
  } catch (e) {
    console.error('Scan failed:', e);
    message.error(t('migration.scan_failed'));
  }
}

async function checkUpdate() {
  try {
    updateInfo.value = await window.go.gui.App.CheckForUpdate();
    if (updateInfo.value && updateInfo.value.hasUpdate) {
      message.info(t('update_available_short', { latest: updateInfo.value.latest }));
    } else if (updateInfo.value && !updateInfo.value.error) {
      message.success(t('up_to_date'));
    }
  } catch (e) {
    message.error(t('update_check_failed') + ': ' + e);
  }
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
  const outcome = await proxyRequests.run('proxy', input, snapshot => window.go.gui.App.TestProxy(snapshot.url, snapshot.noProxy));
  if (!outcome.current) return;
  proxyTestResult.value = outcome.error ? { success: false, message: String(outcome.error), input: outcome.input } : { ...outcome.value, input: outcome.input };
}

async function exportProfile() {
  try {
    const dir = await window.go.gui.App.BrowseDirectory(t('settings.profile_export_dir'));
    if (!dir) {
      return;
    }
    const fullPath = dir + '\\xdev-profile.json';
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
    const filePath = dir + '\\xdev-profile.json';
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

async function migrateTool(toolName, localPath) {
  try {
    await window.go.gui.App.MigrateLocalTool(toolName, localPath);
    message.success(t('migration.migrated_to_xdev', { name: toolName }));
    await loadVersions(toolName);
    await refreshTools();
  } catch (e) {
    message.error(t('migration.migrate_failed') + ': ' + e);
  }
}

async function switchToLocal(toolName) {
  try {
    await window.go.gui.App.SwitchToLocalVersion(toolName);
    message.success(t('detail.switched_to_local', { name: toolName }));
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

async function migrateAll(mode = migrationMode.value) {
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

async function loadEnvVars() {
  try {
    envVars.value = await window.go.gui.App.GetEnvVars();
  } catch (e) {
    console.error('Failed to load env vars:', e);
  }
}

// 视图模式切换
async function onEnvViewModeChange(mode) {
  if (mode === 'all') {
    await loadAllEnvVars();
  }
  envValidationStats.value = null;
}
async function setEnvViewMode(mode) { envViewMode.value = mode; await onEnvViewModeChange(mode); }
async function setEnvAllLevel(level) { envAllLevel.value = level; await loadAllEnvVars(); }
async function refreshCurrentEnvVars() { if (envViewMode.value === 'all') await loadAllEnvVars(); else await loadEnvVars(); }
async function saveEnvDraft(draft) { await setEnvVar(draft.toolName, draft.name, draft.value, draft.level, draft.original); }
async function deleteEnvRow(row) { await deleteEnvVar(row.toolName ?? row.tool, row.name, row.level); if (sameEnvIdentity(editingEnvVar.value, row)) cancelEditEnvVar(); }
async function cleanEnvRow(row) { await cleanEnvVar(row.name, row.level); }

// 加载全部环境变量
async function loadAllEnvVars() {
  try {
    allEnvVars.value = await window.go.gui.App.GetAllEnvVars(envAllLevel.value || 'user');
  } catch (e) {
    console.error('Failed to load all env vars:', e);
    message.error(t('env.load_all_failed') + ': ' + e);
  }
}

// 检测所有环境变量的有效性
async function validateAllEnvVars() {
  try {
    let vars;
    if (envViewMode.value === 'all') {
      vars = allEnvVars.value;
    } else {
      vars = envVars.value;
    }
    let valid = 0;
    let invalid = 0;
    for (const v of vars) {
      if (v.isValid) {
        valid++;
      } else if (v.invalidReason) {
        invalid++;
      }
    }
    envValidationStats.value = { valid, invalid };
    if (invalid > 0) {
      message.warning(t('env.invalid_count', { count: invalid }));
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
    await refreshCurrentEnvVars();
  } catch (e) {
    message.error(t('env.set_failed') + ': ' + e);
  }
}

function editEnvVar(row) {
  editingEnvVar.value = row;
  newEnvTool.value = row.toolName || '';
  newEnvName.value = row.name;
  newEnvValue.value = row.value;
  newEnvLevel.value = row.level || 'user';
}

function cancelEditEnvVar() {
  editingEnvVar.value = null;
  newEnvTool.value = '';
  newEnvName.value = '';
  newEnvValue.value = '';
  newEnvLevel.value = 'user';
}

async function deleteEnvVar(toolName, name, level) {
  try {
    // 在全部环境变量视图下使用 DeleteEnvVarEntry，开发视图下使用 DeleteEnvVar
    if (envViewMode.value === 'all') {
      await window.go.gui.App.DeleteEnvVarEntry(name, level || 'user');
    } else {
      await window.go.gui.App.DeleteEnvVar(toolName, name, level || 'user');
    }
    message.success(t('env.var_deleted', { name }));
    if (envViewMode.value === 'all') {
      await loadAllEnvVars();
    } else {
      await loadEnvVars();
    }
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

async function installPm(toolName, displayName) {
  if (!toolName) return;
  pmOperating.value = toolName;
  try {
    await window.go.gui.App.InstallPackageManager(toolName);
    message.success(t('pm.install_success', { name: displayName }));
    await refreshTools();
  } catch (e) {
    message.error(t('pm.install_failed') + ': ' + e);
  } finally {
    pmOperating.value = '';
  }
}

async function updatePm(toolName, displayName) {
  if (!toolName) return;
  pmOperating.value = toolName;
  try {
    await window.go.gui.App.UpdatePackageManager(toolName);
    message.success(t('pm.update_success', { name: displayName }));
    await refreshTools();
  } catch (e) {
    message.error(t('pm.update_failed') + ': ' + e);
  } finally {
    pmOperating.value = '';
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

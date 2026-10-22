import { ref } from 'vue';

export function useSniffSession({ t, message, addNotification, doInstall }) {
  const availableUpdates = ref([]);
  const updatesLoading = ref(false);
  const sniffOperating = ref(false);
  const updateInstalling = ref({});
  const sniffConfig = ref({});
  const serviceStatus = ref(null);

  async function loadUpdates() {
    updatesLoading.value = true;
    try {
      availableUpdates.value = await window.go.gui.App.GetAvailableUpdates() || [];
    } catch (e) {
      console.error('Failed to load updates:', e);
    } finally {
      updatesLoading.value = false;
    }
  }

  async function loadSniffConfig() {
    try {
      sniffConfig.value = await window.go.gui.App.GetSniffConfig() || {};
    } catch (e) {
      console.error('Failed to load sniff config:', e);
    }
  }

  async function loadSniffServiceStatus() {
    try {
      serviceStatus.value = await window.go.gui.App.SniffServiceStatus();
    } catch (e) {
      console.error('Failed to load sniff service status:', e);
    }
  }

  async function sniffAll() {
    sniffOperating.value = true;
    try {
      await window.go.gui.App.SniffAll();
      addNotification(t('updates.sniff_completed'), 'success');
      await loadUpdates();
    } catch (e) {
      console.error('Sniff all failed:', e);
      addNotification(`${t('updates.sniff_failed')}: ${e}`, 'error');
    } finally {
      sniffOperating.value = false;
    }
  }

  async function sniffTool(tool) {
    try {
      await window.go.gui.App.SniffNow(tool);
      addNotification(t('updates.sniffed_tool', { tool }), 'success');
      await loadUpdates();
    } catch (e) {
      console.error('Sniff tool failed:', e);
      addNotification(`${t('updates.sniff_failed')}: ${e}`, 'error');
    }
  }

  async function installUpdate(update) {
    await doInstall(update.tool, update.latestVersion);
    await loadUpdates();
  }

  async function saveSniffStrategy(strategy) {
    try {
      await window.go.gui.App.SetSniffStrategy(strategy);
      sniffConfig.value.strategy = strategy;
      message.success(t('settings.saved'));
    } catch (e) {
      message.error(`${t('updates.sniff_strategy_save_failed')}: ${e}`);
    }
  }

  async function installSniffService(schedule) {
    try {
      await window.go.gui.App.InstallSniffService(schedule);
      message.success(t('updates.service_installed'));
      await loadSniffServiceStatus();
    } catch (e) {
      message.error(`${t('updates.service_install_failed')}: ${e}`);
    }
  }

  async function uninstallSniffService() {
    try {
      await window.go.gui.App.UninstallSniffService();
      message.success(t('updates.service_uninstalled'));
      await loadSniffServiceStatus();
    } catch (e) {
      message.error(`${t('updates.service_uninstall_failed')}: ${e}`);
    }
  }

  return {
    availableUpdates,
    updatesLoading,
    sniffOperating,
    updateInstalling,
    sniffConfig,
    serviceStatus,
    loadUpdates,
    loadSniffConfig,
    loadSniffServiceStatus,
    sniffAll,
    sniffTool,
    installUpdate,
    saveSniffStrategy,
    installSniffService,
    uninstallSniffService,
  };
}

import { h, ref } from 'vue';
import { NRadio, NRadioGroup } from 'naive-ui';

export function usePackageManagers({ t, message, dialog, refreshTools, selectedTool }) {
  const pmOperating = ref('');

  function installPackageManager(tool) {
    const toolName = tool?.name;
    const displayName = tool?.displayName || tool?.name;
    if (!toolName) return;

    if (tool?.installMethods?.length > 1) {
      showPmMethodSelector(tool);
      return;
    }

    if (tool?.installMethods?.length === 1) {
      installSubTool(toolName, displayName, tool.installMethods[0].name);
      return;
    }

    if (tool?.enableCommand) {
      dialog.warning({
        title: t('pm.enable_confirm_title', { name: displayName }),
        content: `${t('pm.enable_confirm_content', { name: displayName })}\n\n${t('pm.install_command_summary')}\n${tool.enableCommand}`,
        positiveText: t('pm.enable_button'),
        negativeText: t('btn.cancel'),
        ariaLabel: t('pm.enable_confirm_title', { name: displayName }),
        onPositiveClick: () => { installSubTool(tool.name, displayName, ''); },
      });
      return;
    }

    const command = tool?.installCommand || tool?.installHint || t('pm.install_command_unavailable');
    const commandSummary = `${t('pm.install_command_summary')}\n${command}`;
    dialog.warning({
      title: t('pm.install_confirm_title', { name: displayName }),
      content: `${t('pm.install_confirm_content', { name: displayName })}\n\n${commandSummary}`,
      positiveText: tool?.requiresAdmin ? t('pm.install_admin_required') : t('btn.install'),
      negativeText: t('btn.cancel'),
      ariaLabel: t('pm.install_confirm_title', { name: displayName }),
      onPositiveClick: () => installPm(toolName, displayName),
    });
  }

  function showPmMethodSelector(tool) {
    const toolName = tool?.name;
    const displayName = tool?.displayName || tool?.name;
    const methods = tool?.installMethods || [];
    if (methods.length < 2) return;

    const selectedMethod = ref(methods[0].name);
    dialog.warning({
      title: t('pm.install_method_selector_title', { name: displayName }),
      content: () => h(NRadioGroup, {
        value: selectedMethod.value,
        'onUpdate:value': (v) => { selectedMethod.value = v; },
      }, {
        default: () => methods.map(m => h(NRadio, { value: m.name, key: m.name }, { default: () => `[${m.name}] ${m.command}` })),
      }),
      positiveText: t('btn.install'),
      negativeText: t('btn.cancel'),
      ariaLabel: t('pm.install_method_selector_title', { name: displayName }),
      onPositiveClick: () => {
        installSubTool(toolName, displayName, selectedMethod.value);
      },
    });
  }

  async function installSubTool(toolName, displayName, method) {
    if (!toolName) return;
    pmOperating.value = toolName;
    try {
      await window.go.gui.App.InstallSubTool(toolName, method);
      message.success(t('pm.install_success', { name: displayName }));
      await refreshTools();
    } catch (e) {
      message.error(t('pm.install_failed', { name: displayName }) + ': ' + e);
    } finally {
      pmOperating.value = '';
    }
  }

  async function updatePackageManager(tool) {
    const toolName = tool?.name;
    const displayName = tool?.displayName || tool?.name;
    if (!toolName) return;
    const catalogName = tool?.toolName || toolName;
    let latest = '';
    let unverified = false;
    try {
      const versions = await window.go.gui.App.GetAvailableVersions(catalogName);
      latest = versions?.[0]?.version || '';
      if (latest && window.go.gui.App.PreviewInstall) {
        const preview = await window.go.gui.App.PreviewInstall(catalogName, latest);
        unverified = Boolean(preview?.unverified);
      }
    } catch (e) {
      console.error('Preview PM update failed:', e);
    }
    if (unverified && latest) {
      dialog.warning({
        title: t('install.unverified_title') || 'Unverified Version',
        content: t('install.unverified_content', { tool: catalogName, version: latest }) || `Version ${catalogName}@${latest} was discovered by sniffing. Continue?`,
        positiveText: t('btn.update'),
        negativeText: t('btn.cancel'),
        onPositiveClick: () => updatePm(toolName, displayName),
      });
      return;
    }
    await updatePm(toolName, displayName);
  }

  async function installPm(toolName, displayName) {
    if (!toolName) return;
    pmOperating.value = toolName;
    try {
      await window.go.gui.App.InstallPackageManager(toolName);
      message.success(t('pm.install_success', { name: displayName }));
      await refreshTools();
    } catch (e) {
      message.error(t('pm.install_failed', { name: displayName }) + ': ' + e);
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

  async function migratePackageManager(toolName) {
    const displayName = selectedTool.value?.displayName || toolName;
    pmOperating.value = toolName;
    try {
      await window.go.gui.App.MigratePackageManager(toolName);
      message.success(t('pm.migrate_success', { name: displayName }));
      await refreshTools();
    } catch (e) {
      message.error(t('pm.migrate_failed', { name: displayName }) + ': ' + e);
    } finally {
      pmOperating.value = '';
    }
  }

  return {
    pmOperating,
    installPackageManager,
    updatePackageManager,
    migratePackageManager,
  };
}

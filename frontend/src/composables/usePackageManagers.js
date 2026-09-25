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
    // G-06 (P1-38) 入口守卫：pmOperating 由本函数入口置位（不再等到 updatePm），
    // 否则 GetAvailableVersions/PreviewInstall 这段慢 IPC 期间连点两次 Update
    // 会并行跑两条链——弹两个确认框、发两次 UpdatePackageManager。
    if (pmOperating.value) return;
    const catalogName = tool?.toolName || toolName;
    pmOperating.value = catalogName;
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
      // G-06 (P1-38) 守卫所有权：本函数已 return，pmOperating 的复位权交给弹窗回调——
      // onPositiveClick 转 updatePm（由其 finally 复位），onNegativeClick 就地复位。
      // 因此弹窗必须关闭所有会绕过 onNegativeClick 的关闭路径：naive-ui 的
      // closable / closeOnEsc / maskClosable 默认均为 true，任一条生效都会隐藏弹窗
      // 而不触发回调 → pmOperating 永不复位 → Update 按钮永久锁死。
      dialog.warning({
        title: t('install.unverified_title') || 'Unverified Version',
        content: t('install.unverified_content', { tool: catalogName, version: latest }) || `Version ${catalogName}@${latest} was discovered by sniffing. Continue?`,
        positiveText: t('btn.update'),
        negativeText: t('btn.cancel'),
        closable: false,
        closeOnEsc: false,
        maskClosable: false,
        onPositiveClick: () => updatePm(toolName, displayName),
        onNegativeClick: () => { pmOperating.value = ''; },
      });
      return;
    }
    // 直连路径：updatePm 自身在 finally 复位守卫。
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
    // F-1（前端复审）：G-06 同款入口守卫——迁移是复制型重操作且 Go 侧
    // MigratePackageManager 无锁无幂等门，连点两次会并行拷贝同一棵树到同一
    // 目标目录 + 双写环境变量；updatePackageManager 已有同型守卫（见上）。
    if (pmOperating.value) return;
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

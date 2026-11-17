import { h, ref } from 'vue';
import { NRadio, NRadioGroup } from 'naive-ui';
import { useInstall } from './useInstall.js';
import { createInstallCoordinator, confirmInstallAction } from './useInstallCoordinator.js';

export function usePackageManagers({ t, message, dialog, refreshTools, selectedTool,
  coordinator = createInstallCoordinator(), sessionFactory = useInstall }) {
  const pmOperating = ref('');

  async function operate(tool, action, prepare) {
    if (!tool?.name || pmOperating.value) return;
    const lease = coordinator.acquire('package-manager');
    if (!lease) { message.warning(t('install.busy')); return; }
    pmOperating.value = tool.name;
    let session;
    let started = false;
    try {
      const spec = await prepare();
      if (!spec) return;
      session = sessionFactory();
      started = await session.start(spec);
      const outcome = await session.waitForCompletion();
      if (outcome === 'completed') message.success(t(`pm.${action}_success`, { name: tool.displayName || tool.name }));
      else if (outcome !== 'cancelled') message.error(t(`pm.${action}_failed`, { name: tool.displayName || tool.name }) + ': ' + (session.error.value?.message || outcome));
    } catch (e) {
      message.error(t(`pm.${action}_failed`, { name: tool.displayName || tool.name }) + ': ' + e);
    } finally {
      // A terminal failure may have changed disk state too.
      try { if (started) await refreshTools(); }
      finally { session?.dispose(); pmOperating.value = ''; lease.release(); }
    }
  }

  function installPackageManager(tool) {
    return operate(tool, 'install', async () => {
      const name = tool.displayName || tool.name;
      const methods = tool.installMethods || [];
      let method = methods[0]?.name || '';
      if (methods.length > 1) {
        const selected = ref(method);
        if (!(await confirmInstallAction(dialog, {
          title: t('pm.install_method_selector_title', { name }),
          content: () => h(NRadioGroup, { value: selected.value, 'onUpdate:value': (v) => { selected.value = v; } }, {
            default: () => methods.map(m => h(NRadio, { value: m.name, key: m.name }, { default: () => `[${m.name}] ${m.command}` })),
          }), positiveText: t('btn.install'), negativeText: t('btn.cancel'),
        }))) return null;
        method = selected.value;
      } else if (!methods.length) {
        const enable = Boolean(tool.enableCommand);
        if (!(await confirmInstallAction(dialog, {
          title: t(enable ? 'pm.enable_confirm_title' : 'pm.install_confirm_title', { name }),
          content: `${t(enable ? 'pm.enable_confirm_content' : 'pm.install_confirm_content', { name })}\n\n${t('pm.install_command_summary')}\n${tool.enableCommand || tool.installCommand || tool.installHint || t('pm.install_command_unavailable')}`,
          positiveText: enable ? t('pm.enable_button') : tool.requiresAdmin ? t('pm.install_admin_required') : t('btn.install'),
          negativeText: t('btn.cancel'),
        }))) return null;
      }
      return { tool: tool.name, kind: methods.length || tool.enableCommand ? 'sub-tool' : 'package-manager', method, confirmed: true };
    });
  }

  function updatePackageManager(tool) {
    return operate(tool, 'update', async () => {
      const catalogName = tool.toolName || tool.name;
      let latest = '';
      let preview;
      try {
        const versions = await window.go.gui.App.GetAvailableVersions(catalogName);
        latest = versions?.[0]?.version || '';
        if (latest) preview = await window.go.gui.App.PreviewInstall(catalogName, latest);
      } catch (e) { console.error('Preview PM update failed:', e); }
      if (!latest) throw new Error(`No versions available for ${catalogName}`);
      if ((!preview || preview.unverified) && !(await confirmInstallAction(dialog, {
        title: t('install.unverified_title'),
        content: t('install.unverified_content', { tool: catalogName, version: latest }),
        positiveText: t('btn.update'), negativeText: t('btn.cancel'),
      }))) return null;
      return { tool: catalogName, version: latest, confirmed: true };
    });
  }

  async function migratePackageManager(toolName) {
    if (!toolName || pmOperating.value) return;
    const lease = coordinator.acquire('package-manager-migration');
    if (!lease) { message.warning(t('install.busy')); return; }
    const name = selectedTool.value?.displayName || toolName;
    pmOperating.value = toolName;
    try {
      await window.go.gui.App.MigratePackageManager(toolName);
      message.success(t('pm.migrate_success', { name }));
      await refreshTools();
    } catch (e) { message.error(t('pm.migrate_failed', { name }) + ': ' + e); }
    finally { pmOperating.value = ''; lease.release(); }
  }
  return { pmOperating, installPackageManager, updatePackageManager, migratePackageManager };
}

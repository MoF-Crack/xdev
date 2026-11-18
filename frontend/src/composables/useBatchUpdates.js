import { h, ref } from 'vue';
import { NRadio, NRadioGroup } from 'naive-ui';
import { createInstallCoordinator, confirmInstallAction } from './useInstallCoordinator.js';

export function useBatchUpdates({
  previewInstall, parallelBatch, dialog, message, t, loadUpdates, availableUpdates,
  setShowProgress, active, batchActive, coordinator = createInstallCoordinator(),
}) {
  async function installAllUpdates() {
    const list = [...(availableUpdates?.value || [])];
    if (!list.length) return;
    if (active?.value || batchActive?.value) { message.warning(t('install.busy')); return; }
    const lease = coordinator.acquire('batch');
    if (!lease) { message.warning(t('install.busy')); return; }
    batchActive.value = true;
    let shown = false;
    try {
      if (!(await confirmInstallAction(dialog, {
        title: t('updates.title'), content: t('updates.install_all_confirm', { n: list.length }),
        positiveText: t('btn.update'), negativeText: t('btn.cancel'),
      }))) return;
      const confirmed = [];
      let skipped = 0;
      for (const u of list) {
        let preview;
        try { preview = await previewInstall(u.tool, u.latestVersion); } catch { preview = null; }
        if ((!preview || preview.unverified) && !(await confirmInstallAction(dialog, {
          title: t('install.unverified_title'),
          content: t('install.unverified_content', { tool: u.tool, version: u.latestVersion }),
          positiveText: t('btn.install'), negativeText: t('btn.skip'),
        }))) { skipped++; continue; }
        let force = false;
        const integrity = preview?.integrity;
        if (integrity?.path && !integrity.ok && integrity.reason !== 'not installed') {
          force = await confirmInstallAction(dialog, {
            title: t('confirm_repair_title'), content: t('confirm_repair_content', { tool: u.tool, version: u.latestVersion }),
            positiveText: t('detail.repair'), negativeText: t('btn.skip'),
          });
          if (!force) { skipped++; continue; }
        }
        let formFilter = preview?.hasFormSpecificArchives && preview.availableForms?.length === 1 ? preview.availableForms[0] : '';
        if (preview?.hasFormSpecificArchives && preview.availableForms?.length > 1) {
          const selected = ref(preview.availableForms[0]);
          if (!(await confirmInstallAction(dialog, {
            title: `${u.tool}@${u.latestVersion}`,
            content: () => h(NRadioGroup, { value: selected.value, 'onUpdate:value': value => { selected.value = value; } }, {
              default: () => preview.availableForms.map(form => h(NRadio, { value: form, key: form }, { default: () => form })),
            }),
            positiveText: t('btn.install'), negativeText: t('btn.skip'),
          }))) { skipped++; continue; }
          formFilter = selected.value;
        }
        confirmed.push({ tool: u.tool, version: u.latestVersion, formFilter, force });
      }
      if (!confirmed.length) {
        message.info(t('updates.install_all_summary', { done: 0, failed: 0, skipped }));
        return;
      }
      shown = true;
      setShowProgress(true);
      const { done, failed, cancelled } = await parallelBatch.run(confirmed);
      await loadUpdates();
      message.info(t('updates.install_all_summary', { done, failed, skipped: skipped + cancelled }));
    } finally {
      batchActive.value = false;
      if (shown) setShowProgress(false);
      lease.release();
    }
  }
  return { installAllUpdates };
}

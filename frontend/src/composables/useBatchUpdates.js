// useBatchUpdates — GUI「一键更新全部」的批量安装（Spec 035 F2b + Spec 037 阶段 1）。
//
// 设计原则：
// 1. FR-018 逐项确认：unverified 版本每项单独弹确认，拒绝→跳过该
//    项继续；PreviewInstall 调用失败（旧构建）同样按 unverified 弹
//    确认，绝不静默跳过。确认阶段串行（用户逐一决策）。
// 2. Spec 037 阶段 1：确认后的清单交 useParallelBatch 并行执行——
//    每项独立会话（事件按 taskId 路由），Promise.allSettled 等待，
//    单项失败/取消不影响其他项。
// 3. 取消 = cancelAll 取消所有进行中的项（引擎级）。
// 4. 真实汇总（成功/失败/取消计数）；N2 互斥与 N4/N5 防闪烁语义保留
//    （batchActive 期间完成/取消回调不复位全局进度框）。
// 5. 批量形态：默认形态，仅「形态特定且唯一形态」自动选定（对齐
//    单装逻辑）；不弹形态选择对话框。
//
// 依赖全注入（previewInstall/installConfirmed 工厂/parallelBatch/
// dialog/message/t/loadUpdates/availableUpdates/setShowProgress/
// active/batchActive），AppContent 仅装配——批量逻辑在本 composable
// 单测覆盖。

export function useBatchUpdates({
  previewInstall,
  parallelBatch,
  dialog,
  message,
  t,
  loadUpdates,
  availableUpdates,
  setShowProgress,
  active,
  batchActive,
}) {
  // confirmUnverified resolves true (install) / false (skip). It is the
  // per-item confirmation required by FR-018 — the initial batch dialog
  // does NOT count as prior confirmation of the unverified risk.
  function confirmUnverified(tool, version) {
    return new Promise((resolve) => {
      dialog.warning({
        title: t('install.unverified_title') || 'Unverified Version',
        content: t('install.unverified_content', { tool, version })
          || `Version ${tool}@${version} was discovered by sniffing. Continue?`,
        positiveText: t('btn.install'),
        negativeText: t('btn.skip'),
        closable: true,
        onPositiveClick: () => resolve(true),
        onNegativeClick: () => resolve(false),
        onClose: () => resolve(false),
      });
    });
  }

  async function installAllUpdates() {
    const list = [...(availableUpdates?.value || [])];
    if (!list.length) return;
    // N2: never enter the batch while another install is running.
    if (active?.value) {
      message.warning(t('install.busy'));
      return;
    }
    // Ticket 12 (G-02 layer 1): never enter while a previous batch is
    // still running — the flag used to be set only AFTER the confirmation
    // dialog resolved, so the dialog window allowed double entry.
    if (batchActive?.value) {
      message.warning(t('install.busy'));
      return;
    }
    const ok = await new Promise((resolve) => {
      dialog.warning({
        title: t('updates.title'),
        content: t('updates.install_all_confirm', { n: list.length }),
        positiveText: t('btn.update'),
        negativeText: t('btn.cancel'),
        closable: true,
        onPositiveClick: () => resolve(true),
        onNegativeClick: () => resolve(false),
        onClose: () => resolve(false),
      });
    });
    if (!ok) return;
    // Ticket 12 (G-02 layer 2): the dialog may have stayed open long
    // enough for another entry to start a batch — re-check AFTER the
    // dialog resolved and BEFORE taking ownership.
    if (batchActive?.value) {
      message.warning(t('install.busy'));
      return;
    }

    // Ticket 12 (G-02 layer 4 — ownership): only the invocation that SET
    // the flags may RESET them. A rejected late-comer reaching this point
    // must not clear the running batch's state (batchActive=false +
    // setShowProgress(false) would strip the re-entry protection and
    // close the live progress dialog).
    const owned = !batchActive.value;
    if (batchActive) batchActive.value = true;
    try {
      // ---- Phase 1 (serial): per-item FR-018 confirmation ----
      const confirmed = [];
      let skipped = 0;
      for (const u of list) {
        let preview = null;
        try {
          preview = await previewInstall(u.tool, u.latestVersion);
        } catch {
          preview = null; // old build without PreviewInstall — treat as unverified
        }
        const unverified = preview ? Boolean(preview.unverified) : true;
        if (unverified && !(await confirmUnverified(u.tool, u.latestVersion))) {
          skipped++;
          continue;
        }

        // Form handling mirrors single install: pick the only form
        // automatically; otherwise use the default form (empty filter).
        let formFilter = '';
        if (preview?.hasFormSpecificArchives && (preview.availableForms || []).length === 1) {
          formFilter = preview.availableForms[0];
        }
        confirmed.push({ tool: u.tool, version: u.latestVersion, formFilter });
      }

      if (!confirmed.length) {
        message.info(t('updates.install_all_summary', { done: 0, failed: 0, skipped }));
        return;
      }

      // ---- Phase 2 (parallel): execute the confirmed set ----
      setShowProgress(true);
      const { done, failed, cancelled } = await parallelBatch.run(confirmed);
      await loadUpdates();
      message.info(t('updates.install_all_summary', { done, failed, skipped: skipped + cancelled }));
    } finally {
      // Ticket 12 (G-02 layer 4): only the owner resets the shared flags.
      if (owned) {
        if (batchActive) batchActive.value = false;
        setShowProgress(false); // safety net: reset on every exit path
      }
    }
  }

  return { installAllUpdates };
}

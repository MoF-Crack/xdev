// useBatchUpdates — GUI「一键更新全部」的批量安装循环（Spec 035 F2b）。
//
// 设计原则（solution.md F2 v4）：
// 1. FR-018 逐项确认：unverified 版本每项单独弹确认，拒绝→跳过该
//    项继续；PreviewInstall 调用失败（旧构建）同样按 unverified 弹
//    确认，绝不静默跳过。
// 2. 事件驱动等待（waitForCompletion，来自 useInstall），不忙等。
// 3. 取消 = 取消当前项并中止整批（cancelled 文案）。
// 4. 真实汇总（成功/失败/跳过）；取消另有文案。
// 5. 批量形态：默认形态，仅「形态特定且唯一形态」自动选定（对齐
//    单装逻辑）；不弹形态选择对话框。
//
// Spec 036 增量：
// - N2 安装互斥：入口守卫（active 时提示 busy）+ 逐项预检（用户经
//   Run in Background 后门插入单项安装时批量让位）+ started=false
//   不关进度框（进度框属正在进行的另一安装，finally 兜底复位）。
// - N4 防闪烁：batchActive 循环期间置 true——AppContent 的完成/取消
//   回调据此跳过 showProgress 复位（由本循环统一管理关闭时机），
//   error 路径同样不再逐项关开。
//
// 依赖全注入（previewInstall/installConfirmed/waitForCompletion/dialog/
// message/t/loadUpdates/availableUpdates/setShowProgress/active/
// batchActive），AppContent 仅装配——批量逻辑在本 composable 单测覆盖。

export function useBatchUpdates({
  previewInstall,
  installConfirmed,
  waitForCompletion,
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
    // N2: never enter the batch while another install is running — every
    // per-item install would hit the startInstallConfirmed mutex guard.
    if (active?.value) {
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

    let done = 0;
    let failed = 0;
    let skipped = 0;
    let cancelled = false;
    if (batchActive) batchActive.value = true;
    try {
      for (const u of list) {
        // N2 pre-item check: a manual install may have been squeezed in
        // through "Run in Background" between items — yield the rest of
        // the batch instead of hammering the mutex guard per item.
        if (active?.value) {
          cancelled = true;
          break;
        }
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

        setShowProgress(true);
        const started = await installConfirmed(u.tool, u.latestVersion, formFilter);
        if (!started) {
          failed++;
          // N2: keep the progress dialog open — a false start is almost
          // always an active-conflict and the visible dialog belongs to
          // the install that IS running. The finally net closes it.
          continue;
        }

        // 'completed' | 'error' | 'cancelled' | 'disposed' | 'idle'
        const result = await waitForCompletion();
        if (result === 'cancelled' || result === 'disposed') {
          cancelled = true;
          break;
        }
        if (result === 'completed') {
          done++;
          await loadUpdates();
        } else {
          // 'error' | 'idle' (idle = all events lost; count as failed).
          // N4: do not close/reopen the dialog per failed item — the
          // finally net closes it once the batch ends.
          failed++;
        }
      }
    } finally {
      if (batchActive) batchActive.value = false;
      setShowProgress(false); // safety net: reset on every exit path
    }
    if (cancelled) {
      message.warning(t('updates.install_all_cancelled', { done, failed, skipped }));
    } else {
      message.info(t('updates.install_all_summary', { done, failed, skipped }));
    }
  }

  return { installAllUpdates };
}

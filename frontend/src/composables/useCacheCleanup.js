// Wails rejects a promise on transport errors. Application-level cleanup
// results include partial counts so the view can refresh after a partial sweep.
export function useCacheCleanup({ t, message, showCacheSize, loadCacheBreakdown, loadDashboard }) {
  async function run(request, refresh) {
    let result;
    try {
      result = await request();
    } catch (error) {
      message.error(`${t('cache.clean_failed')}: ${error}`);
      return;
    }
    const counts = t('cache.clean_counts', result);
    const feedback = result.detail ? `${counts} — ${result.detail}` : counts;
    if (result.failed > 0) message.error(feedback);
    else if (result.busy > 0) message.warning(feedback);
    else message.success(feedback);
    await refresh();
    await loadDashboard();
  }

  return {
    cleanCache: category => run(() => window.go.gui.App.CleanCacheCategoryResult(category), loadCacheBreakdown),
    cleanCacheCategory: category => run(() => window.go.gui.App.CleanCacheCategoryResult(category), showCacheSize),
    deleteCacheFile: entry => run(() => window.go.gui.App.DeleteCacheFileResult(entry.path), showCacheSize),
  };
}

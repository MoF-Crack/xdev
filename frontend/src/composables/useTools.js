import { computed, ref } from 'vue';

export function useTools(api = window.go.gui.App) {
  const tools = ref([]);
  const loading = ref(false);
  const error = ref(null);
  const searchQuery = ref('');
  const hideUnavailable = ref(true);
  const activeCategory = ref('');
  let requestId = 0;

  const filteredTools = computed(() => {
    const query = searchQuery.value.trim().toLowerCase();
    return tools.value.filter((tool) => {
      const available = tool.installed || tool.localVersion || tool.localPath || tool.canInstall;
      if (hideUnavailable.value && !available) return false;
      if (!query) return true;
      return [tool.name, tool.displayName, tool.description]
        .some((value) => String(value || '').toLowerCase().includes(query));
    });
  });

  async function loadTools(category) {
    if (category) activeCategory.value = category;
    if (!activeCategory.value) return;
    const currentRequest = ++requestId;
    loading.value = true;
    error.value = null;
    try {
      const result = await api.GetToolsByCategory(activeCategory.value);
      if (currentRequest === requestId) tools.value = result || [];
    } catch (cause) {
      if (currentRequest === requestId) error.value = cause;
    } finally {
      if (currentRequest === requestId) loading.value = false;
    }
  }

  function retry() {
    return loadTools(activeCategory.value);
  }

  return { tools, loading, error, searchQuery, hideUnavailable, activeCategory, filteredTools, loadTools, retry };
}

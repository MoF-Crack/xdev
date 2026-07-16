import { describe, expect, it, vi } from 'vitest';

import { useTools } from '../../src/composables/useTools.js';

describe('useTools', () => {
  it('loads, searches, and hides unavailable tools', async () => {
    const api = { GetToolsByCategory: vi.fn().mockResolvedValue([
      { name: 'node', displayName: 'Node.js', description: 'Runtime', installed: true },
      { name: 'zig', displayName: 'Zig', description: 'Compiler', installed: false },
    ]) };
    const state = useTools(api);
    await state.loadTools('language');
    expect(state.tools.value).toHaveLength(2);
    expect(state.filteredTools.value.map((tool) => tool.name)).toEqual(['node']);
    state.hideUnavailable.value = false;
    state.searchQuery.value = 'compiler';
    expect(state.filteredTools.value.map((tool) => tool.name)).toEqual(['zig']);
  });

  it('exposes failures and retries the active category', async () => {
    const api = { GetToolsByCategory: vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce([{ name: 'go' }]) };
    const state = useTools(api);
    await state.loadTools('language');
    expect(state.error.value.message).toBe('offline');
    await state.retry();
    expect(api.GetToolsByCategory).toHaveBeenLastCalledWith('language');
    expect(state.tools.value).toEqual([{ name: 'go' }]);
  });

  it('does not let an older category response replace the latest category', async () => {
    let resolveOld;
    const api = { GetToolsByCategory: vi.fn((category) => category === 'old'
      ? new Promise((resolve) => { resolveOld = resolve; })
      : Promise.resolve([{ name: 'new-tool' }])) };
    const state = useTools(api);
    const oldRequest = state.loadTools('old');
    await state.loadTools('new');
    resolveOld([{ name: 'old-tool' }]);
    await oldRequest;
    expect(state.activeCategory.value).toBe('new');
    expect(state.tools.value).toEqual([{ name: 'new-tool' }]);
  });
});

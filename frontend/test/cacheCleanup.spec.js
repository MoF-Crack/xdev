import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, expect, it, vi } from 'vitest';
import { cleanupWailsMocks, createInitializedWailsBrowserMocks } from './mocks/wails.js';

const message = vi.hoisted(() => ({ success: vi.fn(), warning: vi.fn(), error: vi.fn(), info: vi.fn() }));
vi.mock('naive-ui', async (original) => ({ ...await original(), useMessage: () => message, useDialog: () => ({}) }));
import AppContent from '../src/AppContent.vue';

let wrapper;
afterEach(() => { wrapper?.unmount(); cleanupWailsMocks(); vi.clearAllMocks(); });

it.each([
  ['cleanCache', 'all', 'CleanCacheCategoryResult', { removed: 3, busy: 0, failed: 0 }, 'success'],
  ['cleanCacheCategory', 'partFiles', 'CleanCacheCategoryResult', { removed: 2, busy: 1, failed: 0 }, 'warning'],
  ['deleteCacheFile', { path: 'demo.zip' }, 'DeleteCacheFileResult', { removed: 1, busy: 0, failed: 2, detail: 'file access denied' }, 'error'],
  ['deleteCacheFile', { path: 'missing.zip' }, 'DeleteCacheFileResult', { removed: 0, busy: 0, failed: 0 }, 'success'],
])('%s shows the returned cleanup counts', async (handler, arg, method, result, level) => {
  createInitializedWailsBrowserMocks();
  const backend = vi.fn(async () => result);
  window.go.gui.App[method] = backend;
  wrapper = mount(AppContent, { shallow: true });
  await flushPromises();
  await wrapper.vm[handler](arg);
  expect(backend).toHaveBeenCalledWith(arg.path ?? arg);
  const counts = wrapper.vm.t('cache.clean_counts', result);
  expect(counts).toContain(String(result.removed));
  expect(message[level]).toHaveBeenCalledWith(result.detail ? `${counts} — ${result.detail}` : counts);
});

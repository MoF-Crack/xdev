import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';

const message = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() }));
vi.mock('naive-ui', async (importOriginal) => ({ ...await importOriginal(), useMessage: () => message, useDialog: () => ({}) }));
import AppContent from '../src/AppContent.vue';
import { cleanupWailsMocks, createInitializedWailsBrowserMocks } from './mocks/wails.js';

function mountRemote(overrides) {
  createInitializedWailsBrowserMocks();
  Object.assign(window.go.gui.App, overrides);
  return mount(AppContent, {
    shallow: true,
    global: { stubs: { AppShell: { template: '<main><slot /></main>' }, RemoteDevelopmentPage: { name: 'RemotePageProbe', props: ['instances', 'diagnostics', 'error'], template: '<div />' } } },
  });
}

afterEach(() => { cleanupWailsMocks(); document.body.innerHTML = ''; });

describe('remote repair consumer', () => {
  it('keeps healthy entries together with visible file diagnostics', async () => {
    const instances = [{ id: 'healthy', adapter: 'ssh', status: 'running', addresses: ['build-host:22'] }];
    const diagnostics = [{ path: 'fixtures/instances/broken.json', kind: 'instance', message: 'invalid instance JSON' }];
    const snapshot = vi.fn(async () => ({ instances, diagnostics }));
    const wrapper = mountRemote({ ListRemoteInstancesWithDiagnostics: snapshot });
    await flushPromises();
    await wrapper.vm.onMenuSelect('remote');
    await flushPromises();
    const remote = wrapper.getComponent({ name: 'RemotePageProbe' });
    expect(snapshot).toHaveBeenCalled();
    expect(remote.props('instances')).toEqual(instances);
    expect(remote.props('diagnostics')).toEqual(diagnostics);
    expect(remote.props('error')).toBeNull();
    wrapper.unmount();
  });

  it('renders rejected execution as failure with the retained output', async () => {
    const failure = 'remote execution failed (exit 23): fixture error\nstdout text\nstderr text';
    const wrapper = mountRemote({ ExecRemoteInstance: async () => { throw failure; } });
    await flushPromises();
    await wrapper.vm.execRemoteInstance('docker', 'fixture', 'go version');
    expect(wrapper.vm.remoteOutputs.fixture).toContain(failure);
    expect(wrapper.vm.remoteOutputs.fixture).toBe(`${wrapper.vm.t('remote.exec_failed')}: ${failure}`);
    expect(wrapper.vm.remoteOperating.fixture.exec).toBe(false);
    wrapper.unmount();
  });
});

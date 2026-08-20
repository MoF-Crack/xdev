import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';

import AsyncState from '../../src/components/AsyncState.vue';
import ConfirmAction from '../../src/components/ConfirmAction.vue';
import CopyableValue from '../../src/components/CopyableValue.vue';
import EmptyState from '../../src/components/EmptyState.vue';
import FormSection from '../../src/components/FormSection.vue';
import PageHeader from '../../src/components/PageHeader.vue';
import ResponsiveDataTable from '../../src/components/ResponsiveDataTable.vue';
import SectionCard from '../../src/components/SectionCard.vue';
import StatusBadge from '../../src/components/StatusBadge.vue';

afterEach(() => vi.restoreAllMocks());

describe('shared page structure', () => {
  it('renders the PageHeader title, description, primary, and secondary slots', () => {
    const wrapper = mount(PageHeader, { slots: {
      title: 'Project tools', description: 'Versions used here',
      primary: '<button>Add tool</button>', secondary: '<button>Refresh</button>',
    } });
    expect(wrapper.get('h1').text()).toBe('Project tools');
    expect(wrapper.text()).toContain('Versions used here');
    expect(wrapper.findAll('button').map((button) => button.text())).toEqual(['Refresh', 'Add tool']);
  });

  it('gives SectionCard a consistent title, description, actions, and body', () => {
    const wrapper = mount(SectionCard, { props: { title: 'Runtime', description: 'Current selection' }, slots: {
      actions: '<button>Change</button>', default: '<p>Node 22</p>',
    } });
    expect(wrapper.get('h2').text()).toBe('Runtime');
    expect(wrapper.text()).toContain('Current selection');
    expect(wrapper.text()).toContain('Node 22');
    expect(wrapper.get('button').text()).toBe('Change');
  });

  it('groups related controls in a labelled FormSection', () => {
    const wrapper = mount(FormSection, { props: { title: 'Download source', description: 'Choose a mirror' }, slots: {
      default: '<label>URL <input /></label>', actions: '<button>Save</button>',
    } });
    expect(wrapper.get('fieldset').exists()).toBe(true);
    expect(wrapper.get('legend').text()).toBe('Download source');
    expect(wrapper.text()).toContain('Choose a mirror');
    expect(wrapper.get('button').text()).toBe('Save');
  });

  it('omits empty heading and action containers', () => {
    expect(mount(SectionCard, { slots: { default: 'Body' } }).find('h2').exists()).toBe(false);
    expect(mount(PageHeader, { props: { title: 'Tools' } }).find('.page-header__actions').exists()).toBe(false);
  });
});

describe('shared states', () => {
  it.each([
    [{ loading: true }, 'Loading'],
    [{ empty: true }, 'Nothing here'],
    [{ error: 'Unable to load tools' }, 'Unable to load tools'],
    [{}, 'Loaded content'],
  ])('renders the explicit async state %#', (props, expected) => {
    const wrapper = mount(AsyncState, { props, slots: {
      loading: 'Loading', empty: 'Nothing here', default: 'Loaded content',
    } });
    expect(wrapper.text()).toContain(expected);
  });

  it('emits retry from the error state', async () => {
    const wrapper = mount(AsyncState, { props: { error: 'Network unavailable', retryLabel: 'Try again' } });
    await wrapper.get('button').trigger('click');
    expect(wrapper.emitted('retry')).toHaveLength(1);
  });

  it('shows an error instead of loading when both states are present', () => {
    const wrapper = mount(AsyncState, { props: { loading: true, error: 'Request failed' }, slots: { loading: 'Loading' } });
    expect(wrapper.text()).toContain('Request failed');
    expect(wrapper.text()).not.toContain('Loading');
  });

  it('renders an optional EmptyState action', async () => {
    const wrapper = mount(EmptyState, { props: { title: 'No tools' }, slots: { action: '<button>Add tool</button>' } });
    expect(wrapper.get('[role="status"]').text()).toContain('No tools');
    expect(wrapper.get('button').text()).toBe('Add tool');
  });
});

describe('StatusBadge', () => {
  it('communicates semantic status with text and an optional icon, not color alone', () => {
    const wrapper = mount(StatusBadge, { props: { tone: 'success' }, slots: { icon: '<span aria-hidden="true">✓</span>', default: 'Running' } });
    expect(wrapper.get('[data-tone="success"]').text()).toContain('Running');
    expect(wrapper.find('[role="status"]').exists()).toBe(false);
    expect(wrapper.get('[aria-hidden="true"]').exists()).toBe(true);
  });

  it('only announces status when live is requested', () => {
    expect(mount(StatusBadge, { props: { live: true }, slots: { default: 'Connected' } }).get('[role="status"]').text()).toBe('Connected');
  });
});

describe('CopyableValue', () => {
  it('copies the complete value when the path itself is clicked', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
    const value = 'C:\\Users\\developer\\.xdev\\install\\node\\22.0.0';
    const wrapper = mount(CopyableValue, { props: { value, copyLabel: 'Copy path' } });
    const button = wrapper.get('button.copyable-value__path');
    expect(button.attributes('aria-label')).toBe('Copy path');
    expect(button.attributes('title')).toContain(value);
    expect(button.classes()).toContain('xdev-mono');
    expect(wrapper.findAll('button')).toHaveLength(1);
    await button.trigger('click');
    await vi.waitFor(() => expect(writeText).toHaveBeenCalledWith(value));
    expect(wrapper.emitted('success')).toEqual([[value]]);
  });

  it('emits failure and exposes testable feedback when clipboard writing fails', async () => {
    const error = new Error('permission denied');
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockRejectedValue(error) } });
    const wrapper = mount(CopyableValue, { props: { value: 'secret', failureLabel: 'Copy failed' } });
    await wrapper.get('button.copyable-value__path').trigger('click');
    await vi.waitFor(() => expect(wrapper.emitted('failure')).toEqual([[error]]));
    expect(wrapper.get('[role="status"]').text()).toBe('Copy failed');
  });

  it('disables copying while pending and ignores repeated activation', async () => {
    let resolve;
    const writeText = vi.fn(() => new Promise((done) => { resolve = done; }));
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
    const wrapper = mount(CopyableValue, { props: { value: 'value' } });
    await wrapper.get('button.copyable-value__path').trigger('click');
    expect(wrapper.get('button').attributes('disabled')).toBeDefined();
    await wrapper.get('button.copyable-value__path').trigger('click');
    expect(writeText).toHaveBeenCalledOnce();
    resolve();
    await vi.waitFor(() => expect(wrapper.emitted('success')).toHaveLength(1));
  });

  it('recreates live feedback for repeated successful copies', async () => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockResolvedValue(undefined) } });
    const wrapper = mount(CopyableValue, { props: { value: 'value' } });
    await wrapper.get('button.copyable-value__path').trigger('click');
    await vi.waitFor(() => expect(wrapper.find('[role="status"]').exists()).toBe(true));
    const first = wrapper.get('[role="status"]').attributes('data-announcement');
    await wrapper.get('button.copyable-value__path').trigger('click');
    await vi.waitFor(() => expect(wrapper.get('[role="status"]').attributes('data-announcement')).not.toBe(first));
  });
});

describe('ResponsiveDataTable', () => {
  function mediaQuery(matches) {
    const listeners = new Set();
    return {
      media: { matches, addEventListener: vi.fn((_, fn) => listeners.add(fn)), removeEventListener: vi.fn((_, fn) => listeners.delete(fn)) },
      listeners,
    };
  }

  it('uses n-data-table on desktop and switches to the explicit card slot on narrow screens', async () => {
    const { media, listeners } = mediaQuery(false);
    vi.spyOn(window, 'matchMedia').mockReturnValue(media);
    const wrapper = mount(ResponsiveDataTable, {
      props: { columns: [
        { key: 'name', title: 'Name', contract: 'identity' },
        { key: 'status', title: 'Status', contract: 'status' },
        { key: 'actions', title: 'Actions', contract: 'actions' },
      ], data: [{ id: 1, name: 'Node' }], rowKey: 'id' },
      slots: { card: ({ row }) => `<article>${row.name} card</article>` },
      global: { stubs: { NDataTable: { props: ['columns', 'data'], template: '<div data-testid="data-table">table</div>' } } },
    });
    expect(wrapper.get('[data-testid="data-table"]').exists()).toBe(true);
    media.matches = true;
    listeners.forEach((listener) => listener({ matches: true }));
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain('Node card');
    expect(wrapper.find('[data-testid="data-table"]').exists()).toBe(false);
    wrapper.unmount();
    expect(media.removeEventListener).toHaveBeenCalledOnce();
  });

  it('requires identity, status, and actions columns to remain in the responsive contract', () => {
    expect(() => mount(ResponsiveDataTable, {
      props: { columns: [{ key: 'details', title: 'Details', secondary: true }], data: [] },
      global: { stubs: { NDataTable: true } },
    })).toThrow(/identity, status, and actions/i);
  });

  it('keeps rendering the last valid columns when a runtime update is invalid', async () => {
    const columns = [
      { key: 'name', contract: 'identity' }, { key: 'status', contract: 'status' }, { key: 'actions', contract: 'actions' },
    ];
    const errorHandler = vi.fn();
    const wrapper = mount(ResponsiveDataTable, { props: { columns, data: [] }, global: { config: { errorHandler }, stubs: {
      NDataTable: { props: ['columns'], template: '<div data-testid="column-keys">{{ columns.map(column => column.key).join(",") }}</div>' },
    } } });
    await wrapper.setProps({ columns: [{ key: 'name', contract: 'identity' }] });
    expect(errorHandler.mock.calls[0][0].message).toMatch(/identity, status, and actions/i);
    expect(wrapper.get('[data-testid="column-keys"]').text()).toBe('name,status,actions');
  });

  it('rebuilds legacy media listeners when breakpoint changes and cleans them up', async () => {
    const first = { matches: false, addListener: vi.fn(), removeListener: vi.fn() };
    const second = { matches: true, addListener: vi.fn(), removeListener: vi.fn() };
    vi.spyOn(window, 'matchMedia').mockReturnValueOnce(first).mockReturnValueOnce(second);
    const wrapper = mount(ResponsiveDataTable, { props: { breakpoint: 800, columns: [
      { key: 'name', contract: 'identity' }, { key: 'status', contract: 'status' }, { key: 'actions', contract: 'actions' },
    ], data: [] }, global: { stubs: { NDataTable: true } } });
    await wrapper.setProps({ breakpoint: 640 });
    expect(first.removeListener).toHaveBeenCalledOnce();
    expect(second.addListener).toHaveBeenCalledOnce();
    wrapper.unmount();
    expect(second.removeListener).toHaveBeenCalledOnce();
  });

  it('falls back to the table on narrow screens without a card slot and forwards desktop slots', async () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() });
    const wrapper = mount(ResponsiveDataTable, { props: { columns: [
      { key: 'name', contract: 'identity' }, { key: 'status', contract: 'status' }, { key: 'actions', contract: 'actions' },
    ], data: [{ id: 1 }] }, slots: { empty: 'No rows' }, global: { stubs: { NDataTable: { template: '<div data-testid="data-table"><slot name="empty" /></div>' } } } });
    await wrapper.vm.$nextTick();
    expect(wrapper.get('[data-testid="data-table"]').text()).toContain('No rows');
  });
});

describe('ConfirmAction', () => {
  it('requires explicit confirmation, supports cancel, and returns focus to the trigger', async () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    const wrapper = mount(ConfirmAction, { attachTo: host, props: { title: 'Delete source?', confirmLabel: 'Delete', cancelLabel: 'Cancel' }, slots: { trigger: '<button>Delete source</button>' } });
    const trigger = wrapper.get('button');
    trigger.element.focus();
    await trigger.trigger('click');
    expect(wrapper.get('[role="dialog"]').text()).toContain('Delete source?');
    await wrapper.get('[data-action="cancel"]').trigger('click');
    expect(wrapper.emitted('cancel')).toHaveLength(1);
    expect(document.activeElement).toBe(trigger.element);
    await trigger.trigger('click');
    await wrapper.get('[data-action="confirm"]').trigger('click');
    expect(wrapper.emitted('confirm')).toHaveLength(1);
    expect(document.activeElement).toBe(trigger.element);
    wrapper.unmount();
    host.remove();
  });

  it('moves focus inside, describes the dialog, traps Tab, and cancels with Escape', async () => {
    const host = document.body.appendChild(document.createElement('div'));
    const wrapper = mount(ConfirmAction, { attachTo: host, props: { title: 'Delete?', description: 'This cannot be undone' }, slots: { trigger: '<button>Open</button>' } });
    const trigger = wrapper.get('button');
    await trigger.trigger('click');
    const dialog = wrapper.get('[role="dialog"]');
    expect(document.activeElement).toBe(wrapper.get('[data-action="cancel"]').element);
    expect(dialog.attributes('aria-describedby')).toBe(wrapper.get('.confirm-action__description').attributes('id'));
    await wrapper.get('[data-action="confirm"]').trigger('keydown', { key: 'Tab' });
    expect(document.activeElement).toBe(wrapper.get('[data-action="cancel"]').element);
    await wrapper.get('[data-action="cancel"]').trigger('keydown', { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(wrapper.get('[data-action="confirm"]').element);
    await dialog.trigger('keydown', { key: 'Escape' });
    expect(wrapper.emitted('cancel')).toHaveLength(1);
    expect(document.activeElement).toBe(trigger.element);
    wrapper.unmount(); host.remove();
  });

  it('supports confirm initial focus and emits confirm only once while pending', async () => {
    const host = document.body.appendChild(document.createElement('div'));
    const wrapper = mount(ConfirmAction, { attachTo: host, props: { title: 'Delete?', initialFocus: 'confirm' }, slots: { trigger: '<button>Open</button>' } });
    await wrapper.get('button').trigger('click');
    const confirm = wrapper.get('[data-action="confirm"]');
    expect(document.activeElement).toBe(confirm.element);
    await confirm.trigger('click');
    await confirm.trigger('click');
    expect(wrapper.emitted('confirm')).toHaveLength(1);
    expect(confirm.attributes('disabled')).toBeDefined();
    await wrapper.setProps({ pending: false });
    wrapper.unmount(); host.remove();
  });

  it('keeps cancel and Escape available when opened pending and traps Tab', async () => {
    const host = document.body.appendChild(document.createElement('div'));
    const wrapper = mount(ConfirmAction, { attachTo: host, props: { title: 'Deleting?', pending: true }, slots: { trigger: '<button>Open</button>' } });
    const trigger = wrapper.get('button');
    await trigger.trigger('click');
    const dialog = wrapper.get('[role="dialog"]');
    const cancel = wrapper.get('[data-action="cancel"]');
    expect(dialog.attributes('tabindex')).toBe('-1');
    expect(document.activeElement).toBe(cancel.element);
    expect(cancel.attributes('disabled')).toBeUndefined();
    expect(wrapper.get('[data-action="confirm"]').attributes('disabled')).toBeDefined();
    await cancel.trigger('keydown', { key: 'Tab' });
    expect(document.activeElement).toBe(cancel.element);
    await dialog.trigger('keydown', { key: 'Escape' });
    expect(wrapper.emitted('cancel')).toHaveLength(1);
    expect(document.activeElement).toBe(trigger.element);
    wrapper.unmount(); host.remove();
  });

  it('focuses the dialog and prevents Escape cancellation when cancel is disabled', async () => {
    const host = document.body.appendChild(document.createElement('div'));
    const wrapper = mount(ConfirmAction, { attachTo: host, props: { title: 'Deleting?', pending: true, cancelDisabled: true }, slots: { trigger: '<button>Open</button>' } });
    await wrapper.get('button').trigger('click');
    const dialog = wrapper.get('[role="dialog"]');
    expect(document.activeElement).toBe(dialog.element);
    await dialog.trigger('keydown', { key: 'Tab' });
    expect(document.activeElement).toBe(dialog.element);
    await dialog.trigger('keydown', { key: 'Escape' });
    expect(wrapper.emitted('cancel')).toBeUndefined();
    expect(document.activeElement).toBe(dialog.element);
    await wrapper.setProps({ pending: false, cancelDisabled: false });
    expect(document.activeElement).toBe(wrapper.get('[data-action="cancel"]').element);
    wrapper.unmount(); host.remove();
  });
});

import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import FormSelectorDialog from '../../src/components/FormSelectorDialog.vue';

const t = (key, params) => params?.name ? `${key}:${params.name}` : key;

const modalStub = { props: ['positiveButtonProps'], emits: ['positive-click', 'negative-click', 'close'], template: '<div><slot /><button data-action="confirm" :disabled="positiveButtonProps?.disabled" @click="$emit(\'positive-click\')">confirm</button><button data-action="cancel" @click="$emit(\'negative-click\')">cancel</button></div>' };
const checkboxStub = { props: ['checked'], emits: ['update:checked'], template: '<label><input type="checkbox" :checked="checked" @change="$emit(\'update:checked\', $event.target.checked)" /><slot /></label>' };

describe('FormSelectorDialog', () => {
  it('prefers availableForms over formFactor declarations', async () => {
    const wrapper = mount(FormSelectorDialog, {
      props: { show: true, tool: { name: 'opencode', displayName: 'OpenCode', formFactor: ['gui', 'cli', 'tui'], availableForms: ['cli'] }, t },
      global: { stubs: {
        Modal: modalStub,
        Checkbox: checkboxStub,
        Space: { template: '<div><slot /></div>' },
      } },
    });
    expect(wrapper.findAll('input')).toHaveLength(1);
    await wrapper.get('[data-action="confirm"]').trigger('click');
    expect(wrapper.emitted('confirm')).toEqual([['cli']]);
    wrapper.unmount();
  });

  it('selects the first form by default and emits the selected form', async () => {
    const wrapper = mount(FormSelectorDialog, {
      props: { show: true, tool: { name: 'opencode', displayName: 'OpenCode', formFactor: ['gui', 'cli', 'tui'] }, t },
      global: { stubs: {
        Modal: modalStub,
        Checkbox: checkboxStub,
        Space: { template: '<div><slot /></div>' },
      } },
    });
    expect(wrapper.findAll('input')[0].element.checked).toBe(true);
    await wrapper.get('[data-action="confirm"]').trigger('click');
    expect(wrapper.emitted('confirm')).toEqual([['gui']]);
  });

  it('allows only one selected form at a time', async () => {
    const wrapper = mount(FormSelectorDialog, {
      props: { show: true, tool: { name: 'opencode', formFactor: ['gui', 'cli'] }, t },
      global: { stubs: {
        Modal: { props: ['positiveButtonProps'], template: '<div><slot /><button data-action="confirm" @click="$emit(\'positive-click\')">confirm</button></div>' },
        Checkbox: checkboxStub,
        Space: { template: '<div><slot /></div>' },
      } },
    });
    const inputs = wrapper.findAll('input');
    await inputs[1].setValue(true);
    expect(inputs[0].element.checked).toBe(false);
    expect(inputs[1].element.checked).toBe(true);
    await wrapper.get('[data-action="confirm"]').trigger('click');
    expect(wrapper.emitted('confirm')).toEqual([['cli']]);
  });
});

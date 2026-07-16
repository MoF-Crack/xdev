import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import AIAssistantPage from '../../src/pages/AIAssistantPage.vue';

const t = (key) => key;

describe('AIAssistantPage', () => {
  it('emits suggest, diagnose, and chat including Ctrl+Enter', async () => {
    const wrapper = mount(AIAssistantPage, { props: { t, status: { available: true, url: 'https://ai.test', model: 'local' } } });
    await wrapper.get('[data-field="suggest-tool"]').setValue('node');
    await wrapper.get('[data-action="suggest"]').trigger('submit');
    await wrapper.get('[data-field="diagnose-problem"]').setValue('broken path');
    await wrapper.get('[data-action="diagnose"]').trigger('submit');
    await wrapper.get('[data-field="chat-message"]').setValue('help');
    await wrapper.get('[data-field="chat-message"]').trigger('keyup', { key: 'Enter', ctrlKey: true });
    expect(wrapper.emitted('suggest')[0]).toEqual(['node']);
    expect(wrapper.emitted('diagnose')[0]).toEqual(['broken path']);
    expect(wrapper.emitted('chat')[0]).toEqual(['help']);
  });

  it('renders long results and errors as copyable values', () => {
    const wrapper = mount(AIAssistantPage, { props: { t, suggestionResult: 'x'.repeat(200), diagnoseError: 'offline' } });
    expect(wrapper.findAllComponents({ name: 'CopyableValue' }).map((c) => c.props('value'))).toEqual(['x'.repeat(200), 'offline']);
  });
});

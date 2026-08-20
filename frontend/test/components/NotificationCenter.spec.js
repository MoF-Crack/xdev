import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import NotificationCenter from '../../src/components/NotificationCenter.vue';

const t = (key) => key;

describe('NotificationCenter', () => {
  it('clears the unread badge after the panel is opened and closed', async () => {
    const wrapper = mount(NotificationCenter, {
      props: {
        t,
        notifications: [{ id: 'n1', title: '嗅探完成', type: 'success', time: '12:00:00' }],
      },
    });
    expect(wrapper.vm.unreadCount).toBe(1);

    wrapper.vm.onShowChange(true);
    await wrapper.vm.$nextTick();
    expect(wrapper.vm.unreadCount).toBe(0);

    wrapper.vm.onShowChange(false);
    expect(wrapper.emitted('clear')).toHaveLength(1);
  });
});

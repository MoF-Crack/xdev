import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import ServicesPage from '../../src/pages/ServicesPage.vue';

const t = (key) => key;
const services = [
  { name: 'postgres', type: 'database', version: '16', status: 'running', port: 5432 },
  { name: 'redis', type: 'cache', version: '7', status: 'stopped', port: 6379 },
];

describe('ServicesPage', () => {
  it('renders status text and service metadata', () => {
    const wrapper = mount(ServicesPage, { props: { t, services } });
    expect(wrapper.text()).toContain('running');
    expect(wrapper.text()).toContain('16');
    expect(wrapper.text()).toContain('5432');
    expect(wrapper.findAll('.status-badge__icon')).toHaveLength(2);
  });

  it('emits refresh/start and confirms stop', async () => {
    const wrapper = mount(ServicesPage, { props: { t, services } });
    await wrapper.get('[data-action="refresh-services"]').trigger('click');
    await wrapper.get('[data-service="redis"] [data-action="start-service"]').trigger('click');
    await wrapper.get('[data-service="postgres"] [data-action="stop-service"]').trigger('click');
    expect(wrapper.emitted('stop')).toBeUndefined();
    await wrapper.get('[role="dialog"] [data-action="confirm"]').trigger('click');
    expect(wrapper.emitted('refresh')).toHaveLength(1);
    expect(wrapper.emitted('start')[0]).toEqual(['redis']);
    expect(wrapper.emitted('stop')[0]).toEqual(['postgres']);
  });

  it('isolates pending state by service name', () => {
    const wrapper = mount(ServicesPage, { props: { t, services, operating: { redis: true } } });
    expect(wrapper.get('[data-service="redis"] button').attributes('disabled')).toBeDefined();
    expect(wrapper.get('[data-service="postgres"] button').attributes('disabled')).toBeUndefined();
  });

  it('emits retry from its error state', async () => {
    const wrapper = mount(ServicesPage, { props: { t, error: 'offline' } });
    await wrapper.get('[role="alert"] button').trigger('click');
    expect(wrapper.emitted('retry')).toHaveLength(1);
  });
});

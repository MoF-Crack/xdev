import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import SelfUpdateDialog from '../../src/components/SelfUpdateDialog.vue';

const t = (key, params) => (params ? `${key}{${Object.values(params).join('/')}}` : key);
afterEach(() => { document.body.innerHTML = ''; });

describe('SelfUpdateDialog (Spec 034)', () => {
  // TC-S3-1 弹出：显示版本信息与三个动作按钮
  it('renders version info and three actions when shown', async () => {
    const wrapper = mount(SelfUpdateDialog, { props: { show: true, current: '0.3.3', latest: '0.4.0', t } });
    expect(wrapper.get('[data-testid="self-update-dialog"]').exists()).toBe(true);
    expect(wrapper.text()).toContain('0.3.3');
    expect(wrapper.text()).toContain('0.4.0');
    expect(wrapper.get('[data-action="self-update-update"]').exists()).toBe(true);
    expect(wrapper.get('[data-action="self-update-ignore"]').exists()).toBe(true);
    expect(wrapper.get('[data-action="self-update-quit"]').exists()).toBe(true);
  });

  it('is hidden when show=false', () => {
    const wrapper = mount(SelfUpdateDialog, { props: { show: false, current: '', latest: '', t } });
    expect(wrapper.find('[data-testid="self-update-dialog"]').exists()).toBe(false);
  });

  // TC-S3-2 更新
  it('emits update on update button', async () => {
    const wrapper = mount(SelfUpdateDialog, { props: { show: true, current: '0.3.3', latest: '0.4.0', t } });
    await wrapper.get('[data-action="self-update-update"]').trigger('click');
    expect(wrapper.emitted('update')).toHaveLength(1);
  });

  // TC-S3-3 忽略并继续
  it('emits ignore on ignore button', async () => {
    const wrapper = mount(SelfUpdateDialog, { props: { show: true, current: '0.3.3', latest: '0.4.0', t } });
    await wrapper.get('[data-action="self-update-ignore"]').trigger('click');
    expect(wrapper.emitted('ignore')).toHaveLength(1);
  });

  // TC-S3-4 退出
  it('emits quit on quit button', async () => {
    const wrapper = mount(SelfUpdateDialog, { props: { show: true, current: '0.3.3', latest: '0.4.0', t } });
    await wrapper.get('[data-action="self-update-quit"]').trigger('click');
    expect(wrapper.emitted('quit')).toHaveLength(1);
  });
});
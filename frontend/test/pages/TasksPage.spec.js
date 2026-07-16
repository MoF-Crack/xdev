import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import TasksPage from '../../src/pages/TasksPage.vue';

const t = (key) => key;
const tasks = { build: { run: 'npm run build', desc: 'Build app', dir: 'frontend' } };

describe('TasksPage', () => {
  it('renders task details and emits run', async () => {
    const wrapper = mount(TasksPage, { props: { t, tasks } });
    expect(wrapper.text()).toContain('npm run build');
    expect(wrapper.text()).toContain('Build app');
    expect(wrapper.text()).toContain('frontend');
    await wrapper.get('[data-action="run-task"]').trigger('click');
    expect(wrapper.emitted('run')[0]).toEqual(['build']);
  });

  it.each([
    ['running', 'tasks.status_running'],
    ['success', 'tasks.status_success'],
    ['error', 'tasks.status_failed'],
  ])('shows %s text status', (state, label) => {
    const wrapper = mount(TasksPage, { props: { t, tasks, taskStates: { build: state } } });
    expect(wrapper.text()).toContain(label);
  });
});

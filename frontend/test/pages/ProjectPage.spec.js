import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import ProjectPage from '../../src/pages/ProjectPage.vue';
const t = (key) => key;
const config = { hasConfig: true, dir: 'D:\\work\\demo', tools: { node: '20.18.0' }, env: { NODE_ENV: 'development' }, tasks: { build: { run: 'npm run build', desc: 'Build app', dir: 'frontend' } } };
const lockfile = { found: true, dir: 'D:\\work\\demo', tools: { node: { version: '20', resolved: '20.18.0', sha256: 'abcdef0123456789' } } };
describe('ProjectPage', () => {
  it('renders config sections and copyable values', () => { const w = mount(ProjectPage, { props: { t, config, lockfile } }); expect(w.text()).toContain('NODE_ENV'); expect(w.text()).toContain('npm run build'); expect(w.findAll('.copyable-value')).toHaveLength(4); });
  it('emits actions and confirms unset', async () => { const w=mount(ProjectPage,{props:{t,config,lockfile}}); await w.get('[data-field="project-tool"]').setValue('go'); await w.get('[data-field="project-version"]').setValue('1.24'); w.get('form').element.dispatchEvent(new Event('submit')); await w.vm.$nextTick(); await w.get('[data-action="unset-project-tool"]').trigger('click'); expect(w.emitted('unset-tool')).toBeUndefined(); await w.get('[role="dialog"] [data-action="confirm"]').trigger('click'); await w.get('[data-action="run-project-task"]').trigger('click'); expect(w.emitted('set-tool')[0]).toEqual(['go','1.24']); expect(w.emitted('unset-tool')[0]).toEqual(['node']); expect(w.emitted('run-task')[0]).toEqual(['build']); });
  it('exposes loading empty and project error states',()=>{expect(mount(ProjectPage,{props:{t,loading:true}}).get('[role="status"]').exists()).toBe(true);expect(mount(ProjectPage,{props:{t,config:{hasConfig:false}}}).text()).toContain('project.no_config');expect(mount(ProjectPage,{props:{t,error:'offline'}}).get('[role="alert"]').text()).toContain('offline')});
  it('shows and disables a running embedded task',()=>{const w=mount(ProjectPage,{props:{t,config,lockfile,taskStates:{build:'running'}}});expect(w.text()).toContain('tasks.status_running');expect(w.get('[data-action="run-project-task"]').attributes('disabled')).toBeDefined()});
});

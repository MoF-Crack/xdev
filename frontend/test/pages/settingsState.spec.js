import { describe, expect, it } from 'vitest';
import { createLatestRequestController, createMirrorTestState } from '../../src/pages/settingsState.js';

describe('mirror test state', () => {
  it('keeps the latest request when promises resolve out of order', async () => {
    let firstResolve; let secondResolve;
    const first = new Promise(resolve => { firstResolve = resolve; });
    const second = new Promise(resolve => { secondResolve = resolve; });
    const state = createMirrorTestState();
    const oldRun = state.run('node', 'https://old', () => first);
    const newRun = state.run('node', 'https://new', () => second);
    secondResolve({ success: true, latency: 2 }); await newRun;
    firstResolve({ success: true, latency: 99 }); await oldRun;
    expect(state.results.node).toMatchObject({ url: 'https://new', latency: 2 });
  });

  it('replaces an old success with the latest failure', async () => {
    const state = createMirrorTestState({ node: { success: true, url: 'https://node' } });
    await state.run('node', 'https://node', async () => { throw new Error('offline'); });
    expect(state.results.node).toMatchObject({ success: false, url: 'https://node' });
    expect(state.results.node.message).toContain('offline');
  });
});

describe('latest request controller', () => {
  it('keeps pending by key and makes stale failures silent', async () => {
    let rejectOld; let resolveNew; let resolveGo;
    const old = new Promise((_, reject) => { rejectOld = reject; });
    const latest = new Promise(resolve => { resolveNew = resolve; });
    const go = new Promise(resolve => { resolveGo = resolve; });
    const state = createLatestRequestController();
    const oldRun = state.run('node', { url: 'https://old' }, () => old);
    const newRun = state.run('node', { url: 'https://new' }, () => latest);
    const goRun = state.run('go', { url: 'https://go' }, () => go);
    rejectOld(new Error('stale')); await oldRun;
    expect(state.pending.node).toBe(true); expect(state.pending.go).toBe(true);
    resolveNew({ success: true }); resolveGo({ success: true });
    expect((await newRun).current).toBe(true); expect((await goRun).current).toBe(true);
    expect(state.pending).toEqual({});
  });

  it('uses an input snapshot and only returns the latest proxy result as current', async () => {
    let firstResolve; let secondResolve;
    const first = new Promise(resolve => { firstResolve = resolve; });
    const second = new Promise(resolve => { secondResolve = resolve; });
    const state = createLatestRequestController();
    const oldRun = state.run('proxy', { url: 'https://old' }, () => first);
    const newRun = state.run('proxy', { url: 'https://new' }, () => second);
    secondResolve('new'); expect(await newRun).toMatchObject({ current: true, input: { url: 'https://new' }, value: 'new' });
    firstResolve('old'); expect(await oldRun).toMatchObject({ current: false, input: { url: 'https://old' } });
  });
});

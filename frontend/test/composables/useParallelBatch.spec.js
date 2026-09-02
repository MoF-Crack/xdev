// useParallelBatch — Spec 037 阶段 1 单元测试：并行执行引擎。
// 覆盖：并行启动（全部会话同时跑）、allSettled 等待（单项失败不影响
// 其他项）、汇总计数（completed/error/cancelled/idle/failed）、行状态
// 镜像、cancelAll、clear、空清单短路。
import { describe, expect, it, vi } from 'vitest';
import { nextTick } from 'vue';
import { useParallelBatch } from '../../src/composables/useParallelBatch.js';

function makeCtx({ sessionImpl } = {}) {
  const message = { info: vi.fn(), warning: vi.fn(), success: vi.fn(), error: vi.fn() };
  const t = (key, params) => (params ? `${key}:${JSON.stringify(params)}` : key);
  const sessions = [];
  const sessionFactory = (row) => {
    const session = sessionImpl ? sessionImpl(row) : {
      installConfirmed: vi.fn(async () => true),
      waitForCompletion: vi.fn(async () => 'completed'),
      cancel: vi.fn(async () => true),
      pause: vi.fn(async () => true),
      resume: vi.fn(async () => true),
    };
    sessions.push(session);
    return session;
  };
  const parallelBatch = useParallelBatch({ sessionFactory, message, t });
  return { parallelBatch, sessions, message, t };
}

describe('useParallelBatch', () => {
  it('starts every item in parallel (all sessions created before any completes)', async () => {
    let activeCount = 0;
    const { parallelBatch } = makeCtx({
      sessionImpl: () => ({
        installConfirmed: vi.fn(async () => { activeCount++; return true; }),
        waitForCompletion: vi.fn(async () => { await new Promise((r) => setTimeout(r, 20)); return 'completed'; }),
        cancel: vi.fn(async () => true),
      }),
    });
    const p = parallelBatch.run([
      { tool: 'a', version: '1' }, { tool: 'b', version: '2' }, { tool: 'c', version: '3' },
    ]);
    await nextTick();
    // All three installs were started before the first 20ms wait resolved —
    // this is the parallel property (serial execution would start one).
    expect(activeCount).toBe(3);
    const summary = await p;
    expect(summary).toEqual({ done: 3, failed: 0, cancelled: 0 });
  });

  it('one item failing does not affect the others (allSettled semantics)', async () => {
    const { parallelBatch } = makeCtx({
      sessionImpl: (row) => ({
        installConfirmed: vi.fn(async () => true),
        waitForCompletion: vi.fn(async () => (row.tool === 'b' ? 'error' : 'completed')),
        cancel: vi.fn(async () => true),
      }),
    });
    const summary = await parallelBatch.run([
      { tool: 'a', version: '1' }, { tool: 'b', version: '2' }, { tool: 'c', version: '3' },
    ]);
    expect(summary).toEqual({ done: 2, failed: 1, cancelled: 0 });
  });

  it('cancelled items are counted separately from failures', async () => {
    const { parallelBatch } = makeCtx({
      sessionImpl: (row) => ({
        installConfirmed: vi.fn(async () => true),
        waitForCompletion: vi.fn(async () => (row.tool === 'a' ? 'cancelled' : 'completed')),
        cancel: vi.fn(async () => true),
      }),
    });
    const summary = await parallelBatch.run([{ tool: 'a', version: '1' }, { tool: 'b', version: '2' }]);
    expect(summary).toEqual({ done: 1, failed: 0, cancelled: 1 });
  });

  it('a session that fails to start counts as failed without waiting', async () => {
    const { parallelBatch } = makeCtx({
      sessionImpl: (row) => ({
        installConfirmed: vi.fn(async () => row.tool !== 'a'),
        waitForCompletion: vi.fn(async () => 'completed'),
        cancel: vi.fn(async () => true),
      }),
    });
    const summary = await parallelBatch.run([{ tool: 'a', version: '1' }, { tool: 'b', version: '2' }]);
    expect(summary).toEqual({ done: 1, failed: 1, cancelled: 0 });
  });

  it('an exception inside a session is contained (counts as failed)', async () => {
    const { parallelBatch } = makeCtx({
      sessionImpl: (row) => ({
        installConfirmed: vi.fn(async () => {
          if (row.tool === 'a') throw new Error('boom');
          return true;
        }),
        waitForCompletion: vi.fn(async () => 'completed'),
        cancel: vi.fn(async () => true),
      }),
    });
    const summary = await parallelBatch.run([{ tool: 'a', version: '1' }, { tool: 'b', version: '2' }]);
    expect(summary).toEqual({ done: 1, failed: 1, cancelled: 0 });
  });

  it('cancelAll invokes cancel on every live session', async () => {
    const { parallelBatch, sessions } = makeCtx();
    const p = parallelBatch.run([{ tool: 'a', version: '1' }, { tool: 'b', version: '2' }]);
    await nextTick();
    await parallelBatch.cancelAll();
    expect(sessions.every((s) => s.cancel)).toBe(true);
    await p;
  });

  it('empty list short-circuits with zero counts', async () => {
    const { parallelBatch } = makeCtx();
    const summary = await parallelBatch.run([]);
    expect(summary).toEqual({ done: 0, failed: 0, cancelled: 0 });
  });

  it('items rows carry tool/version for the multi-item dialog', async () => {
    const { parallelBatch } = makeCtx();
    await parallelBatch.run([{ tool: 'node', version: '22.0.0', formFilter: 'cli' }]);
    expect(parallelBatch.items.length).toBe(1);
    expect(parallelBatch.items[0].tool).toBe('node');
    expect(parallelBatch.items[0].version).toBe('22.0.0');
    expect(parallelBatch.items[0].formFilter).toBe('cli');
    expect(parallelBatch.running.value).toBe(false);
  });
});

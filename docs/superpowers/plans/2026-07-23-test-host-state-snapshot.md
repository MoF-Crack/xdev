# 测试宿主状态快照实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Go 测试套件提供套件级宿主状态快照检测，证明测试未污染用户持久环境，并在污染时失败。

**Architecture:** 新增 `internal/testkit/hostsnapshot` 包，通过 `TestMain`/`RunWithHostSnapshot` 在测试前后 `Capture`+`Diff`。默认只跟踪 xdev 已知污染面；`--no-path-fix` 仍为第一道防线。

**Tech Stack:** Go 1.23、`testing`、Windows `HKCU` 注册表 API、GitHub Actions（实现阶段）

## Global Constraints

- Spec 权威：`specs/005-test-host-state-snapshot/spec.md` `0.2.0`（**Approved**，`2026-07-23-spec-005-approved-001`）
- Constitution：`.specify/memory/constitution.md` `1.0.0` 原则 II
- 正式 Plan：`specs/005-test-host-state-snapshot/plan.md` `0.1.0`（**Ready**，`2026-07-23-plan-005-ready-001`）
- 不修改 `.github/workflows/ci.yaml` 直至实现授权明确包含 CI 变更（本轮可先以测试包默认启用满足 FR-004 本地证据）
- 不提交 `keys/priv.key`；禁止 `git add .` / `git add -A`
- Windows 主平台强制；Linux/macOS 分层证据
- 本文件与 `specs/005-.../plan.md` 保持任务清单同源；以 Spec 目录 `plan.md` 为 Plan 状态权威

---

## 前置门禁

- [x] **Clarify：** Spec 已澄清为 `0.2.0`（Constitution `1.0.0` 对齐、边界条件收敛）
- [x] **Gate S：** 用户原文「统一spec005 approve」→ Spec `Draft → Approved`（`2026-07-23-spec-005-approved-001`，commit `425cd23`）
- [x] **Plan 起草：** 正式 `plan.md` Planned（`2026-07-23-plan-005-planned-001`）
- [x] **Gate P：** 用户原文「同意方案并且实施」→ Plan `Planned → Ready`（`2026-07-23-plan-005-ready-001`）；授权 TDD 实现
- [x] 进入实现任务

---

## File Structure

| 文件 | 职责 |
|---|---|
| `internal/testkit/hostsnapshot/options.go` | 默认 env/registry/path 白名单与排除前缀 |
| `internal/testkit/hostsnapshot/hostsnapshot.go` | `Capture`/`Diff`/`RunWithHostSnapshot` |
| `internal/testkit/hostsnapshot/registry_windows.go` | Windows `HKCU` 采集 |
| `internal/testkit/hostsnapshot/registry_stub.go` | 非 Windows stub |
| `internal/testkit/hostsnapshot/hostsnapshot_test.go` | 单元测试 |
| `internal/integration/host_snapshot_testmain_test.go` 或各包 `TestMain` | 接入点 |
| `specs/005-test-host-state-snapshot/reviews/*` | 审查记录（实现后） |

---

### Task 1: Diff 与 Options 核心（TDD）

**Files:**
- Create: `internal/testkit/hostsnapshot/options.go`
- Create: `internal/testkit/hostsnapshot/hostsnapshot.go`
- Create: `internal/testkit/hostsnapshot/hostsnapshot_test.go`

**Interfaces:**
- Produces: `Options`, `Snapshot`, `Change`, `Diff`, `DefaultOptions`

- [ ] **Step 1: Write failing tests for Diff**

```go
func TestDiff_DetectsEnvChange(t *testing.T) {
    before := Snapshot{Env: map[string]string{"GOROOT": "A"}}
    after := Snapshot{Env: map[string]string{"GOROOT": "B"}}
    changes := Diff(before, after)
    if len(changes) != 1 || changes[0].Key != "GOROOT" {
        t.Fatalf("got %#v", changes)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/testkit/hostsnapshot -run TestDiff_DetectsEnvChange -count=1`
Expected: FAIL package/function undefined

- [ ] **Step 3: Minimal Diff + DefaultOptions implementation**

- [ ] **Step 4: Run tests PASS**

- [ ] **Step 5: Commit** (only after Gate S+P)

```bash
git add internal/testkit/hostsnapshot
git commit -m "test: add host snapshot diff core"
```

---

### Task 2: Capture 环境变量与路径

**Files:**
- Modify: `internal/testkit/hostsnapshot/hostsnapshot.go`
- Modify: `internal/testkit/hostsnapshot/hostsnapshot_test.go`

**Interfaces:**
- Produces: `Capture(opts Options) (Snapshot, error)`

- [ ] **Step 1: Failing test** — Capture reads current env keys from DefaultOptions
- [ ] **Step 2: Implement Capture for env + file existence/hash**
- [ ] **Step 3: Exclude temp prefixes and snapshot self path**
- [ ] **Step 4: PASS + commit**

---

### Task 3: Windows 注册表用户环境

**Files:**
- Create: `internal/testkit/hostsnapshot/registry_windows.go`
- Create: `internal/testkit/hostsnapshot/registry_stub.go`
- Modify: tests with `//go:build windows` probe helpers

- [ ] **Step 1: Failing Windows test reading HKCU\Environment GOROOT-style keys**
- [ ] **Step 2: Implement registry capture for whitelist only**
- [ ] **Step 3: Stub non-Windows**
- [ ] **Step 4: PASS + commit**

---

### Task 4: RunWithHostSnapshot 包装器

**Files:**
- Modify: `internal/testkit/hostsnapshot/hostsnapshot.go`
- Create: wrapper tests

```go
func RunWithHostSnapshot(m *testing.M, opts Options) int {
    // Capture before, code := m.Run(), Capture after, Diff, fail if changes
}
```

- [ ] **Step 1: Failing test with fake m.Run pollution callback if design allows; else manual probe package**
- [ ] **Step 2: Implement wrapper; print `host state snapshot: no changes detected` on clean**
- [ ] **Step 3: PASS + commit**

---

### Task 5: 接入 integration 套件 + 泄漏探针

**Files:**
- Modify: `internal/integration` TestMain or new test file
- Create: controlled leak probe (build tag `hostsnapshot_probe` 或独立测试函数默认跳过，CI 显式开启)

- [ ] **Step 1: Wire RunWithHostSnapshot into integration TestMain**
- [ ] **Step 2: Prove SC-002 — existing isolation tests still green**
- [ ] **Step 3: Prove SC-001 — intentional leak fails with structured report**
- [ ] **Step 4: Commit**

---

### Task 6: CI 与文档

**Files:**
- Modify: `.github/workflows/ci.yaml` only if authorized
- Update Spec reviews / acceptance notes

- [ ] **Step 1: Ensure CI does not set `XDEV_HOST_SNAPSHOT=0`**
- [ ] **Step 2: Document how to run probes**
- [ ] **Step 3: Dual independent reviews**
- [ ] **Step 4: User acceptance gate**

---

## Spec Coverage Checklist

| Spec item | Task |
|---|---|
| FR-001 Capture scope | Task 2-3 |
| FR-002 After capture + diff | Task 1, 4 |
| FR-003 Failure output | Task 1, 4 |
| FR-004 CI enable | Task 6 |
| FR-005 Cross-platform | Task 2-3 |
| FR-006 Default on / explicit off | Task 4 |
| FR-007 Configurable whitelist | Task 1 options |
| SC-001 Leak probe fails | Task 5 |
| SC-002 Clean suite passes | Task 5 |
| SC-003 CI failure on leak | Task 6 |
| SC-004 ≤5% overhead | Task 5 measure |

---

## Current Status (2026-07-23)

- Spec `0.2.0` **Approved** (`2026-07-23-spec-005-approved-001`)
- Plan `0.1.0` **Ready** (`2026-07-23-plan-005-ready-001`; auth text: 同意方案并且实施)
- Implementation **in progress** (TDD `internal/testkit/hostsnapshot`)

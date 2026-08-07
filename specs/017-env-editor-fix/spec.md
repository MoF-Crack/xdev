# Spec 017: 环境变量编辑器全面修复

> **状态**：待用户批准  
> **类型**：Bug 修复  
> **日期**：2026-07-31

## 问题清单

### P1: 编辑对话框打开后无法切换到其他页面

**根因**：`EnvVarEditorDialog` 的遮罩 `.env-modal{position:fixed;inset:0;z-index:3000}` 覆盖全屏。虽然 `v-if="show"` + `@update:show` 理论上能关闭，但：
- 对话框内部 `close()` emit `update:show=false` → 父组件 `showEnvEditor = $event` → `v-if` 移除
- 但 `onMenuSelect` 里 `showEnvEditor.value = false` 在 `selectedView.value = key` **之前**执行
- Vue 的响应式更新是异步的（nextTick），`v-if` 移除和 `selectedView` 变更在同一 tick
- 侧栏按钮的点击事件被遮罩拦截时，`onMenuSelect` **根本不会被调用**

**验证**：用户点击侧栏其他页面时，如果遮罩还在，点击被遮罩吃掉，`onMenuSelect` 不触发。只有 env-vars↔settings 之间切换可能因为某些布局巧合偶尔能穿透。

### P2: 编辑环境变量时有效路径未预加载

**根因**：`EnvVarEditorDialog` 的 `watch` 监听 `[props.show, props.editing]`。当 `v-if="showEnvEditor"` 在外层控制时：
- 打开编辑：`editEnvVar(row)` 设 `editingEnvVar.value = row` + `showEnvEditor.value = true`
- 外层 `v-if` 创建组件，传入 `:show="true"` + `:editing="row"`
- 组件内部 `watch` 触发，`resetPaths(props.editing?.value)` — **应该能工作**
- 但 `v-if` 外层 + `:show` 内层双重控制可能导致 `watch` 的 `[show, editing]` 数组比较在首次渲染时 props 尚未完全传入

**实际 bug**：`invalidPathSet()` 依赖 `props.editing.pathEntries`，但 `GetAllEnvVars` 对 **non-windows** 路径返回 nil pathEntries。更关键的是：`GetAllEnvVars` 在 windows 下确实调用了 `buildPathEntries`，但 `pathEntries` 只在值包含路径字符时才返回非 nil。如果值是纯路径（无分号），`buildPathEntries` 返回单个 entry——没问题。问题在于 `watch` 的触发时机和 `v-if` 外层控制的交互。

### P3: 页面切换时闪过终端窗口

**根因**：`app_updates.go:runXdevCommand` 和 `app_project.go` 的 `exec.Command` 调用缺少 `util.HideWindow(cmd)`。已在上一轮部分修复，但可能仍有遗漏的 exec 调用路径。

### P4: Path 变量被过度保护

**已部分修复**：`SetEnvVar`、`CleanEnvVarPath`、`CleanAllInvalidEnvVars`、底层 `env.SetEnvVar`、`env.DeleteEnvVar` 都已加 `!strings.EqualFold(name, "path")` 例外。`DeleteEnvVarEntry` 仍保持完全禁止删除 Path（正确行为）。

## 修复方案

### F1: 对话框生命周期重写

**放弃 `v-if` 外层 + `:show` 内层的双重控制**，改为单一控制：

`AppContent.vue`:
```html
<EnvVarEditorDialog :show="showEnvEditor" :editing="editingEnvVar" ... @update:show="onEditorShowChange" @save="saveEnvDraft" />
```

`EnvVarEditorDialog.vue`:
- 保留 `v-if="show"` 在内层（组件自身控制渲染）
- `close()` / `save()` emit `update:show=false`
- 父组件 `onEditorShowChange(val)` 设 `showEnvEditor.value = val`
- **不需要**外层 `v-if`

`onMenuSelect`:
- **移除** `showEnvEditor.value = false`（因为遮罩拦截点击，`onMenuSelect` 不会被调用）
- 改为：给遮罩设 `pointer-events: auto` 但侧栏设更高 `z-index` 且 `position: relative` 创建独立 stacking context

**更简单的方案**：把对话框和遮罩放在 `AppShell` 的 slot 内部（而非外部），这样遮罩只覆盖内容区，不覆盖侧栏。

### F2: 预加载修复

`EnvVarEditorDialog.vue` 的 `watch` 改为分别监听 `show` 和 `editing`：
```js
watch(() => props.show, async (visible) => {
  if (!visible) return;
  // 当 show 变 true 时，editing 应该已经被设置
  draft.value = props.editing ? {...} : {...};
  resetPaths(props.editing?.value || '');
  ...
});
```

不用数组 watch，避免 props 传入时序问题。

### F3: 终端闪烁

全面排查 `internal/gui/` 所有 `exec.Command` 调用，确保每处都有 `util.HideWindow(cmd)`。已修复的：
- `app_updates.go:runXdevCommand` ✅
- `app_project.go:activate/deactivate` ✅

仍需检查：
- `app_local_detect.go` 的所有 exec 调用（已有 HideWindow）
- `installer.go` 的 exec 调用
- `shim.go` 的 exec 调用

### F4: Path 保护

已修复，无需进一步改动。确认所有 5 处 `IsProtectedSystemEnvVar` 都加了 `!strings.EqualFold(name, "path")` 例外（除 `DeleteEnvVarEntry` 保持完全禁止）。

## 实施任务

### T1: 重写对话框生命周期
- `AppContent.vue`: 移除外层 `v-if`，改回 `:show` + `@update:show`
- `EnvVarEditorDialog.vue`: 保留内层 `v-if="show"`；`watch` 改为单独监听 `show`
- `onMenuSelect`: 移除 `showEnvEditor.value = false`（不再需要）
- 遮罩只覆盖内容区：把对话框移到 `AppShell` slot 内，或给遮罩设 `left: 216px`（侧栏宽度）

### T2: 修复预加载
- `watch(() => props.show)` 触发时读 `props.editing`
- 确保 `editingEnvVar` 在 `showEnvEditor = true` **之前**设置（已满足，`editEnvVar` 先设 editing 再设 show）

### T3: 终端闪烁排查
- 确认所有 `internal/gui/` 的 `exec.Command` 都有 `HideWindow`

### T4: 构建验证
- `go test ./internal/gui/`
- `wails build`
- 手动测试：打开编辑→切换页面→预加载→保存

## 验收

1. 打开环境变量编辑对话框 → 点侧栏仪表盘 → 正常切换，无遮罩残留
2. 编辑含有效+无效路径的变量 → 两种路径都正确加载显示
3. 环境变量↔设置切换无终端闪烁
4. 编辑 Path 可保存，一键清理不跳过 Path

## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-spec-017-draft-001`

# XDev 版本管理策略

> **文档类型**：治理策略
> **状态**：Active
> **创建日期**：2026-07-25
> **所有者**：xinyan

## Semver 规则

XDev 遵循 [语义化版本](https://semver.org/) 规范：

### 0.x.y 阶段（当前）

- `v0.x.y`：初始开发阶段，API 不保证稳定
- Minor（`v0.x.0`）：新功能、行为变更
- Patch（`v0.0.y`）：bug 修复、依赖升级、CI 改进

### 1.0.0+ 阶段（未来）

- `v1.0.0`：首个稳定版，公共 API 冻结
- Patch（`v1.0.y`）：向后兼容的 bug 修复
- Minor（`v1.x.0`）：向后兼容的新功能
- Major（`vx.0.0`）：破坏性变更

## Tag 规则

- 格式：`v<major>.<minor>.<patch>`（如 `v0.1.0`）
- 预发布：`v<major>.<minor>.<patch>-<label>`（如 `v1.0.0-rc.1`）
- Tag 指向 main 分支上通过 CI + Governance 的 commit

## Changelog 自动化

### 当前

GitHub Release 使用 `generate_release_notes: true` 自动生成基于 PR/commit 的 release notes。

### 未来改进（可选）

- 接入 [conventional-commits](https://www.conventionalcommits.org/) 规范
- 使用 `git-cliff` 或 `release-please` 从 commit message 生成结构化 changelog
- Changelog 分类：Features / Bug Fixes / Dependencies / CI/CD / Docs

## 发布流程

> **重要**：GitHub Actions 免费额度有限。以下规则用于避免额度耗尽。

### 提交规则

1. **本地提交（`git commit`）**：Agent 可自行执行，不需要用户审批。
2. **推送到 GitHub（`git push origin main`）**：**必须先获得用户明确同意**。
   Agent 不得自行 push。唯一例外：跟踪 GitHub Actions 运行进度时发现 CI 失败，
   且修复属于本次已批准工作范围内的直接关联问题，可自行提交并推送修复
   （不需再次审批），但必须在推送前说明修复内容。
3. **打 tag 触发发版（`git push origin v*`）**：**必须用户明确说"发布新版本"或"打 tag"**
   才能执行。Agent 不得自行决定发版。每次发版触发多平台编译（build + integration-tests
   + release + release-macos + release-arm64），消耗大量免费额度。

### 发版步骤

1. 确保用户已明确同意发布新版本
2. 确保 main 分支 CI + Governance 全绿
3. `git tag v<x.y.z> && git push origin v<x.y.z>`
4. GitHub Actions 自动构建 + 发布 release
5. 增量构建：只补齐 release 中缺失的产物

### 免费额度保护

- **不要频繁打 tag**：每个 tag 触发 5+ 个 job（build×3 + integration-tests×3 + release×3）
- **不要为测试打 tag**：如需测试 release 流程，使用 `dry-run` 或本地模拟
- **增量构建**：重新打同版本 tag 时，已有产物会被跳过，减少编译量
- **跟踪 CI**：打 tag 后用 `gh run watch` 跟踪进度，如发现失败及时修复避免浪费后续 job

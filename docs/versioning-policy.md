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

1. 确保 main 分支 CI + Governance 全绿
2. `git tag v<x.y.z> && git push origin v<x.y.z>`
3. GitHub Actions 自动构建 + 发布 release
4. 增量构建：只补齐 release 中缺失的产物

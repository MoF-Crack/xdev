# XDev Agent Guide

## 会话启动协议（Session Startup Protocol）

新会话开始时，按以下规则处理 handoff 文档：

1. **若 `HANDOFF.md` 存在**（仓库根）——启动时先读取，了解当前状态与待办，再响应用户请求。
2. **若 `HANDOFF.md` 不存在**——不主动创建；完成首个功能 Spec 的全流程收敛（LOOP 出口）后，在仓库根自动创建 `HANDOFF.md`，记录已完成 Spec、验证结果、遗留非阻塞项。
3. **功能收敛后自动更新**——每个功能实现完成全流程收敛（spec/plan/task/checklist/loop 全部通过）时，更新 `HANDOFF.md`：追加新完成 Spec、刷新当前状态与待办。
4. **git 忽略**——`HANDOFF.md` 已加入 `.gitignore`，不随仓库提交。注意：新 clone 或换机后本文件不存在（属预期），按第 2 条重新创建。
5. **本地备份分支（恢复点）**——`handoff-backup` 分支保存每个收敛状态的代码快照（与 HANDOFF.md 文字记录互补，解决会话挂起/上下文超限后的恢复）：
   - **时机**：每个 Batch 收敛（L2 验证门全绿）时提交一次；功能 Spec 全流程收敛（LOOP 出口）也提交一次
   - **操作**：`git add -A && git commit -m "<Spec> <Batch>: <验证结果摘要>"`（HANDOFF.md 被 .gitignore 忽略，不入库）
   - **恢复**：会话挂起/换机后，`git log --oneline handoff-backup` 查看恢复点时间线 → `git checkout <commit>` 或 `git checkout handoff-backup` 精确恢复代码快照；文字状态以 HANDOFF.md 为准，两相对照
   - **提交信息规范**：必须含 Spec 编号、Batch 编号、验证结果（如 `Spec 025 B1: go test 全绿 + wails build 通过`），使 `git log` 本身成为恢复点索引
   - 不 push（private 仓库本地备份即可）；若未来需要换机保护再 push 该分支

## Agent skills

### Issue tracker

Issues and specs live as local markdown files under `.scratch/<feature-slug>/`. See `docs/agents/issue-tracker.md`.

### Domain docs

Single-context: one `CONTEXT.md` at the repo root plus ADRs in `docs/adr/`. See `docs/agents/domain.md`.

# Spec 027 LOOP

> **状态**：待批准
> **前置**：Spec 027 + Plan + Task + Checklist 已获批
> **日期**：2026-08-05

## 批次划分

| Batch | Tasks | 内容 | 依赖 |
|-------|-------|------|------|
| B1 | T-A1, T-A2, T-A3 | Instances 类型 + 多实例检测 + MigratePackageManager | 串行（同文件） |
| B2 | T-B1 | 前端多实例列表 + 迁移按钮 | B1 API 定型 |
| B3 | T-C1, T-C2, T-C3 | 集成验证 | B1-B2 |

## LOOP 流程
- L1 Implement：我直接实施（Spec 024-026 教训：subagent 不可靠）
- L2 Verify：go build + go test -run "Instance|Migrate"
- L3 Review：B1 自审（ora 挂起教训）
- L4 收敛 → 下一 Batch

## 防挂起护栏
- subagent >15 分钟无结果 → 直接接手
- manifest 编辑用 Node.js（非 PowerShell）
- 只跑目标测试

## 全量收敛
1. `go test ./... -short` 无 FAIL
2. `npx vite build` + vitest
3. `wails build` 产出 xdev-gui.exe
4. e2e 含新用例
5. 手动场景：PIP Instances=[xdev pip, 系统 pip]；PIPX 迁移到 xdev 目录
6. 文档收尾

---

**审批请求**：请审批本 LOOP。获批后开始 B1 实施。
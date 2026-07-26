# 审查记录：T001-Spec 合规审查

> **文档类型**：审查记录
> **状态**：Accepted
> **文档版本**：`0.1.0`
> **创建日期**：2026-07-25
> **关联功能**：`specs/001-ci-go-version-consistency/`
> **审查者**：Independent Spec Reviewer (not implementer)

## 审查对象

| 字段 | 值 |
|---|---|
| 任务编号 | T001–T003 |
| 审查类型 | Spec 合规审查 |
| 审查范围 | `scripts/ci/check-go-version.sh`、`.github/workflows/ci.yaml` |
| 关联 Spec 版本 | `0.2.0` |

## 逐条判定

| 审查项 | 判定 | 证据与说明 |
|---|---|---|
| FR-001: CI 从 go.mod 提取 go 指令值 | 通过 | `scripts/ci/check-go-version.sh` 第 6 行使用 `go mod edit -json | python3 -c 'import sys,json; print(json.load(sys.stdin)["Go"])'` 提取权威版本，回退到 `grep '^go ' go.mod \| awk '{print $2}'` |
| FR-002: 所有 Go job 的 go-version 比对 | 通过 | 脚本第 7 行 `grep 'go-version:' .github/workflows/ci.yaml` 提取所有 job 的 go-version 声明并与权威版本比对 |
| FR-003: 不一致时明确失败并输出差异 | 通过 | 第 13-16 行：minor 级别比对失败时 `exit 1`，使用 `::error::` 输出 "Go version mismatch: go.mod requires $GOMOD_GO but CI uses $CI_GO" 和修复指引 |
| FR-004: 本地可用等效脚本 | 通过 | `scripts/ci/check-go-version.sh` 可直接在本地运行，无需 CI 环境 |
| FR-005: Minor 级别匹配 | 通过 | 第 10-11 行 `cut -d. -f1,2` 提取 major.minor 后比对，patch 版本差异不视为不一致 |
| 未超范围 | 通过 | 未修改 `go.mod` 内容、未修改 `setup-go` action、未引入浮动版本 |

## 验证证据

```text
# 一致时
$ bash scripts/ci/check-go-version.sh
Go version consistent: go.mod=1.25, CI=1.25

# 不一致时（模拟）
$ GOMOD_GO=1.24 CI_GO=1.23 bash -c '...'
::error::Go version mismatch: go.mod requires 1.24 but CI uses 1.23
::error::Fix: update go-version in .github/workflows/ci.yaml to match go.mod
```

## 结论

Spec 合规审查通过，所有功能需求已正确实现。

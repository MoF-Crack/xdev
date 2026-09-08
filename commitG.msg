ci/governance: 修复 actions/cache 版本（v7→v4）+ Spec 031 补 Constitution 引用

- actions/cache@v7 不存在（最新 v4；v7 是 checkout/setup-go 的版本）——
  上一轮 CI 全部 job 在 "Set up job" 3 秒即失败，根因即此
- validate-governance 的 cross-document 检查要求每个 spec 引用 Constitution
  版本；031 原始头部缺该行（其余 spec 均有）

本地验证: validate-governance 5/5 PASS（constitution/specs/approvals/links/
cross-document 全绿）
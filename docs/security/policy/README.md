# 安全政策

> **文档类型**：治理索引
> **状态**：Living
> **权威范围**：XDev 长期安全政策的存放和批准规则

## 规则

- Policy 描述必须长期成立的安全约束，不包含一次性操作步骤。
- Policy 不得放宽 Ratified Constitution。
- 新建或语义修改 Policy 必须经过独立安全审查和用户批准。
- 临时偏离 Policy 必须创建 `docs/security/exceptions/<exception-id>.md`。

## 计划迁入

- `download-source-policy.md`：下载来源、传输、重定向、校验和发布门禁。
- `signing-policy.md`：manifest 签名、根公钥、密钥角色和发布约束。该文件将在后续独立计划中创建。

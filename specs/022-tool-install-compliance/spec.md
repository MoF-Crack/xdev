# Spec 022: 工具安装流程与指令符合性审计

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.0.0
> **创建日期**：2026-08-02
> **最后更新**：2026-08-20
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：定义内置工具安装流程、环境变量与官方规范对齐的审计范围和修复验收
> **替代文档**：无
> **Constitution**：`1.0.0`
实施记录见同目录 `report.md`。本文件是活跃规格目录要求的 `spec.md` 权威入口。

## 目标

对照各工具官方安装规范，审计 `manifests/*.yaml` 的环境变量、bin 布局、安装方式和完整性字段，并修复已确认偏差。

## 范围

- P1 环境变量命名与白名单
- P2 安装流程（install_kind、错误平台 URL、多余 shim）
- P3 完整性与文档注释
- 不改变 Generic 插件注册模型，不引入新的工具类别

## 验收

1. `report.md` 中 P1–P3 项已实施或标明不做的理由
2. 相关 manifest 可被 catalog 加载
3. `go test ./internal/manifest ./internal/installer -short` 通过

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-20 | `1.0.0` | `2026-08-20-spec-022-approved-001` 补齐活跃目录 spec.md 元数据 |

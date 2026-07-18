> **文档类型**：Feature Spec（旧）
> **状态**：Archived（待迁移）
> **来源**：.trae/specs/enhance-gui-ux-and-fixes/spec.md
> **P4B 迁移目标**：specs/archive/2026/000-enhance-gui-ux-and-fixes/spec.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16

# GUI 体验增强与缺陷修复 Spec

## Why

当前 GUI 存在多项体验问题：环境变量管理埋在设置页面深处不易操作；迁移方式单一（仅 junction）；yarn/pnpm 检测误判；部分工具版本号获取失败；迁移工具显示 "local" 而非真实版本；缺少下载源连通性测试和关于页面；仪表盘信息不足；缓存策略粗放导致空间浪费。

## What Changes

- **前端页面拆分**：环境变量管理从 Settings 页面分离为独立页面，侧边栏新增导航项
- **迁移方式扩展**：新增"移动目录"迁移方式（将整个工具目录移动到 xdev install 目录下），与现有 junction 方式并列供用户选择
- **包管理器检测修复**：修复 `isInsideNodeDir` 兜底返回 `true` 导致 yarn/pnpm 误判的问题
- **版本号获取增强**：优化 `getVersion` 逻辑，增加更多工具的版本参数映射和输出解析规则
- **迁移工具版本显示**：迁移到 xdev 管理的工具标记为 `local` 来源，但版本号仍显示工具自身的真实版本
- **下载源连通性测试**：新增镜像源连通性测试 API，前端在镜像配置卡片中提供测试按钮
- **关于页面**：新增 About 页面，展示版本号、构建信息、项目链接等
- **仪表盘增强**：DashboardStats 新增已安装工具的版本信息列表
- **缓存策略优化**：实现已安装版本的缓存包自动清理 + 缓存大小分析 API

## Impact

- Affected specs: GUI 页面导航、工具检测、迁移功能、仪表盘数据
- Affected code:
  - `frontend/src/AppContent.vue` — 页面拆分、新增导航项、新增关于页面
  - `internal/gui/app_local_detect.go` — 版本号获取增强、包管理器检测修复
  - `internal/gui/app_tool_query.go` — `isInsideNodeDir` 修复
  - `internal/gui/app_migration.go` — 新增移动目录迁移方式
  - `internal/gui/app_config.go` — 新增镜像连通性测试
  - `internal/gui/app.go` — DashboardStats 扩展、缓存分析 API
  - `internal/gui/app_env_vars.go` — 无逻辑变更，仅前端调用方式变化
  - `frontend/src/i18n/locales/zh.ts` / `en.ts` — 新增翻译条目

## ADDED Requirements

### Requirement: 环境变量管理独立页面

系统 SHALL 将环境变量管理功能从 Settings 页面分离为独立页面，侧边栏新增"环境变量"导航项（图标 `TerminalOutline`），位于 Settings 之前。

#### Scenario: 用户从侧边栏进入环境变量页面
- **WHEN** 用户点击侧边栏"环境变量"导航项
- **THEN** 显示独立的环境变量管理页面，包含当前 Settings 中环境变量卡片的所有功能（开发/全部视图切换、变量表格、添加/编辑/删除操作、无效变量检测）

### Requirement: 移动目录迁移方式

系统 SHALL 在迁移功能中提供两种迁移方式供用户选择：
1. **Junction 方式**（现有）：创建符号链接指向原始目录，原始目录保留
2. **移动方式**（新增）：将整个工具目录移动到 xdev install 目录下，原始目录被移走

#### Scenario: 用户选择移动方式迁移工具
- **WHEN** 用户在迁移界面选择"移动"方式迁移某个工具
- **THEN** 系统将工具目录从原始位置移动到 `<installDir>/<toolName>/<version>/`，创建 junction 指向新位置，激活版本，创建 shim 文件

#### Scenario: 移动方式迁移失败回滚
- **WHEN** 移动过程中发生错误（如目标目录已存在、磁盘空间不足）
- **THEN** 系统尝试将目录移回原始位置，返回错误信息

### Requirement: yarn/pnpm 检测修复

系统 SHALL 修复 `isInsideNodeDir` 函数的误判问题：当路径包含 `\node\` 但向上遍历未找到 `node.exe`/`node` 时，应返回 `false` 而非 `true`。

#### Scenario: yarn 位于非 Node.js 安装目录
- **WHEN** `exec.LookPath("yarn")` 返回的路径包含 `\node\` 但该目录树中不存在 node 可执行文件
- **THEN** `isInsideNodeDir` 返回 `false`，yarn 被正常识别为独立安装的包管理器

#### Scenario: yarn 位于 Node.js 安装目录
- **WHEN** `exec.LookPath("yarn")` 返回的路径位于包含 `node.exe` 的 Node.js 安装目录中
- **THEN** `isInsideNodeDir` 返回 `true`，yarn 被标记为"随 Node.js 附带"

### Requirement: 版本号获取增强

系统 SHALL 扩展 `getVersion` 的版本参数映射和输出解析规则，确保更多工具能正确获取版本号。

#### Scenario: 工具版本输出格式非标准
- **WHEN** 工具的 `--version` 输出格式不包含标准 semver 模式（如 `cmake version 3.28.1`、`Make 4.4.1`）
- **THEN** `extractVersion` 能正确提取版本号 `3.28.1`、`4.4.1`

#### Scenario: 工具需要特殊版本参数
- **WHEN** 工具不支持 `--version` 但支持 `-version` 或 `version`（如 `gradle -version`、`make --version`）
- **THEN** `getVersion` 使用正确的参数获取版本号

### Requirement: 迁移工具版本显示

系统 SHALL 在迁移工具到 xdev 管理后，将工具标记为 `local` 来源，但版本号仍显示工具自身的真实版本信息而非 "local" 字符串。

#### Scenario: 迁移工具后查看版本
- **WHEN** 用户通过 junction 或移动方式迁移了一个本地工具（如 Python 3.11.5）
- **THEN** 工具列表中该工具的版本显示为 `3.11.5`，同时带有 `local` 来源标记（如标签或图标区分）

### Requirement: 下载源连通性测试

系统 SHALL 提供镜像源连通性测试 API，前端在镜像配置卡片中为每个镜像源提供测试按钮。

#### Scenario: 用户测试镜像源连通性
- **WHEN** 用户点击某个镜像源旁的"测试"按钮
- **THEN** 系统向该镜像源发送 HEAD 请求，返回连通状态（成功/失败）、HTTP 状态码和延迟毫秒数

#### Scenario: 镜像源不可达
- **WHEN** 镜像源连接超时（10 秒）或返回错误
- **THEN** 返回失败状态和错误信息

### Requirement: 关于页面

系统 SHALL 新增 About 页面，侧边栏底部新增"关于"导航项。

#### Scenario: 用户查看关于页面
- **WHEN** 用户点击侧边栏"关于"导航项
- **THEN** 显示关于页面，包含：应用名称、版本号、构建日期、Go 版本、Wails 版本、项目仓库链接、许可证信息

### Requirement: 仪表盘工具版本信息

系统 SHALL 在 DashboardStats 中新增已安装工具的版本信息列表，前端仪表盘展示各工具的当前版本。

#### Scenario: 用户查看仪表盘
- **WHEN** 用户打开仪表盘页面
- **THEN** 仪表盘除现有统计卡片外，新增"已安装工具"列表，显示每个已安装工具的名称和当前激活版本号

### Requirement: 缓存策略优化

系统 SHALL 实现缓存包自动清理和缓存内容分析功能。

#### Scenario: 已安装版本的缓存包自动清理
- **WHEN** 工具版本安装成功后
- **THEN** 系统自动删除该版本对应的缓存压缩包和 `.sha256` 文件（安装完成后不再需要）

#### Scenario: 用户查看缓存内容分析
- **WHEN** 用户在设置页面查看缓存信息
- **THEN** 系统返回缓存目录下各文件的分类统计（下载缓存包、SHA-256 文件、临时文件、扫描缓存），前端展示分类明细和清理按钮

## MODIFIED Requirements

### Requirement: 侧边栏导航结构

侧边栏导航项顺序变更为：Dashboard → 工具分类 → 环境变量 → 设置 → 关于

### Requirement: Settings 页面内容

Settings 页面移除环境变量管理卡片，其余设置项保持不变。

### Requirement: DashboardStats 结构体

`DashboardStats` 新增 `InstalledTools []InstalledToolInfo` 字段，其中 `InstalledToolInfo` 包含 `Name string`、`DisplayName string`、`Version string`、`Category string`。

## REMOVED Requirements

（无移除的需求）

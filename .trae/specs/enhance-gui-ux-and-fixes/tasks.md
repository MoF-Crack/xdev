# Tasks

- [x] Task 1: 修复 `isInsideNodeDir` 兜底误判 bug
  - [x] SubTask 1.1: 修改 `app_tool_query.go` 中 `isInsideNodeDir` 函数，向上遍历未找到 node.exe 时返回 `false` 而非 `true`
  - [x] SubTask 1.2: 增加单元测试验证修复：路径含 `\node\` 但无 node.exe 时返回 false

- [x] Task 2: 增强版本号获取逻辑
  - [x] SubTask 2.1: 扩展 `getVersion` 中版本参数映射，补充 cmake/make/gradle 等工具的正确参数
  - [x] SubTask 2.2: 增强 `extractVersion` 解析规则，处理更多前缀格式（如 `cmake version`、`GNU Make`）
  - [x] SubTask 2.3: 添加单元测试覆盖新增的版本提取场景

- [x] Task 3: 迁移工具版本显示修复
  - [x] SubTask 3.1: 修改 `MigrateLocalTool` 和 `MigrateLocalToolByJunction`，迁移时获取工具真实版本号而非使用 "local"
  - [x] SubTask 3.2: 在 `localInfo` 结构体中新增 `Source string` 字段（"xdev"/"local"），迁移工具标记为 "local"
  - [x] SubTask 3.3: 修改 `ToolInfo` 结构体，新增 `Source string` 字段传递到前端
  - [x] SubTask 3.4: 前端工具列表中为 local 来源的工具添加视觉标记（标签/图标）

- [x] Task 4: 新增移动目录迁移方式
  - [x] SubTask 4.1: 后端新增 `MigrateLocalToolByMove(toolJson string) error` 方法，将工具目录移动到 xdev install 目录
  - [x] SubTask 4.2: 实现移动失败回滚逻辑（移回原始位置）
  - [x] SubTask 4.3: 前端迁移界面添加迁移方式选择（Junction / 移动），默认 Junction
  - [x] SubTask 4.4: 更新 i18n 翻译条目

- [x] Task 5: 新增镜像源连通性测试 API
  - [x] SubTask 5.1: 后端新增 `TestMirrorConnectivity(mirrorURL string) ProxyTestResult` 方法
  - [x] SubTask 5.2: 前端镜像配置卡片中为每个镜像源添加"测试"按钮
  - [x] SubTask 5.3: 显示测试结果（成功/失败、延迟毫秒数）

- [x] Task 6: DashboardStats 扩展 — 新增已安装工具版本列表
  - [x] SubTask 6.1: 新增 `InstalledToolInfo` 结构体（Name, DisplayName, Version, Category）
  - [x] SubTask 6.2: `DashboardStats` 新增 `InstalledTools []InstalledToolInfo` 字段
  - [x] SubTask 6.3: `refreshDashboardStats` 中填充 InstalledTools 列表
  - [x] SubTask 6.4: 前端仪表盘新增"已安装工具"列表展示

- [x] Task 7: 缓存策略优化
  - [x] SubTask 7.1: 安装成功后自动删除对应缓存包和 `.sha256` 文件（修改 `app_install.go`）
  - [x] SubTask 7.2: 后端新增 `GetCacheBreakdown() CacheBreakdown` 方法，返回缓存分类统计
  - [x] SubTask 7.3: 后端新增 `CleanCache(category string) error` 方法，按分类清理缓存
  - [x] SubTask 7.4: 前端设置页面展示缓存分类明细和清理按钮

- [x] Task 8: 环境变量管理页面独立
  - [x] SubTask 8.1: 侧边栏新增"环境变量"导航项（图标 TerminalOutline），位于 Settings 之前
  - [x] SubTask 8.2: 将 Settings 中环境变量管理卡片的模板和逻辑提取为独立视图
  - [x] SubTask 8.3: Settings 页面移除环境变量管理卡片
  - [x] SubTask 8.4: 更新 i18n 翻译条目

- [x] Task 9: 新增关于页面
  - [x] SubTask 9.1: 后端新增 `GetAboutInfo() AboutInfo` 方法，返回版本号、构建日期、Go 版本、Wails 版本等信息
  - [x] SubTask 9.2: 侧边栏底部新增"关于"导航项
  - [x] SubTask 9.3: 前端实现关于页面，展示应用名称、版本、构建信息、项目链接
  - [x] SubTask 9.4: 更新 i18n 翻译条目

- [x] Task 10: 编译测试与验证
  - [x] SubTask 10.1: 运行 `go vet ./...` 和 `go test ./internal/...` 确保无警告和测试失败
  - [x] SubTask 10.2: 编译 CLI 和 GUI 二进制文件
  - [x] SubTask 10.3: 验证 Wails 绑定生成正确（TypeScript 类型更新）

# Task Dependencies

- [Task 3] depends on [Task 2] — 版本显示修复需要先增强版本号获取逻辑
- [Task 4] depends on [Task 3] — 移动迁移方式也需要正确获取版本号
- [Task 6] depends on [Task 3] — DashboardStats 扩展需要 localInfo.Source 字段
- [Task 10] depends on [Task 1-9] — 最终验证依赖所有功能完成
- [Task 1, 2, 5, 7, 8, 9] 可并行执行

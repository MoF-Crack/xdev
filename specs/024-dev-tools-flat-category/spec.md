# Spec 024: 开发工具扁平化分类与列表并行加载

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.0.0
> **创建日期**：2026-08-04
> **最后更新**：2026-08-20
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：开发工具扁平分类与 manifest 并行加载的目标、范围和验收
> **替代文档**：无
> **Constitution**：`1.0.0`
## 一、目标

### 1.1 需求 1：导航重构 — 合并分类为「开发工具」

当前侧栏导航的分类是扁平的（Spec 023 遗留）：

```
工具
  ├── 编程语言 (language)
  ├── 构建工具 (build)
  ├── IDE (ide)              ← Spec 023 新增
  ├── AI 代理 (agent)        ← Spec 023 新增
  ├── 命令工具 (harness)     ← Spec 023 新增（仅 copilot-cli）
  ├── 移动开发 (mobile)
  ├── 系统工具 (system)
  ├── 本地工具 (local)
  └── 包管理器 (package-manager)
```

用户反馈：

1. 「开发工具」下不分设二级菜单——取消 IDE/Agent/Harness 三个并列入口
2. 把当前被错误归到「构建工具」(build) 或「命令工具」(harness) 下的、本质属于 IDE/Agent 类的开发工具，迁移到一个合并后的扁平分类「开发工具」(development_tools)

**目标导航结构（扁平，无二级菜单）：**

```
工具
  ├── 编程语言 (language)
  ├── 构建工具 (build)             ← 只保留真正的构建工具
  ├── 开发工具 (development_tools) ← 合并 IDE + Agent + Harness
  ├── 移动开发 (mobile)
  ├── 系统工具 (system)
  ├── 本地工具 (local)
  └── 包管理器 (package-manager)
```

### 1.2 需求 2：列表加载效率优化 — manifest 分片并行读取

当前 `GetToolsByCategory` 是串行计算：

- `waitForLocalCache()`（最多 500ms 阻塞）
- 遍历 `registry.All()` 逐个调 `pluginToToolInfo` → `currentVersion` / `getLocalCached`
- 切换分类时整页重新加载，无预取

目标：

1. **后端**：`internal/manifest` 加载 catalog 时用 goroutine 并行读取+解析每个 yaml 文件（当前串行 `os.ReadFile` + `yaml.Unmarshal`）
2. **前端**：不引入并行预取（按需加载保持不变，避免 N 倍 RPC 负载）；前端分类列表切换体验依赖后端 `toolsCache`（60s TTL）兜底

## 二、现状（已核实）

### 2.1 分类系统现状

- `internal/gui/app_tool_query.go:86`：`order := []string{"language", "build", "ide", "agent", "harness", "mobile", "system", "local", "package-manager"}`
- manifest `category` 字段决定工具归属；`GetCategories()` 按 registry 统计 + 固定顺序输出
- 前端 `AppContent.vue:724` `dynamicToolPages` 把所有分类平铺注入到 `tools-group`
- `createNavigationGroups` 把所有动态页一股脑塞进 tools-group，无分组层级

### 2.2 当前各分类下的工具（manifest category 字段）

| category | 工具 |
|----------|------|
| `language` | cangjie, bun, dart, deno, dotnet, go, erlang, haskell, java, julia, kotlin, lua, node, perl, php, python, ruby, rust, scala, typescript, zig |
| `build` | cmake, git, gradle, llvm, make, maven, mingw, ninja, sbt |
| `ide` | clion, cursor, goland, jetbrains-idea-community, pycharm-community, rubymine, vscode, webstorm, zed |
| `agent` | claude-code, codex, opencode |
| `harness` | copilot-cli |
| `mobile` | android-build-tools, android-cmdline-tools, android-emulator, android-ndk, android-platform, android-platform-tools, flutter |
| `package-manager` | uv |
| `system` | （无 manifest，动态） |
| `local` | （运行时聚合） |

### 2.3 用户反馈的「错误归类」

用户指出：「被错误放在 build 菜单下的开发工具，比如 Claude code、opencode 等等」。

**核实结论**：

- `claude-code`、`codex`、`opencode` 当前 category 已经是 `agent`，**不在 build 下**——但 Spec 023 给它们分了一个独立的 `agent` 分类，用户希望并入「开发工具」扁平分类
- `copilot-cli` 当前 category 是 `harness`（Spec 023 决策），但它本质是个 AI 编码代理（与 claude-code 同类），应一并归入「开发工具」
- `build` 分类下的工具（cmake/git/gradle/llvm/make/maven/mingw/ninja/sbt）经核实均为合理的构建工具/工具链，**不在本次迁移范围**

**迁移决策**（Spec 024 范围）：

| 工具 | 旧 category | 新 category |
|------|-------------|-------------|
| vscode, cursor, zed, jetbrains-idea-community, pycharm-community, webstorm, clion, goland, rubymine | `ide` | `development_tools` |
| claude-code, codex, opencode | `agent` | `development_tools` |
| copilot-cli | `harness` | `development_tools` |

`build` / `language` / `mobile` / `package-manager` / `system` / `local` 分类不变。

### 2.4 加载链路现状

**后端启动加载：**

- `manifest.LoadCatalog(dir)` → `loadFromDir` 串行 `os.ReadFile + yaml.Unmarshal` 每个 yaml（约 50 个文件）
- `loadFromEmbedded` 串行 `fs.ReadFile + yaml.Unmarshal`
- `LoadCatalogCached` 有磁盘缓存（fingerprint 命中跳过解析），但缓存未命中时全量串行解析

**前端列表加载：**

- `loadCategories()` → `GetCategories()` 一次 RPC
- `onCategorySelect(key)` → `loadTools(key)` → `GetToolsByCategory(key)` 一次 RPC
- 切换分类 = 一次新的 RPC，**无预取**，用户每次点开都等 500ms+扫描

**缓存：**

- 后端 `toolsCache`（TTL 60s）已存在，命中快；但首次加载仍慢
- 前端无分类列表缓存，每次切换都重新请求

## 三、功能需求

### F1: 新分类 `development_tools`（扁平合并）

- manifest：把 13 个工具（9 IDE + 3 Agent + 1 Harness）的 `category` 字段改为 `development_tools`
- 后端 `GetCategories()`：
  - `order` 移除 `ide` / `agent` / `harness`，新增 `development_tools`
  - 顺序：`language / build / development_tools / mobile / system / local / package-manager`
  - 兼容性：仍处理可能残留的 `ide`/`agent`/`harness` category（磁盘覆盖 manifest 时旧值可能残留），归并到 `development_tools` 计数与列表
- i18n：
  - 新增 `category.development_tools`（zh: 「开发工具」/ en: 「Development Tools」）
  - 保留 `category.ide/agent/harness` keys 作为 fallback（避免历史 i18n 引用报错），但不在新分类列表中使用
- 前端导航：动态页自动注入 `development_tools` 项（沿用现有 `createNavigationGroups` 机制，无需改结构）

### F2: 兼容旧分类的归并层

为防止「磁盘 manifest 覆盖嵌入 manifest 时仍带旧 category」导致工具消失，后端需做归并：

- `GetCategories()`：把 registry 中 `category ∈ {ide, agent, harness}` 的工具计数累加到 `development_tools`
- `computeToolsByCategory("development_tools")`：返回 registry 中 `category ∈ {ide, agent, harness, development_tools}` 的全部工具
- `computeToolsByCategory("ide"/"agent"/"harness")`：仍可工作（向后兼容），但前端不再生成这些导航入口

这一归并层保证：即使 manifest 文件没改 category 字段，工具也能在「开发工具」下出现。manifest 字段调整是规范化的"应做"项，但归并层让迁移无断点风险。

### F3: 后端 manifest 并行加载

- `loadFromDir`：用 `errgroup` 并发读取+解析每个 yaml 文件（受 GOMAXPROCS 限制，或显式 semaphore 限制并发到 ~8）
- `loadFromEmbedded`：同样并行
- `loadMergedCatalog` 的 embedded 步骤并行、disk 步骤并行
- 错误语义保持：单文件解析失败不致命（embedded 步骤原本就 fail-fast，改为收集错误后返回第一个错误，保持外层语义）
- `LoadCatalogCached` 缓存命中路径不变（已快）
- 线程安全：每个 goroutine 写自己的 `Tool` 变量，最后合并到 map 时加锁或用 channel 收集

### F4: ~~前端并行预取分类列表~~（已移除）

**范围调整（2026-08-04）**：原计划的"前端 `GetCategories` 后并行预取多个分类列表"已移除，避免 N 倍 RPC 负载与前端 cache 一致性复杂度。前端按需加载（切换分类时 `GetToolsByCategory`）保持不变，体验由后端 `toolsCache`（60s TTL）+ 后端 manifest 并行加载兜底。

### F5: 清理遗留 i18n / 文档

- i18n `category.harness` 文案「命令工具」语义已不准确——保留 key 但改文案为「开发工具（旧）」或保留兼容，前端不再使用
- 不删除 `category.ide/agent/harness` keys（避免 e2e 或测试中残留引用报错）

## 四、范围边界

**做：**

- 13 个 manifest 的 category 字段调整（ide/agent/harness → development_tools）
- `GetCategories()` 顺序与归并层
- i18n 新增 + 保留旧 key
- 后端 manifest 并行加载
- 必要的测试调整与新增

**不做：**

- 不重构导航为多级折叠菜单（用户明确"不分设二级菜单"）
- 不调整 `build` / `language` / `mobile` 分类下的任何工具
- 不改 manifest 其他字段（form_factor / depends / shims 等保持不变）
- 不改 `ToolInfo` / `LaunchTool` / `CheckToolDependencies` 等 Spec 023 已完成的 API
- 不改 `ToolCatalogPage.vue` 的渲染逻辑（数据驱动，分类合并后自动生效）
- 不改 `package-manager` / `local` / `system` 的特殊处理分支
- **不做前端并行预取**（避免 N 倍 RPC 负载与前端 cache 一致性复杂度）；前端按需加载保持不变

## 五、验证

1. `go test ./internal/manifest/ ./internal/gui/ ./internal/cli/ -short -count=1` 通过
2. manifest 并行加载：解析结果与串行一致（用现有 50 个 manifest 做对比测试或断言 catalog 大小）
3. `GetCategories()`：返回 `development_tools` 分类，计数 = 13；不再返回 `ide`/`agent`/`harness`（归并层使其计数为 0 或不出现）
4. `GetToolsByCategory("development_tools")`：返回 13 个工具
5. 兼容性：临时把某个 manifest 改回 `category: ide`，`GetCategories` 仍归并到 `development_tools`，`GetToolsByCategory("development_tools")` 仍包含该工具
6. 前端 `npx vitest run` + `npx vite build` 通过（无并行预取相关测试）
7. e2e：`npx playwright test e2e/app.spec.js` 18/18 通过（分类入口名变化需同步 e2e fixture）
8. `wails build` 产出 xdev-gui.exe

## 六、风险

| 风险 | 缓解 |
|------|------|
| 磁盘覆盖 manifest 残留旧 category（用户 `$XMDEV_HOME/manifests/` 有自定义副本） | F2 归并层兜底；不依赖 manifest 字段必改 |
| 并行加载改变错误语义（fail-fast → 收集后返回） | 保持"返回第一个错误"语义；测试覆盖单文件损坏场景 |
| e2e 用例引用旧分类名 | 同步更新 e2e fixture（ide/agent/harness → development_tools） |
| 历史 i18n key 删除导致引用报错 | 保留旧 key，仅不使用 |
| 并行加载的 goroutine 泄漏 | errgroup + context，所有路径都合并结果 |
| 测试中 mock registry 用旧 category | 同步更新测试 fixture |

## 七、审批请求

请审批本 spec。获批后依次产出：

1. **plan**（Phase 划分：A 后端能力 + B 分类迁移 + C 集成验证；前端并行预取已移除）
2. **task**（细化到可执行单元）
3. **checklist**（逐项验收标准）
4. **loop**（实施循环与防挂起护栏）

每份文档单独获批才进入下一阶段，全部获批后开始实施。

## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-08 | `1.0.0` | `2026-08-20-spec-024-approved-001` 开发工具扁平分类与 manifest 并行加载的目标、范围和验收 |

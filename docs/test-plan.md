> **文档类型**：长期参考文档
> **状态**：Living
> **来源**：docs/test-plan.md
> **P4B 迁移目标**：原位保留
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16

# XDev 功能测试方案

> **范围**：对 xdev（CLI / TUI / GUI 三模式 + 38 个工具插件）进行系统性功能测试
> **基线**：当前已编译二进制 `build/bin/xdev.exe`（1.2.2-alpha），现有 31 个测试文件 / 332 个 `Test*` 函数（单元层）
> **目标**：在现有单元测试之上，补齐**集成测试**与**端到端（E2E）验收测试**两层，形成「单元 → 集成 → 验收」三层覆盖
> **平台**：Windows 10/11（主），macOS/Linux 跨平台为后续专项（本方案标注但不执行）

---

## 一、测试分层总览

| 层级 | 现状 | 本方案重点 | 工具 |
|------|:----:|-----------|------|
| **L1 单元测试** | ✅ 332 个函数，覆盖大部分 `internal/` 包 | 补缺口（见 §3）+ 回归保护 | `go test` |
| **L2 集成测试** | ⚠️ 几乎空白（仅个别 `_test.go` 触及文件系统） | **本次重点**：CLI 命令链路、安装工作流、Shim 调度、Lockfile 往返 | `go test` + build tag `integration` + `t.TempDir()` 沙箱 |
| **L3 端到端验收** | ❌ 无 | 真实二进制 + 真实网络（可选）跑通用户故事 | 脚本驱动 `xdev.exe` 子进程 + 断言输出 |
| **L4 GUI 手测** | ❌ 无 | 按 GUI 验收清单逐项核验 | 人工 + 可选 Playwright |

**原则**：
- 单元测试**不依赖网络、不依赖 Docker**；集成测试用 `--xmdevenv-dir <tmpdir>` 隔离，默认离线（用本地 fixture 包）。
- 涉及真实下载的用例打 build tag `online`，CI 默认跳过，手动/定时触发。
- 涉及 Docker 的用例打 build tag `docker`，仅本机有 Docker 时跑。

---

## 二、功能模块拆分（测试对象矩阵）

| 模块 | 包路径 | CLI 命令 | 关键风险 |
|------|--------|---------|---------|
| ① 配置 | `config` | `init` `config` | 配置迁移、默认值、损坏文件 |
| ② Manifest | `manifest` | `available` `search` `refresh` `manifest` | YAML 解析、嵌入 FS、签名验证 |
| ③ 版本解析 | `resolver` | （install 内部） | semver 范围、`latest`/`lts`、OR 逻辑 |
| ④ 下载 | `downloader` | `install` | 断点续传、SHA-256、镜像重写、重试 |
| ⑤ 解压 | `extractor` | `install` | Zip Slip 防护、7z 外部依赖 |
| ⑥ 安装工作流 | `installer` | `install` `uninstall` `use` | 8 步编排、PostInstall/Deactivate 钩子 |
| ⑦ Shim 调度 | `shim` | （隐式） | `argv[0]` 分发、索引失效、跨工具扫描 |
| ⑧ 版本切换 | `installer.Activate` | `use` | Junction 创建/删除、PATH 清理 |
| ⑨ 环境变量 | `env` | `env` | 注册表写入、广播、覆盖语义 |
| ⑩ 项目级 | `project` `lockfile` `task` | `local` `hook` `install --locked` `task` | `.xdev.toml` 向上查找、spec≠resolved |
| ⑪ 插件 | `plugin` (+Lua/vfox) | `plugin` | Lua 沙箱、vfox 兼容、Generic 委托 |
| ⑫ 服务编排 | `service` | `service` | 进程启动/停止、`.xdev-service` 标记发现 |
| ⑬ 隔离 | `isolator` | `isolate` | none/container/nix、Docker 可用性探测 |
| ⑭ 远程 | `orchestrator` | `remote` | docker/ssh adapter、容器生命周期 |
| ⑮ AI | `ai` | `ai` | Ollama 优雅降级、prompt 构造 |
| ⑯ 健康检查 | （doctor） | `doctor` | PATH 冲突检测、Junction 有效性 |
| ⑰ Profile | `gui.app_profile` | `profile` | 导出/导入往返、校验 |
| ⑱ 自更新 | `update` | `self update` | 版本比较、替换二进制 |
| ⑲ 签名 | `signer` | `keygen` `sign-manifests` | Ed25519 签名/验证 |

---

## 三、L1 单元测试补缺口

现有 332 个函数，但下列**公共包缺口**应补：

| 缺口 | 建议测试 | 优先级 |
|------|---------|:------:|
| `installer` Install 主流程无 happy-path 测试 | 用 fake Plugin（实现接口）跑通 resolve→extract→activate，断言 `InstallResult{Spec,URL,SHA256}` | 🔴 |
| `cli/install.go` `updateLockfile` | 构造 tmpdir + `.xdev.toml`，断言写入的 `.xdev.lock` 含 URL/SHA256 | 🔴 |
| `shim.Lookup` 跨工具扫描（npm→node） | 构造 current/ 目录树，断言别名解析 | 🟡 |
| `mirror.Apply` 边界（Adoptium/GitHub 跳过） | 表驱动：上游 vs 镜像 vs 跳过 | 🟡 |
| `extractor` Zip Slip 拒绝 | 构造恶意 zip（`../` 路径），断言报错 | 🟡 |
| `config` 损坏 YAML 恢复 | 写入非法 YAML，断言 `Load` 行为 | 🟢 |

---

## 四、L2 集成测试方案（本次重点）

用 `//go:build integration` 隔离，目录 `internal/integration/`。每个测试用 `t.TempDir()` 作为 `--xmdevenv-dir`，**完全隔离不污染用户环境**。

### 4.1 CLI 命令链路测试（`cli_integration_test.go`）

通过 `exec.Command(buildBin, "--xmdevenv-dir", tmpdir, ...)` 驱动真实二进制。

| 用例 ID | 场景 | 断言 |
|---------|------|------|
| CLI-01 | `xdev init` 全新目录 | 退出码 0；生成 config.yaml；目录结构齐全 |
| CLI-02 | `xdev init` 已存在目录 | 幂等，不报错 |
| CLI-03 | `xdev available` | 列出 ≥38 个工具；含 java/python/node |
| CLI-04 | `xdev search python` | 返回 python 相关；`search zzz` 返回空 |
| CLI-05 | `xdev doctor`（空环境） | 退出 0；报告无安装 |
| CLI-06 | `xdev config get/set/list` 往返 | set 后 get 一致 |
| CLI-07 | `xdev local node@20` 生成 `.xdev.toml` | 文件存在；`local --show` 输出含 node |
| CLI-08 | `xdev local --unset node` | 文件中 node 移除 |
| CLI-09 | `xdev hook --shell bash`（无 .xdev.toml） | 空输出，退出 0 |
| CLI-10 | `xdev task list`（无 .xdev.toml） | 友好提示，退出 0 |
| CLI-11 | 未知工具 `xdev install foo@1` | 非零退出 + 错误信息 |

### 4.2 安装工作流集成（`install_integration_test.go`）

**关键**：默认用离线 fixture（预置 zip 到 cache），仅 `//go:build online` 用例真实下载。

| 用例 ID | 场景 | build tag | 断言 |
|---------|------|:---------:|------|
| INST-01 | 离线安装（fixture 包） | integration | install/<tool>/<ver> 存在；current/<tool> Junction 指向它 |
| INST-02 | 安装后 `xdev use` 切换 | integration | current 重新指向；旧版本仍在 |
| INST-03 | `xdev uninstall` | integration | install 目录删除；current 移除 |
| INST-04 | `--no-activate` 安装 | integration | install 存在但无 current |
| INST-05 | 已安装版本重复 install | integration | 直接激活，不重复下载 |
| INST-06 | 安装后 `.xdev.lock` 生成（有 .xdev.toml） | integration | lock 含 resolved+url+sha256 |
| INST-07 | `xdev install --locked` 从 lockfile | integration | 用 resolved 精确版本，不解析 |
| INST-08 | `xdev install --from-local` 批量 | integration | 多工具安装 |
| INST-09 | 真实下载 node@lts | online | SHA-256 校验通过 |
| INST-10 | PostInstall 钩子触发 | integration | 钩子执行标志置位 |

### 4.3 Shim 调度集成（`shim_integration_test.go`）

| 用例 ID | 场景 | 断言 |
|---------|------|------|
| SHIM-01 | 安装后 shims/<tool>.exe 存在 | 文件存在 |
| SHIM-02 | 复制 shim 为别名调用 | 解析到 current/<tool>/<bin> |
| SHIM-03 | 跨工具扫描（npm→node） | Lookup 返回 node 路径 |
| SHIM-04 | 索引失效后重建 | 卸载后 Lookup 不再命中 |
| SHIM-05 | `xdev which <tool>` | 输出 current 路径 |

### 4.4 Lockfile 往返集成（`lockfile_integration_test.go`）

| 用例 ID | 场景 | 断言 |
|---------|------|------|
| LOCK-01 | install 生成 → 读取 → 内容一致 | spec≠resolved；url/sha256 非空 |
| LOCK-02 | 多次 install 累加工具 | tools map 增长 |
| LOCK-03 | `--locked` 缺 lockfile | 报错信息友好 |
| LOCK-04 | FindConfigDir 向上查找 | 子目录能找到父级 lock |

### 4.5 项目级配置集成（`project_integration_test.go`）

| 用例 ID | 场景 | 断言 |
|---------|------|------|
| PROJ-01 | `.xdev.toml` + `.tool-versions` 共存优先级 | `.xdev.toml` 优先 |
| PROJ-02 | `xdev hook` 输出 PATH/env | bash 输出含 export PATH |
| PROJ-03 | cd hook 脚本生成（不实际注入 RC） | 输出含 marker |
| PROJ-04 | task run 执行命令 | 退出码透传 |

### 4.6 服务/隔离/远程集成（带条件 build tag）

| 用例 ID | 场景 | tag | 断言 |
|---------|------|:---:|------|
| SVC-01 | `.xdev-service` 标记发现 | integration | List 返回该服务 |
| ISO-01 | `isolate --mode none setup/exec` | integration | exec 透传输出 |
| ISO-02 | `isolate --mode container` 无 Docker | integration | 友好报错 |
| ISO-03 | `isolate --mode container` 有 Docker | docker | 容器创建/执行/销毁 |
| REM-01 | `remote` 无 Docker | integration | 友好报错 |

---

## 五、L3 端到端验收测试（用户故事）

脚本化驱动，模拟真实用户完整流程。放 `tests/e2e/`。

### 故事 EPIC-1：新手首次使用
1. `xdev init` → `xdev doctor` → `xdev available` → 安装一个工具 → `xdev use` → `xdev current` → 验证 shim 生效 → `xdev uninstall`
2. **断言**：每步退出码 + 关键输出 + 副作用文件

### 故事 EPIC-2：多版本切换
1. 安装 java@17 + java@25 → `use 17` → `java -version` 显示 17 → `use 25` → 显示 25
2. **断言**：current/<tool> Junction 目标变化；JAVA_HOME 更新

### 故事 EPIC-3：团队 Lockfile 协作
1. 项目 A：`xdev local node@20` + `xdev install --from-local` → 生成 `.xdev.lock`
2. 模拟项目 B（拷贝 .xdev.lock）：`xdev install --locked` → 版本一致
3. **断言**：两环境 resolved 版本相同

### 故事 EPIC-4：迁移本地工具
1. 预置一个本地 Python → GUI/CLI 迁移 → 版本显示真实版本（非 "local"）
2. **断言**：localInfo.Source = "local"；版本号正确

### 故事 EPIC-5：隔离开发（Docker 可用时）
1. `xdev isolate --mode container setup` → `exec -- go version` → `teardown`
2. **断言**：容器创建、命令执行、销毁清理

---

## 六、L4 GUI 验收清单（手测 / 可选自动化）

逐项核验 Wails GUI，覆盖侧边栏全部视图：

### 6.1 导航与页面完整性
- [ ] 13 个 nav 项均可进入、不白屏：dashboard / favorites / 各 category / custom-tools / project / tasks / plugins / services / ai / isolate / remote / env-vars / settings / about
- [ ] 每个页面 headerTitle 正确（非原始 key）
- [ ] 非 tool 页面隐藏搜索栏（守卫逻辑）

### 6.2 安装与版本管理
- [ ] 工具分类列表加载、搜索过滤、排序
- [ ] 安装进度条、暂停/继续/取消
- [ ] 版本切换、卸载、收藏
- [ ] `local` 来源工具有视觉标记

### 6.3 项目配置面板（project 视图）
- [ ] 无 `.xdev.toml` 显示空状态提示
- [ ] tools/env/tasks 三个子卡片渲染
- [ ] Lockfile 卡片：刷新、install_all 按钮、SHA256 截断显示
- [ ] 添加/移除项目级工具

### 6.4 隔离面板（isolate 视图）
- [ ] Docker 可用性 tag 正确
- [ ] 模式下拉（none/container/nix）
- [ ] setup/teardown 按钮、exec 命令输入与输出区

### 6.5 服务/AI/远程/插件
- [ ] services：start/stop 按钮联动状态 tag
- [ ] ai：status 卡片、suggest/diagnose（无 Ollama 时优雅降级）
- [ ] remote：实例创建/销毁/exec
- [ ] plugins：内置 + Lua 列表、创建模板

### 6.6 设置与环境变量
- [ ] 镜像配置卡片含连通性测试按钮
- [ ] 缓存分类明细 + 清理按钮
- [ ] 环境变量独立页面 CRUD、无效变量检测
- [ ] 关于页面版本/构建信息

### 6.7 资源安全
- [ ] 快速切换页面不崩溃（并发刷新 CAS 防护）
- [ ] 关闭窗口时无 goroutine 泄漏

---

## 七、测试数据与隔离策略

### 7.1 沙箱主目录
所有集成/E2E 测试统一用**临时目录**作为 `--xmdevenv-dir`，禁止触碰真实 `$HOME/.xdev`：
```
tmp, _ := os.MkdirTemp("", "xdev-test-*")
defer os.RemoveAll(tmp)
```

### 7.2 离线 fixture
在 `testdata/fixtures/` 预置：
- `fake-tool-1.0.0.zip`（含 bin/fake.exe 的最小 zip）
- `fake.yaml` manifest（指向本地 file:// 归档）
- 恶意 zip（含 `../` 路径）用于 Zip Slip 测试

### 7.3 Build tag 约定
| tag | 含义 | CI 默认 |
|-----|------|:-------:|
| `integration` | 文件系统集成，离线 | 跑 |
| `online` | 需真实网络下载 | 跳过 |
| `docker` | 需 Docker daemon | 跳过 |

运行命令：
```
go test -tags integration ./internal/integration/...
go test -tags online ./internal/integration/...   # 手动
```

### 7.4 并发安全
- 同包测试可并行，但**共享 tmpdir 的用例串行**（用 `t.Setenv` 或独立子目录）。
- 注册表写入测试后必须清理用户 PATH（用 `--no-path-fix` 防止提示）。

---

## 八、回归与 CI 策略

| 阶段 | 命令 | 触发 |
|------|------|------|
| 快速（每次提交） | `go vet ./... && go test ./internal/...` | PR |
| 集成（合并前） | `go test -tags integration ./internal/integration/...` | PR + 手动 |
| E2E（发布前） | `tests/e2e/run.ps1` | tag 推送 |
| 在线（每日） | `go test -tags online ...`（GitHub Actions 定时） | cron |

### 覆盖率目标
- 单元层 ≥ 80%（`go test -cover`，对齐 `UNI_ENV_开发文档.md` 目标）
- 集成层：关键链路 100%（init/install/use/uninstall/local/lockfile/shim）
- 关注点：本次新增的 `InstallResult.Spec/URL/SHA256` 与 `updateLockfile` 必须有断言保护

---

## 九、执行顺序建议

1. **先补 L1 缺口**（§3 表，🔴 优先）—— `installer` happy-path + `updateLockfile`，保护本次 Lockfile 闭环改动。
2. **搭 L2 集成骨架** —— `internal/integration/` 目录 + fixture + build tag 约定。
3. **跑 CLI-01~11** —— 验证命令链路（多数无需网络）。
4. **跑 INST-01~08（离线）** —— 验证安装工作流（核心）。
5. **跑 SHIM/LOCK/PROJ 集成** —— 验证本次相关功能。
6. **L3 E2E 脚本** —— 选 EPIC-1/EPIC-3（新手 + Lockfile 协作）先做。
7. **L4 GUI 手测** —— 按 §6 清单逐项过。

---

## 十、验收标准

- [x] `go vet ./...` 零警告
- [x] `go test ./internal/...` 全绿（含新增）
- [x] `go test -tags integration ./internal/integration/...` 全绿
- [x] L3 EPIC-1、EPIC-3 已实现并验证（在线时；网络不可用时优雅 SKIP）
- [ ] L4 GUI 清单 §6.1–6.4 全勾
- [x] 覆盖率：关键链路（install/use/uninstall/lockfile/shim dispatch）均有断言

---

## 十一、运行指南与实现状态

### 实现状态（截至 2026-07-08）

| 层级 | 状态 | 文件 | 用例数 |
|------|:----:|------|:------:|
| **L1 单元** | ✅ 完成 | `installer/install_workflow_test.go`、`cli/install_lockfile_test.go`、`extractor/extractor_test.go`（追加）、`*_lock_test.go`（新增）、`*_install_many_test.go`（新增）、`*_dependency_test.go`（新增）、`*_required_plugin_test.go`（新增） | +22 |
| **L2 集成** | ✅ 完成 | `internal/integration/{client,cli,install}_*_test.go` | 19 |
| **L3 端到端** | ✅ 完成 | `internal/e2e/epic{1,3}_test.go` | 2 |
| **L4 GUI** | ⏳ 待办 | （手动清单见 §6） | — |

测试函数总数：332 → **426+**（含 22 新增单元测试 + 19 集成 + 2 在线 E2E，默认 build tag 隔离）。

### 新增单元测试清单（2026-07-08）

| 文件 | 测试数 | 覆盖功能 |
|------|:------:|---------|
| `internal/plugin/required_plugin_test.go` | 2 | Generic.RequiredPlugin 返回值 |
| `internal/manifest/required_plugin_test.go` | 2 | YAML 解析 required_plugins 字段 |
| `internal/cli/lock_test.go` | 5 | lockGenerate / lockCheck / 命令注册 |
| `internal/installer/install_many_test.go` | 4 | 批量安装：空/已装/不支持/混合 |
| `internal/installer/dependency_test.go` | 7 | 依赖检查：满足/缺失/不支持/被依赖/卸载 |

### 运行命令

```bash
# 默认（无 tag）：仅单元测试，零网络、零副作用
go vet ./...
go test ./internal/...

# 集成层：驱动真实二进制，离线（预置 install 目录模拟已安装）
go vet -tags integration ./...
go test -tags integration ./internal/integration/...

# 端到端（需网络 + build/bin/xdev.exe）：真实下载 + shim 调度验收
# 网络不可用时自动 SKIP，不致 CI 失败
go test -tags online ./internal/e2e/...
```

### 前置条件
- L2/L3 需要 `build/bin/xdev.exe`：先运行 `build.sh`（或 `go build -o build/bin/xdev.exe ./cmd/xmdevenvmt`）。
- L3 需要 `ninja@1.12.1` 可达（GitHub releases 或镜像）。网络中断时 EPIC 用例 SKIP 而非 FAIL。

### 设计要点
- **隔离**：所有 L2/L3 用例用 `t.TempDir()` 作 `--xmdevenv-dir` + `--no-path-fix`，不触碰宿主 shell RC / PATH 注册表（注：`use` 激活会写入指向 tmpdir 的用户级环境变量，无害残留）。
- **网络弹性**：L3 在线用例对下载失败走 `t.Skipf` 优雅降级，在线测试集退化而非变红。
- **Build tag 约定**：默认跑单元；`integration` 跑离线集成；`online` 跑真实下载；`docker`（预留）跑容器隔离。

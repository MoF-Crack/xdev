# Git 初始导入实施方案

## 目标

为现有 XDev 工程建立干净、可审查、可验证的本地 Git 历史，同时确保以下内容不会进入版本库：

- 私钥和其他敏感信息；
- 本地依赖目录；
- 构建产物和可执行文件；
- 下载及哈希校验缓存；
- 可重复生成的审计报告；
- 前端测试和构建输出。

本方案创建四个本地提交：

1. 建立仓库忽略规则；
2. 导入可构建、可测试的应用基线；
3. 导入项目文档和历史规格；
4. 导入 CI、OpenCode 和 Spec Kit 工作流。

创建远程仓库、配置 remote 和执行 `git push` 不在本方案范围内。

## 当前基线

所有命令均在以下目录执行：

```text
D:\Env\Android\ReverseTools\ReverseEng\xmdevenvmt
```

预期初始状态：

```text
分支：main
提交：无
远程仓库：无
Git 程序：D:\Env\Tools\Git\cmd\git.exe
全局配置：D:\Env\Config\Git\gitconfig
```

当前工作区包含大量未跟踪的现有源码和文档。这是一次初始导入，不是对已版本化仓库执行清理，因此不得将未跟踪文件视为可删除文件。

## 安全规则

- 导入期间禁止执行 `git add .`、`git add -A` 或 `git commit -a`。
- 只能暂存本方案明确列出的路径白名单。
- 永远不得暂存或提交 `keys/priv.key`。
- 本方案只建立忽略规则，不物理删除现有源码、构建产物、缓存或依赖目录。
- 不创建或配置远程仓库，不执行推送。
- 后续提交创建后，不回写或修改前序提交；如果门禁失败，应修复当前阶段后正常创建提交。
- 暂存区出现未知文件、敏感信息或非预期的 5 MiB 以上文件时必须停止。
- 必须保留 `.opencode/opencode.json` 中现有的插件配置。
- 不得使用 `git reset --hard`、`git clean -fd` 或 `git checkout --`。

## 提交结构

| 提交 | 目的 | 必须具备的证据 |
|---|---|---|
| 1 | 定义仓库边界 | 已知本地状态均被正确忽略 |
| 2 | 导入可构建应用 | Go 测试、vet、前端测试与构建、审计脚本自测通过 |
| 3 | 导入文档和历史规格 | 文档路径已审查，未包含生成的审计报告 |
| 4 | 导入 CI 和 SDD/OMO 工作流 | Constitution 已填写，Spec Kit 检查通过 |

## 阶段 0：执行前检查

### 步骤 0.1：加载持久化工具环境

使用新 PowerShell 进程，或在当前进程中加载用户级环境变量：

```powershell
$env:Path = [Environment]::GetEnvironmentVariable('Path', 'User') + ';' +
    [Environment]::GetEnvironmentVariable('Path', 'Machine')
$env:GIT_CONFIG_GLOBAL = [Environment]::GetEnvironmentVariable('GIT_CONFIG_GLOBAL', 'User')
$env:GIT_TEMPLATE_DIR = [Environment]::GetEnvironmentVariable('GIT_TEMPLATE_DIR', 'User')
```

执行验证：

```powershell
git --version
(Get-Command git).Source
git config --global --list --show-origin
```

通过条件：

- Git 解析到 `D:\Env\Tools\Git\cmd\git.exe`；
- 全局配置解析到 `D:\Env\Config\Git\gitconfig`；
- `user.name` 和 `user.email` 均已设置。

### 步骤 0.2：确认仓库身份

```powershell
git rev-parse --show-toplevel
git status --short --branch
git branch --show-current
git remote -v
git log --oneline -1 2>$null
```

通过条件：

- 仓库根目录为当前工程目录；
- 当前分支为 `main`；
- `git remote -v` 无输出；
- 仓库中尚无提交。

如果本方案编写后出现了提交或远程仓库，必须停止并重新审计仓库状态。

### 步骤 0.3：确认私钥隔离

```powershell
git check-ignore -v -- keys/priv.key
git status --short --untracked-files=all -- keys/priv.key
git rev-list --all --objects 2>$null | findstr /I /C:"keys/priv.key"
```

通过条件：

- `git check-ignore` 显示命中 `keys/priv.key` 规则；
- 后两个命令均不输出私钥路径。

如果私钥出现在暂存区、未跟踪列表或 Git 对象历史中，必须立即停止。在完成密钥事件处置前不得配置 remote。

## 阶段 1：建立仓库边界

### 步骤 1.1：扩展 `.gitignore`

保留全部现有规则，并添加：

```gitignore
# 生成的下载源审计报告
build/download-audit-*.json
build/download-audit-*.md

# 下载及哈希验证缓存
build/linkaudit-cache/

# 前端测试输出
frontend/test-results/
frontend/playwright-report/
frontend/blob-report/

# 自动生成的前端包哈希
frontend/package.json.md5
```

不得重复添加 `build/*.exe` 或 `linkaudit.exe`，现有 `*.exe` 已覆盖这些文件。

不得添加宽泛的 `build/*.md`。`build/manifest-changes-unsigned.md` 是 manifest 安全变更和未签名状态记录，必须保留提交资格。

### 步骤 1.2：验证忽略规则

使用 PowerShell 数组传递多路径，避免 Bash 风格续行符：

```powershell
$ignoredCandidates = @(
    'keys/priv.key',
    'frontend/node_modules',
    'frontend/dist',
    'frontend/test-results',
    'frontend/package.json.md5',
    'build/xdev-1.1.8.exe',
    'build/download-audit-cross-platform.json',
    'build/linkaudit-cache',
    'linkaudit.exe'
)
git check-ignore -v -- $ignoredCandidates
```

确认安全审计文档未被忽略：

```powershell
git check-ignore -q -- build/manifest-changes-unsigned.md
if ($LASTEXITCODE -eq 0) {
    throw 'build/manifest-changes-unsigned.md 不得被忽略'
}
```

列出剩余导入候选：

```powershell
git ls-files --others --exclude-standard
```

以下路径不应再出现在候选列表中：

```text
keys/priv.key
frontend/node_modules/
frontend/dist/
frontend/test-results/
frontend/package.json.md5
build/bin/
build/*.exe
build/download-audit-*.json
build/download-audit-*.md
build/linkaudit-cache/
vendor/
```

### 步骤 1.3：创建提交 1

```powershell
git add -- .gitignore
git diff --cached --check
git diff --cached --stat
git diff --cached -- .gitignore
git commit -m "chore: establish repository ignore rules"
git log --oneline -1
```

通过条件：

- 暂存区仅包含 `.gitignore`；
- `git diff --cached --check` 无空白错误；
- 提交创建成功。

## 阶段 2：导入应用基线

本提交只包含构建和测试 XDev 所需的源码、测试、manifest、签名、公钥和构建输入，不包含项目文档、CI 和 agent 工作流。

### 步骤 2.1：按白名单暂存应用

```powershell
$applicationPaths = @(
    'main.go',
    'main_bindings.go',
    'go.mod',
    'go.sum',
    'wails.json',
    'cmd',
    'internal',
    'frontend/src',
    'frontend/test',
    'frontend/e2e',
    'frontend/wailsjs',
    'frontend/embed.go',
    'frontend/index.html',
    'frontend/package.json',
    'frontend/package-lock.json',
    'frontend/vite.config.js',
    'frontend/playwright.config.js',
    'manifests',
    'keys/pub.key',
    'toolsHashFullCheck',
    'scripts',
    'build.ps1',
    'build.sh',
    'build/appicon.png',
    'build/windows',
    'build/manifest-changes-unsigned.md'
)
git add -- $applicationPaths
```

不得用仓库级 `git add` 替代该白名单。

### 步骤 2.2：审计暂存区

```powershell
git diff --cached --check
git diff --cached --stat
git diff --cached --name-only
git diff --cached --name-only | findstr /I /C:"keys/priv.key"
```

私钥查询必须无输出。

检查暂存文件大小：

```powershell
$large = foreach ($path in (git diff --cached --name-only)) {
    if (Test-Path -LiteralPath $path -PathType Leaf) {
        $item = Get-Item -LiteralPath $path
        if ($item.Length -gt 5MB) {
            [PSCustomObject]@{
                Path = $path
                MiB = [math]::Round($item.Length / 1MB, 2)
            }
        }
    }
}
$large | Format-Table -AutoSize
```

预期不出现未知的 5 MiB 以上文件。如果出现，应执行 `git restore --staged -- <path>` 将其移出暂存区，并判断其是构建产物还是必须提交的资产。

确认以下必要文件已经暂存：

```powershell
git diff --cached --name-only | findstr /I /C:"frontend/wailsjs/"
git diff --cached --name-only | findstr /I /C:"frontend/e2e/app.spec.js-snapshots/"
git diff --cached --name-only | findstr /I /C:"manifests/"
git diff --cached --name-only | findstr /I /C:"keys/pub.key"
git diff --cached --name-only | findstr /I /C:"build/windows/"
```

### 步骤 2.3：执行必要验证

在仓库根目录验证 Go：

```powershell
go test ./...
go vet ./...
```

执行不访问网络的下载审计逻辑自测：

```powershell
.\scripts\audit-links.ps1 -SelfTest
```

在 `frontend` 目录验证前端：

```powershell
npm test
npm run test:unit
npm run build
```

如果本机已安装 Playwright 浏览器依赖，再执行：

```powershell
npm run test:e2e
```

E2E 仅在失败原因明确为本地浏览器运行时缺失时可标记为非阻塞。任何应用断言失败都必须阻止提交。

完整下载源 audit 依赖外部站点状态，不作为初始本地提交的无条件门禁；如果执行，应单独记录结果。

### 步骤 2.4：创建提交 2

仅在必要验证全部通过后执行：

```powershell
git status --short
git diff --cached --stat
git commit -m "feat: import XDev application baseline"
git log --oneline -2
```

通过条件：

- 已包含应用源码、测试、manifest、签名、公钥和构建资源；
- 未包含私钥、依赖目录、二进制、缓存、下载审计报告和前端构建/测试输出；
- 当前执行中具备完整的必要验证证据。

## 阶段 3：导入项目文档和历史规格

### 步骤 3.1：暂存文档

```powershell
$documentationPaths = @(
    'README.md',
    'docs',
    'IMPROVEMENT-PLAN.md',
    'UNI_ENV_开发文档.md',
    '.trae'
)
git add -- $documentationPaths
```

`.trae/specs/` 作为历史规格保留。后续新增规格统一进入 Spec Kit 的 `specs/` 目录，避免长期维护两个活跃的规范源。

### 步骤 3.2：审查文档边界

```powershell
git diff --cached --check
git diff --cached --stat
git diff --cached --name-only
git diff --cached --name-only | findstr /I /C:"build/download-audit-"
```

生成的下载审计报告查询必须无输出。

提交前至少审查以下文档：

```text
README.md
docs/architecture.md
docs/security.md
docs/pending-work-plan.md
IMPROVEMENT-PLAN.md
.trae/specs/enhance-gui-ux-and-fixes/spec.md
```

如果引用的文档不存在，应记录差异，不得新增无关占位文件掩盖问题。

### 步骤 3.3：创建提交 3

```powershell
git commit -m "docs: import project documentation and historical specs"
git log --oneline -3
```

## 阶段 4：导入 CI 和 SDD/OMO 工作流

### 步骤 4.1：完成 Spec Kit Constitution

暂存 `.specify` 前，必须替换以下文件中的全部占位符：

```text
.specify/memory/constitution.md
```

Constitution 至少应定义：

- 项目范围：跨平台开发环境管理器；
- 安全边界：下载产物必须校验 checksum，执行文件和脚本安装器必须经过明确、可信的路由；
- 签名边界：私钥不得提交，公钥和运行时所需 manifest 签名必须版本化；
- 测试策略：行为变更需要聚焦测试，集成敏感变更需要集成覆盖；
- 跨平台策略：必须考虑 Windows、Linux 和 macOS 行为；
- 依赖策略：`go.mod`、`go.sum` 和前端 lockfile 是依赖事实来源，生成的依赖目录不进入版本库；
- SDD/LOOP 策略：实现必须对照活跃 spec 审查，不符合约束的工作必须回到实现循环，不能进入下一任务；
- 治理规则：Constitution 变更必须更新版本、修订日期并说明理由。

不得在 Constitution 中臆造产品需求，应从现有架构、安全文档、CI 和测试中提炼工程约束。

执行占位符扫描：

```powershell
$placeholders = Select-String -Path '.specify\memory\constitution.md' `
    -Pattern '\[[A-Z][A-Z0-9_]*\]'
$placeholders
if ($placeholders) {
    throw 'Spec Kit Constitution 仍包含占位符'
}
```

该检查是强制门禁。不得将空 Constitution 当成可运行的 SDD 约束层提交。

### 步骤 4.2：暂存工作流配置

```powershell
$workflowPaths = @(
    '.github',
    '.opencode/opencode.json',
    '.opencode/tui.json',
    '.opencode/commands',
    '.specify'
)
git add -- $workflowPaths
```

不得强制添加 `.opencode/.gitignore` 已忽略的本地文件：

```text
.opencode/node_modules/
.opencode/package.json
.opencode/package-lock.json
.opencode/.gitignore
```

### 步骤 4.3：验证 CI 和工作流

```powershell
specify check
git diff --cached --check
git diff --cached --stat
git diff --cached --name-only
```

确认 OpenCode 配置未被覆盖：

```powershell
$config = Get-Content -LiteralPath '.opencode\opencode.json' -Raw
if ($config -notmatch 'opencode-visual-cache') {
    throw '现有 OpenCode 插件配置已丢失'
}
```

确认 10 个 Spec Kit 命令完整：

```powershell
$commands = Get-ChildItem -LiteralPath '.opencode\commands' `
    -Filter 'speckit.*.md' -File
if ($commands.Count -ne 10) {
    throw "预期 10 个 Spec Kit 命令，实际找到 $($commands.Count) 个"
}
```

提交前必须审查 `.github/workflows/ci.yaml`。当前 CI 使用 Go `1.22`，而 `go.mod` 声明了工程 Go 版本要求；在将 CI 视为权威门禁前必须解决版本不一致。

### 步骤 4.4：创建提交 4

```powershell
git commit -m "chore: add CI and Spec Kit workflow"
git log --oneline --decorate -4
```

## 阶段 5：最终仓库审计

### 步骤 5.1：检查四提交结构

```powershell
git log --oneline --reverse
git status --short --branch
```

预期顺序：

```text
chore: establish repository ignore rules
feat: import XDev application baseline
docs: import project documentation and historical specs
chore: add CI and Spec Kit workflow
```

除明确保留的 ignored 文件和执行期间用户并发产生的变更外，工作区应保持干净。

### 步骤 5.2：审计禁止跟踪的路径

```powershell
$forbidden = @(
    'keys/priv.key',
    'frontend/node_modules/',
    'frontend/dist/',
    'frontend/test-results/',
    'build/bin/',
    'build/linkaudit-cache/',
    '.opencode/node_modules/',
    'vendor/'
)

$tracked = @(git ls-files)
$violations = foreach ($prefix in $forbidden) {
    $tracked | Where-Object {
        $_ -eq $prefix.TrimEnd('/') -or $_.StartsWith($prefix)
    }
}
if ($violations) {
    $violations
    throw '发现禁止跟踪的路径'
}
```

确认生成的下载审计报告未被跟踪：

```powershell
$auditReports = @(git ls-files 'build/download-audit-*')
if ($auditReports) {
    $auditReports
    throw '生成的下载审计报告不得被跟踪'
}
```

确认必要资产已被跟踪：

```powershell
$requiredAssets = @(
    'keys/pub.key',
    'manifests/*.sig',
    'frontend/wailsjs/**',
    'frontend/e2e/app.spec.js-snapshots/**',
    'build/windows/**',
    '.specify/memory/constitution.md'
)
git ls-files -- $requiredAssets
```

### 步骤 5.3：重新检查私钥历史

```powershell
git log --all --full-history -- keys/priv.key
git rev-list --all --objects | findstr /I /C:"keys/priv.key"
```

配置任何远程仓库前，上述命令均不得输出私钥路径。

### 步骤 5.4：记录最终证据

```powershell
git status --short --branch
git log --oneline --decorate -4
git count-objects -vH
```

记录已执行的验证命令、结果，以及被明确跳过的可选 E2E 或网络检查。

## 失败与恢复

### 提交前

从暂存区移除当前路径，但保留工作区文件：

```powershell
git restore --staged -- <path>
```

在仓库尚无 `HEAD` 的首次提交前，如果 `git restore --staged` 不可用，使用：

```powershell
git rm --cached -- <path>
```

禁止使用会破坏工作区内容的命令。

### 提交后

如果已完成的提交遗漏了非敏感文件，应将其纳入下一个逻辑提交，或创建专门的补充提交。除非用户明确要求，不得 amend。

如果敏感信息被提交，必须立即停止，不得配置 remote 或推送。历史清理和密钥轮换应另行制定事件响应方案。

## 延后决策

以下事项在本地初始导入完成后另行处理：

- 创建和配置远程仓库；
- 推送 `main`；
- 选择永久 Git 代理策略；
- 物理删除已忽略的构建产物、缓存和依赖目录；
- 将 `build/manifest-changes-unsigned.md` 迁移到 `docs/security/`；
- 决定 OpenCode 本地 `package.json` 和 lockfile 是否应成为团队级可复现输入；
- 将历史 `.trae` specs 迁移到 Spec Kit 的 `specs/` 目录。

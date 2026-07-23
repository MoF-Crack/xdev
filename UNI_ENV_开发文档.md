# UNI-ENV 项目开发文档

> **版本**: 0.1.0 (草案)  
> **状态**: 规划阶段
> ⚠️ **声明**：本文档描述的是对标竞品 uni-env 的设计愿景，用于指导 xdev 的 isolator/orchestrator 模块演进，非 xdev 自身的功能规格文档。xdev 的实际功能以代码和 README 为准。
> **最后更新**: 2026-06-15  

---

## 目录

1. [项目概述](#1-项目概述)  
2. [核心设计原则](#2-核心设计原则)  
3. [架构总览](#3-架构总览)  
4. [模块说明](#4-模块说明)  
5. [统一环境定义 (\`env.uni.yaml\`) 规范](#5-统一环境定义-envuniyaml-规范)  
6. [命令行接口 (CLI) 参考](#6-命令行接口-cli-参考)  
7. [Adapter 接口规范](#7-adapter-接口规范)  
8. [Lockfile 格式](#8-lockfile-格式)  
9. [贡献指南](#9-贡献指南)  
10. [许可证](#10-许可证)  

---

## 1. 项目概述

**uni-env** 是一个开源的开发环境管理工具，旨在统一并超越现有工具（mise、asdf、devContainer、Nix、DevPod、Coder 等）的优缺点，提供一种**渐进式、可插拔、声明式**的环境定义与管理体验。

### 核心目标

- **统一声明**：一份 \`env.uni.yaml\` 文件即可描述工具版本、环境变量、容器配置、生命周期钩子、团队策略。
- **渐进隔离**：支持三种隔离模式（none / container / nix），用户可根据项目复杂度自由选择，无需重构。
- **可重现性**：通过 lockfile + digest pinning 实现确定性环境，避免"在我机器能跑"。
- **后端无关**：Orchestrator 层通过 Adapter 模式支持本地 Docker、Kubernetes、SSH 裸机、云实例等。
- **团队就绪**：模板继承、策略约束、自动停机等特性面向协作场景。

### 适用场景

- 个人开发者：快速切换多语言版本，零容器开销。
- 中小团队：统一开发环境，消除"环境不一致"问题。
- 企业：自托管远程开发平台，满足合规与审计需求。

---

## 2. 核心设计原则

1. **渐进复杂度**：用户可以从最简单的 \`mode: none\` 开始，随着项目成长逐步升级到 \`container\` 甚至 \`nix\`，无需重写配置。
2. **兼容而非替代**：不强制抛弃现有工具，而是兼容 \`.tool-versions\`、\`.nvmrc\`、\`devcontainer.json\` 等格式，并提供 \`import\` 命令。
3. **约定优于配置**：合理的默认值（如自动检测 \`package.json\` 中的 \`engines\`）减少样板代码。
4. **安全默认**：lockfile 强制哈希校验，镜像必须指定 digest，防止供应链攻击。
5. **可审计**：所有环境变更记录在 lockfile 和日志中，支持回滚。

---

## 3. 架构总览

\`\`\`
┌─────────────────────────────────────────────────────────────────┐
│                         CLI (uni)                               │
│          parse flags → dispatch subcommand → orchestrate        │
└───────────────────────────┬─────────────────────────────────────┘
                            │
                    ┌───────▼────────┐
                    │   Core Engine  │
                    │  (解析 env.uni │
                    │   .yaml, 调度) │
                    └───────┬────────┘
                            │
          ┌─────────────────┼──────────────────┐
          ▼                 ▼                  ▼
   ┌────────────┐   ┌──────────────┐   ┌──────────────┐
   │  Resolver  │   │  Isolator    │   │ Orchestrator │
   │  (mise-    │   │  (Docker/    │   │  (Adapter    │
   │   core)    │   │   Nix/none)  │   │   Manager)   │
   └────────────┘   └──────────────┘   └──────┬───────┘
                                              │
                          ┌───────────────────┼───────────────────┐
                          ▼                   ▼                   ▼
                   ┌────────────┐    ┌──────────────┐    ┌──────────────┐
                   │ Docker     │    │ K8s Adapter  │    │ SSH Adapter  │
                   │ Adapter    │    │              │    │              │
                   └────────────┘    └──────────────┘    └──────────────┘
\`\`\`

### 数据流示例 (\`uni up\`)

1. CLI 解析参数 → 读取当前目录下的 \`env.uni.yaml\`（若无则扫描 \`.tool-versions\` 等自动生成）。
2. Core Engine 合并继承的模板（\`inherit:\` 字段），生成最终配置。
3. Resolver 根据 \`tools:\` 段调用 mise-core 下载/链接所需版本，生成 lockfile。
4. Isolator 根据 \`isolation.mode\` 执行对应逻辑：
   - \`none\`: 直接将 Resolver 产出的 bin 目录添加到 PATH，设置环境变量。
   - \`container\`: 生成临时 \`devcontainer.json\`，调用 Docker Adapter 启动容器。
   - \`nix\`: 生成 \`flake.nix\`，调用 \`nix develop\` 进入 shell。
5. Orchestrator 执行 \`hooks.postStart\`，等待服务就绪。
6. 输出连接信息（如容器 ID、端口映射、编辑器命令）。

---

## 4. 模块说明

### 4.1 \`uni\` CLI

- 语言：Go（兼顾跨平台编译与性能）
- 入口：\`cmd/uni/main.go\`
- 子命令：
  - \`up\` — 创建/启动环境
  - \`down\` — 停止/销毁环境
  - \`status\` — 显示当前环境状态
  - \`exec <cmd>\` — 在环境中执行命令
  - \`import <path>\` — 从 \`.tool-versions\` / \`devcontainer.json\` / \`flake.nix\` 生成 \`env.uni.yaml\`
  - \`config\` — 查看/编辑当前配置
  - \`template\` — 管理团队模板（list / pull / push）

### 4.2 Core Engine (\`internal/core/\`)

- 职责：YAML 解析、模板合并、策略校验、生命周期调度。
- 关键数据结构：
  \`\`\`go
  type Environment struct {
      Name      string             \`yaml:"name"\`
      Inherit   []string           \`yaml:"inherit"\`
      Tools     map[string]string  \`yaml:"tools"\`
      Env       map[string]string  \`yaml:"env"\`
      Isolation IsolationConfig    \`yaml:"isolation"\`
      Hooks     Hooks              \`yaml:"hooks"\`
      Policy    *Policy            \`yaml:"policy,omitempty"\`
  }
  \`\`\`

### 4.3 Resolver (\`internal/resolver/\`)

- 封装 mise-core 作为库调用（或通过 gRPC 通信）。
- 支持插件式 backend：mise（默认）、vfox（Windows fallback）、Nixpkgs（\`mode: nix\` 时）。
- 输出：\`ToolSet\`（版本 + 安装路径 + checksum）。

### 4.4 Isolator (\`internal/isolator/\`)

- 工厂模式，根据 \`mode\` 返回具体实现：
  - \`noneIsolator\`: 仅修改当前进程环境。
  - \`containerIsolator\`: 生成 devcontainer.json，调用 Docker SDK。
  - \`nixIsolator\`: 生成 flake.nix，调用 \`nix-shell\` / \`nix develop\`。

### 4.5 Orchestrator (\`internal/orchestrator/\`)

- 管理环境生命周期（create / start / stop / destroy / snapshot）。
- 通过 Adapter 接口与不同后端交互：
  \`\`\`go
  type Adapter interface {
      Create(ctx context.Context, spec *EnvironmentSpec) (*Instance, error)
      Start(id string) error
      Stop(id string) error
      Destroy(id string) error
      Exec(id string, cmd []string) (*ExecResult, error)
  }
  \`\`\`
- 内置 Adapter：\`local-docker\`、\`k8s\`、\`ssh\`。

### 4.6 Lockfile (\`internal/lockfile/\`)

- 格式：\`env.uni.lock\`（JSON 或 YAML）。
- 内容：每个工具的 name + version + sha256sum + download URL；容器镜像的 digest；环境变量的最终值。
- 用途：后续 \`uni up\` 时优先使用 lockfile 中的确切版本，无需重新解析远程 registry。

---

## 5. 统一环境定义 (\`env.uni.yaml\`) 规范

### 完整 Schema

\`\`\`yaml
# 版本号（必填）
apiVersion: uni-env/v1

# 环境名称（可选，默认取目录名）
name: my-service

# 继承的模板列表（可选，支持 git/https/file 路径）
inherit:
  - git://github.com/myorg/uni-templates#base-go:1.x
  - file://./team-defaults.yaml

# 运行时工具及其版本（必填至少一项）
tools:
  go: "1.23.4"
  node: "22.12.0"
  python: "3.12.2"
  # 非语言工具也可声明（若 resolver 支持）
  postgresql: "16.4"

# 环境变量（可选）
env:
  GOFLAGS: "-mod=readonly"
  DATABASE_URL: "postgres://dev:dev@localhost:5432/app?sslmode=disable"
  # 引用本地文件（gitignored）
  _.file: .env.local

# 隔离策略（可选，默认 mode: none）
isolation:
  mode: container          # none | container | nix
  # 当 mode=container 时的详细配置
  container:
    image: ghcr.io/company/dev-images/go-node:1.2.0
    imageDigest: "sha256:abc123..."   # 强制锁定
    features:
      - ghcr.io/devcontainers/features/docker-in-docker:1
    services:
      redis:
        image: redis:7-alpine
        ports: ["6379:6379"]
    forwardPorts: [3000, 4000]
    user: "1000:1000"                # 显式 UID/GID
    postCreateCommand: |
      go mod download
      pre-commit install
  # 当 mode=nix 时的详细配置（可选）
  nix:
    packages: [gcc, openssl.dev]
    extraFlags: "--impure"

# 生命周期钩子（可选）
hooks:
  preStart:
    - command: mise install        # 确保工具已安装
  postStart:
    - run: ./scripts/db-migrate.sh
      if: ${{ env.FIRST_TIME == "1" }}
  preStop:
    - run: ./scripts/cleanup.sh

# 团队策略（可选，需要中央服务配合）
policy:
  template: company/base-go:1.x    # 强制使用的模板
  allowedBackends: [local-docker, k8s-namespace]
  maxIdleMinutes: 30               # 自动停机时间
  auditLog: true
\`\`\`

### 字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| \`apiVersion\` | string | 是 | 当前版本 \`uni-env/v1\` |
| \`name\` | string | 否 | 环境标识，用于日志和状态查询 |
| \`inherit\` | []string | 否 | 模板 URL 列表，支持 \`git://\`、\`https://\`、\`file://\` |
| \`tools\` | map[string]string | 是（至少一项） | 工具名 → 版本号，版本号支持 semver 范围（如 \`^22\`） |
| \`env\` | map[string]string | 否 | 环境变量键值对；特殊键 \`_.file\` 表示从文件加载 |
| \`isolation.mode\` | enum | 否 | \`none\`（默认）、\`container\`、\`nix\` |
| \`isolation.container\` | object | 条件必填 | 当 \`mode=container\` 时必须提供 |
| \`isolation.container.image\` | string | 是 | 基础镜像名（不含 digest） |
| \`isolation.container.imageDigest\` | string | 强烈推荐 | 镜像 SHA256 digest |
| \`isolation.container.features\` | []string | 否 | devcontainer features URI |
| \`isolation.container.services\` | map[string]object | 否 | 附加服务容器定义 |
| \`isolation.container.forwardPorts\` | []int | 否 | 自动转发到宿主机的端口 |
| \`isolation.container.user\` | string | 否 | 容器内用户 UID:GID |
| \`isolation.container.postCreateCommand\` | string | 否 | 容器创建后执行的命令 |
| \`isolation.nix\` | object | 条件必填 | 当 \`mode=nix\` 时必须提供 |
| \`isolation.nix.packages\` | []string | 否 | 额外 Nix 包 |
| \`hooks\` | object | 否 | 包含 \`preStart\`、\`postStart\`、\`preStop\` 数组 |
| \`hooks.*[].command\` | string | 条件 | 简单命令字符串 |
| \`hooks.*[].run\` | string | 条件 | 复杂命令（支持 shell） |
| \`hooks.*[].if\` | string | 否 | 条件表达式（使用 \`${{ }}\` 语法） |
| \`policy\` | object | 否 | 团队治理配置（需中央服务） |

### 模板继承机制

- 模板是另一个 \`env.uni.yaml\` 文件（或部分片段）。
- 继承规则：**子配置覆盖父配置**（deep merge）。
- 模板 URL 支持 Git 仓库 + 分支/标签（如 \`git://github.com/org/templates#base-python:2.x\`）。
- 本地模板（\`file://\`）用于团队内共享片段。

---

## 6. 命令行接口 (CLI) 参考

### 全局选项

| 标志 | 类型 | 默认 | 说明 |
|------|------|------|------|
| \`--config\` | string | \`./env.uni.yaml\` | 配置文件路径 |
| \`--verbose\` | bool | false | 输出调试日志 |
| \`--dry-run\` | bool | false | 模拟执行，不实际创建环境 |

### 子命令详情

#### \`uni up\`

- 描述：创建并启动开发环境。
- 用法：\`uni up [flags]\`
- 特有标志：
  - \`--reuse\`：重用已有的容器/workspace（默认重建）
  - \`--no-hooks\`：跳过钩子执行
  - \`--backend\`：强制指定后端（如 \`local-docker\`），覆盖配置

#### \`uni down\`

- 描述：停止并销毁环境。
- 用法：\`uni down [environment-id]\`
- 若不指定 ID，则关闭当前目录关联的环境。

#### \`uni status\`

- 描述：查看环境状态（运行中/停止/不存在）。
- 用法：\`uni status [environment-id]\`

#### \`uni exec\`

- 描述：在环境中执行命令。
- 用法：\`uni exec -- <command> [args...]\`
- 示例：\`uni exec -- go test ./...\`

#### \`uni import\`

- 描述：从现有配置文件生成 \`env.uni.yaml\`。
- 用法：\`uni import <source> [target]\`
- 支持的 source：
  - \`.tool-versions\` → 生成 \`tools:\` 段
  - \`devcontainer.json\` → 生成 \`isolation.container:\` 段
  - \`flake.nix\` → 生成 \`isolation.nix:\` 段（有限转换）
  - \`package.json\` → 提取 \`engines\` 字段

#### \`uni config\`

- 描述：查看或编辑当前环境配置。
- 用法：\`uni config [get/set] <key> [value]\`
- 示例：\`uni config set tools.node 22.14.0\`

#### \`uni template\`

- 描述：管理团队模板。
- 子命令：
  - \`list\`：列出可用模板
  - \`pull <url>\`：下载模板到本地缓存
  - \`push <path>\`：上传本地模板到注册中心（需认证）

---

## 7. Adapter 接口规范

所有后端适配器必须实现以下 Go 接口：

\`\`\`go
// internal/adapter/types.go
package adapter

type Instance struct {
    ID        string
    Status    string // "running", "stopped", "creating"
    Addresses []string // 连接地址（IP:port）
    Metadata  map[string]string
}

type EnvironmentSpec struct {
    Image       string
    ImageDigest string
    Features    []string
    Services    map[string]ServiceSpec
    ForwardPorts []int
    User        string
    PostCreate  string
    Env         map[string]string
    Tools       map[string]string // 由 Resolver 填充的已安装工具路径
}

type ServiceSpec struct {
    Image string
    Ports []string // e.g., "5432:5432"
    Env   map[string]string
}

type Adapter interface {
    // 唯一标识符，如 "local-docker"
    Name() string

    // 创建并启动环境
    Create(ctx context.Context, spec *EnvironmentSpec) (*Instance, error)

    // 启动已存在的环境（如从停止状态恢复）
    Start(ctx context.Context, id string) error

    // 停止环境（保留资源）
    Stop(ctx context.Context, id string) error

    // 销毁环境（释放所有资源）
    Destroy(ctx context.Context, id string) error

    // 在环境中执行命令
    Exec(ctx context.Context, id string, cmd []string) (*ExecResult, error)

    // 获取环境状态
    Status(ctx context.Context, id string) (*Instance, error)
}

type ExecResult struct {
    Stdout string
    Stderr string
    ExitCode int
}
\`\`\`

### 内置 Adapter 说明

| Adapter 名称 | 后端 | 依赖 | 适用场景 |
|--------------|------|------|----------|
| \`local-docker\` | Docker Engine | Docker CLI / SDK | 本地容器开发 |
| \`k8s\` | Kubernetes | kubectl / client-go | 团队共享集群 |
| \`ssh\` | 远程 Linux 机器 | SSH 密钥 | 裸机或已有 VM |
| \`cloud-ec2\` | AWS EC2 | AWS SDK | 临时云实例 |

---

## 8. Lockfile 格式

Lockfile 文件名：\`env.uni.lock\`（放置在与 \`env.uni.yaml\` 同级目录）。

### 示例

\`\`\`yaml
# 自动生成，请勿手动编辑
version: 1
createdAt: "2026-06-15T10:00:00Z"
environmentName: my-service

tools:
  go:
    version: "1.23.4"
    resolved: "1.23.4"
    sha256: "a1b2c3d4e5f6..."
    url: "https://go.dev/dl/go1.23.4.linux-amd64.tar.gz"
  node:
    version: "22.12.0"
    resolved: "22.12.0"
    sha256: "f6e5d4c3b2a1..."
    url: "https://nodejs.org/dist/v22.12.0/node-v22.12.0-linux-x64.tar.xz"

container:
  image: "ghcr.io/company/dev-images/go-node:1.2.0"
  digest: "sha256:abc123def456..."
  services:
    redis:
      image: "redis:7-alpine"
      digest: "sha256:789012..."

env:
  final:
    GOFLAGS: "-mod=readonly"
    DATABASE_URL: "postgres://dev:dev@localhost:5432/app?sslmode=disable"
\`\`\`

### 作用

- **确定性**：后续 \`uni up\` 直接使用 lockfile 中的 checksum 和 URL，避免网络变化导致的版本漂移。
- **审计**：记录每个环境的精确组成，可用于合规检查。
- **离线部署**：将 lockfile + 预下载的工具缓存打包，可在无网络环境复现。

---

## 9. 贡献指南

### 开发环境准备

1. 安装 Go 1.23+、Docker（可选）、Node.js（用于测试前端集成）。
2. Fork 并克隆仓库。
3. 运行 \`make deps\` 安装依赖。
4. 运行 \`make build\` 编译 CLI。

### 代码风格

- Go 代码遵循 [Go 官方编码规范](https://go.dev/doc/effective_go)。
- 使用 \`gofmt\` 格式化，\`golangci-lint\` 进行静态检查。
- 提交前运行 \`make lint\` 和 \`make test\`。

### 提交规范

- 使用 [Conventional Commits](https://www.conventionalcommits.org/) 格式：
  - \`feat:\` 新功能
  - \`fix:\` 修复 bug
  - \`docs:\` 文档变更
  - \`refactor:\` 重构
  - \`test:\` 添加测试
- PR 标题示例：\`feat(resolver): add vfox backend for Windows\`

### 测试策略

- 单元测试覆盖率目标 ≥ 80%。
- 集成测试使用 \`testcontainers-go\` 启动真实的 Docker 容器。
- 端到端测试通过 GitHub Actions 在 Ubuntu / macOS / Windows 上运行。

### 如何报告 Issue

- 使用 GitHub Issues 模板（Bug Report / Feature Request）。
- 提供：
  - uni-env 版本（\`uni version\`）
  - 操作系统及版本
  - 复现步骤
  - 期望行为与实际行为
  - 相关日志（开启 \`--verbose\`）

---

## 10. 许可证

本项目采用 **Apache License 2.0**。

- 商业友好，允许修改和再分发。
- 必须保留版权声明和免责条款。
- 专利授权条款保护贡献者。

---

> **附录 A：快速入门示例**  
> 详见 examples/ 目录，包含从简单到复杂的多种场景。

> **附录 B：常见问题 (FAQ)**  
> 见 FAQ.md。

> **附录 C：API 参考（程序化调用）**  
> uni-env 也提供 Go 库，可供其他工具集成，详见 pkg/ 目录的 godoc。

---

*本文档遵循 [Semantic Versioning](https://semver.org/)，重大更新时会更新版本号。*

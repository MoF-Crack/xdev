# XDev 实施方案：短中长期路线图

> **文档类型**：实施计划
> **状态**：Draft — 待用户批准后实施
> **创建日期**：2026-07-25
> **所有者**：xinyan
> **执行基线**：commit `cfba854`（main），tag `v0.1.0`

---

## 当前事实快照

### Dependabot 告警（26 个）

| 生态 | 数量 | 清单 |
|---|---|---|
| Go | 19 | `golang.org/x/crypto`、`golang.org/x/net` |
| npm | 7 | `esbuild`、`postcss`、`vite`、`vitest`、`brace-expansion` |

| 严重度 | 数量 |
|---|---|
| Critical | 8 |
| High | 6 |
| Medium | 12 |

### 现有 Draft Specs

| Spec | 主题 | 状态 |
|---|---|---|
| 001 | CI Go 版本一致性 | Draft |
| 002 | 集成测试 CI 门禁 | Draft |
| 003 | 发布签名硬门禁 | Draft |
| 004 | 三平台发布产物同源 | Draft |
| 005 | 宿主状态快照 | **Accepted**（已实现） |
| 006 | 双审查自动验证 | Draft |
| 007 | UI 下载集成验收 | Draft |

### CI 现状

- Actions 全用 Node 20 版本（被 forced to Node 24，仅警告）
- Release v0.1.0 已发布（12 产物）
- 增量构建已实现
- 签名 step 因无 secret 而跳过

---

## 短期方案（1-3 个工作日）

### S1. Dependabot 漏洞处理

**目标**：消除 26 个安全告警中的绝大多数。

**步骤**：

1. **Go 依赖升级**（19 个告警）
   ```bash
   go get golang.org/x/crypto@latest
   go get golang.org/x/net@latest
   go mod tidy
   go vet ./...
   go test ./... -count=1
   ```
   - 验证：`go test` 全过 + `gh api repos/MoF-Crack/xdev/dependabot/alerts --jq '. | length'` 从 26 降到 ≤7（剩余 npm）
   - 风险：`x/crypto` 和 `x/net` 是间接依赖，升级后 API 可能 breaking（但 Go 团队通常向后兼容）
   - 回退：`git checkout -- go.mod go.sum` 即可恢复

2. **npm 依赖升级**（7 个告警）
   ```bash
   cd frontend
   npm audit fix           # 自动修复兼容性升级
   npm audit fix --force   # 强制升级 major 版本（需测试）
   npm ci
   npm run build
   npm test                # 如有 e2e 测试
   ```
   - 目标版本：`vite@^6`（从 ^5）、`vitest@^3`（从 ^2）、`esbuild`/`postcss` 跟随
   - 验证：`npm audit` 报 0 漏洞 + `npm run build` 成功 + 前端 e2e 测试通过
   - 风险：vite 5→6 有 breaking change（配置 API 变化），需检查 `vite.config.*`

3. **提交策略**：分两个 commit（Go 一个、npm 一个），便于回退

**退出条件**：
- Dependabot 告警 ≤2（允许无法自动修复的残余）
- CI 三平台全绿
- 本地 `go test` + `npm run build` 通过

---

### S2. Node 20 Deprecation 消除

**目标**：消除 CI 中所有 Node 20 deprecation 警告。

**步骤**：

1. 升级 `.github/workflows/ci.yaml` 中的 actions 版本：
   ```yaml
   # 当前（Node 20）→ 目标（Node 24 兼容）
   actions/checkout@v4    → actions/checkout@v5    # v5 使用 Node 24
   actions/setup-go@v5    → actions/setup-go@v6    # v6 使用 Node 24
   actions/setup-node@v4   → actions/setup-node@v5  # v5 使用 Node 24
   actions/upload-artifact@v4 → actions/upload-artifact@v5  # v5 使用 Node 24
   ```
   > 注意：需确认每个 action 的最新版本是否已使用 Node 24。如果在编写时
   > 这些版本尚未发布，则保持当前版本并接受警告（不影响功能）。

2. 检查 `softprops/action-gh-release@v2` 是否有 Node 24 版本

**验证**：CI run 的 ANNOTATIONS 中不再出现 "Node.js 20 is deprecated" 警告。

**风险**：action major 版本升级可能改变输入参数行为，需检查 release notes。

---

### S3. Release 签名 Secret 配置

**目标**：配置 Ed25519 签名密钥对，让 release 产物自动签名。

**前置**：Spec 003（发布签名硬门禁）仍为 Draft，但 release job 的签名
代码已就绪，只需配置 secret。

**步骤**：

1. **生成密钥对**（本地执行，不提交）：
   ```bash
   # 用 xdev 自带的 keygen
   go build -o xdev .
   ./xdev keygen
   # 或手动：openssl genpkey -algorithm Ed25519 -out keys/priv.key
   #         openssl pkey -in keys/priv.key -pubout -out keys/pub.key
   ```

2. **配置 GitHub Secrets**：
   ```bash
   # 私钥（PEM 格式，base64 编码后存入 secret）
   gh secret set XDEV_SIGNING_PRIVATE_KEY --repo MoF-Crack/xdev < keys/priv.key
   # 公钥（base64 编码）
   gh secret set XDEV_SIGNING_PUBLIC_KEY --repo MoF-Crack/xdev < keys/pub.key
   ```

3. **验证**：重新打 tag 触发 release，检查日志中 "Sign manifests" step
   从 `-`（跳过）变为 `✓`（执行），且 release 中产物包含签名信息。

**退出条件**：
- release run 的 "Sign manifests" step 显示 ✓
- 产物中 `dist/*` 包含 `.sig` 文件或 ldflags 注入了公钥
- **不提交私钥到仓库**（`keys/priv.key` 在 `.gitignore` 中）

---

## 中期方案（1-2 周）

### M1. GUI 全平台覆盖

**目标**：补齐 GUI 变体的全平台产物。

**当前缺口**：
| 产物 | linux-amd64 | linux-arm64 | darwin-amd64 | darwin-arm64 |
|---|---|---|---|---|
| AppImage | ✅ | ❌ | N/A | N/A |
| DMG | N/A | N/A | ❌ | ✅ |

**步骤**：

1. **Linux ARM64 AppImage**
   - 在 release job 中增加 arm64 交叉编译 + AppImage 构建
   - `appimagetool-aarch64.AppImage` 替代 `x86_64` 版本
   - 需要 `qemu-user-static` 在 ubuntu runner 上模拟 arm64

2. **macOS AMD64 DMG**
   - 在 `release-macos` job 中增加 `GOARCH=amd64 go build -tags bindings`
   - 用 `hdiutil` 生成 `xdev-gui-darwin-amd64-v${VER}.dmg`

3. **增量构建兼容**：每个新产物的文件名加入 `EXISTING_ASSETS` 检查

**退出条件**：release 包含 14 个产物（现有 12 + 新增 2）

---

### M2. SHA256SUMS 校验文件

**目标**：为每个 release 生成校验和文件，方便用户验证下载完整性。

**步骤**：

1. 在 release job 的 "Create Release" step 之前增加：
   ```yaml
   - name: Generate SHA256SUMS
     run: |
       cd dist
       sha256sum * > SHA256SUMS
       # 如有签名密钥，签名校验和文件
       if [ -n "$XDEV_SIGNING_PRIVATE_KEY" ]; then
         gpg --detach-sign SHA256SUMS  # 或用 xdev 自签名
       fi
   ```

2. `action-gh-release` 的 `files` glob 自动包含 `dist/SHA256SUMS` 和 `dist/SHA256SUMS.sig`

**退出条件**：release 中包含 `SHA256SUMS` 文件，用户可 `sha256sum -c SHA256SUMS` 验证。

---

### M3. Spec 003（发布签名硬门禁）推进

**目标**：将 Draft Spec 003 走完 SDD 流程到 Accepted + 实现。

**步骤**：

1. **澄清**：审查 spec.md 内容，确认与当前实现（release job 已有签名代码）的差异
2. **批准**：用户批准 spec → 状态改为 Approved
3. **计划**：编写 plan.md，明确硬门禁的实现步骤
4. **实现**：在 release job 中加入"无签名密钥则失败"的硬门禁
5. **验收**：验证无 secret 时 release 失败、有 secret 时正常

**退出条件**：Spec 003 状态为 Accepted，release job 在无签名时阻塞。

---

### M4. Spec 004（三平台同源验证）推进

**目标**：确保三平台产物从同一 commit + 同一依赖版本构建。

**步骤**：

1. 在 release job 中增加 commit SHA 记录
2. 在每个产物中嵌入 commit SHA（通过 ldflags `-X main.version`）
3. 增加验证 step：检查所有产物的 commit SHA 一致

**退出条件**：Spec 004 Accepted，release 产物可追溯构建 commit。

---

## 长期方案（3-6 周）

### L1. 7 份 Draft Feature Spec 全量推进

**目标**：将 001-007（除已 Accepted 的 005）全部推进到 Accepted + 实现。

**优先级排序**：

| Spec | 主题 | 优先级理由 |
|---|---|---|
| 003 | 发布签名硬门禁 | 中期已覆盖，可合并 |
| 004 | 三平台同源 | 中期已覆盖，可合并 |
| 001 | CI Go 版本一致性 | 影响构建可复现性 |
| 002 | 集成测试 CI 门禁 | 提升质量保障 |
| 006 | 双审查自动验证 | 治理流程优化 |
| 007 | UI 下载集成验收 | 端到端测试覆盖 |

**每份 Spec 的 SDD 流程**：
1. 澄清 → 2. 批准（Approved）→ 3. 计划（Plan Ready）→ 4. 实现 → 5. 审查 → 6. 验收（Accepted）

---

### L2. Spec 005 SC-004 开销验证

**目标**：验证宿主快照的开销 ≤5%。

**当前阻塞**：全包 TestMain 覆盖受 import cycle 限制（`env`/`util` 不能挂 hostsnapshot TestMain）。

**方案**：
1. 为不能接入 TestMain 的包单独写 benchmark
2. 对比 `XDEV_HOST_SNAPSHOT=0` vs `=1` 的测试耗时
3. 生成性能报告，确认 ≤5% 阈值

---

### L3. Semver 版本策略 + Changelog 自动化

**目标**：建立结构化版本管理和变更记录。

**步骤**：

1. **版本策略文档**：
   - `v0.x.y`：0.x 阶段，API 不保证稳定
   - `v1.0.0`：首个稳定版，API 冻结
   - Patch（`v1.0.x`）：bug 修复
   - Minor（`v1.x.0`）：新功能，向后兼容
   - Major（`vx.0.0`）：破坏性变更

2. **Changelog 自动化**：
   - 使用 `conventional-commits` + `release-please` 或 `git-cliff`
   - 从 commit message 自动生成 release notes
   - 替代当前 `generate_release_notes: true` 的 GitHub 默认生成

3. **集成到 CI**：
   - release job 读取 conventional commits 生成结构化 changelog
   - changelog 作为 release body 附加到 GitHub Release

---

## 实施顺序与依赖

```
短期（并行）
├── S1 Dependabot ──┐
├── S2 Node 20    ──┤── 短期全部完成后 ──→ 中期
└── S3 签名 Secret ─┘                      │
                                            ├── M1 GUI 全平台
                                            ├── M2 SHA256SUMS
                                            ├── M3 Spec 003
                                            └── M4 Spec 004
                                                  │
                                                  └──→ 长期
                                                       ├── L1 7 份 Spec
                                                       ├── L2 SC-004
                                                       └── L3 Semver
```

**依赖关系**：
- S3（签名 secret）是 M3（Spec 003 硬门禁）的前置
- S1（Dependabot）无依赖，可独立执行
- M1-M4 之间可并行（不同 step/job）
- L1 的 Spec 003/004 可与 M3/M4 合并执行

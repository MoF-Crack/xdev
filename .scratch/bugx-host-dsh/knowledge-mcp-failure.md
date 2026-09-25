# Incident: bugx_tool_matrix / bugx_knowledge_route 失败——MCP root 解析到家目录（只诊断，未修复）

Date: 2026-09-18 20:0x（事件发生于 20:04:22 本地 / 12:04:22 UTC）
关联：`.scratch/bugx-host-dsh/spec.md`（宿主方案 §2.3 的"工作区必须是目标仓库"前置条件被实测反证）

## 现象

新 dsh 会话（BugX 预设）中 `bugx_tool_matrix` 与 `bugx_knowledge_route` 两个 MCP 调用失败，报
`C:\Users\xinyan\.bugx\knowledge\lesson-ledger.sqlite3` 被锁定或缺失；MCP 返回
`interactive_required: true`，要求向用户呈现：重试 / 降级 / 暂停。

## 一句话根因

**不是锁，账本也没坏**——该会话挂上的 supervisor MCP 进程把 **家目录 C:\Users\xinyan 当成了项目根**
（`serve --root C:\Users\xinyan --role host`）；而家目录的 `.bugx` 是 BugX 的**全局引擎/知识根**，
`~/.bugx/knowledge` 按全局规范只有 `global.sqlite3`（global manifest: `scope=global,
database=global.sqlite3`），**不存在也不允许存在**项目命名的 `lesson-ledger.sqlite3`，于是知识工具
一开库就命中存在性检查失败：`SQLite 运行库不存在`（缺失分支，不是锁定）。

## 证据链

### 1) 进程谱系（实测，全部存续至诊断时刻）

```text
powershell.exe(61772, 普通交互 shell)                       19:57:47 启动 dsh GUI
 └─ node(58764, 经 xdev shim) dsh … bin.js web
     └─ node(19140, E:\XDev\current\node) bin.js web        ← GUI re-exec
         └─ cmd.exe(65896) /d /s /c "bugx mcp serve --root ."   20:00:03
             └─ node(19360) bugx.js mcp serve --root .
                 └─ node(66756, re-exec 用真实 node)
                     └─ py -3.14(16188) -m bugx_cli.cli mcp serve --root .
                         └─ python(3144)  ← 全局日志: "root": "C:\\Users\\xinyan"
                             └─ python(39440) bugx_mcp_server.py
                                 serve --root C:\Users\xinyan --role host   ← 出错的服务器
```

- 全局日志 `C:\Users\xinyan\.bugx\logs\bugx.jsonl`：`command_started pid=3144 command=mcp root=C:\Users\xinyan`。
- **项目日志 `D:\...\xmdevenvmt\.bugx\logs\bugx.jsonl` 中 mcp-serve 事件数 = 0** → 从未有任何 MCP 服务过目标仓库；
  全局日志中 10 条 mcp-serve 事件全部来自 pid 39440（root=家目录）。
- 项目侧账本本身健康：`.bugx/knowledge/lesson-ledger.sqlite3`（184,320 B，WAL，manifest scope=project），
  doctor 两次验证 ✓；其 `-shm/-wal` 的 20:26 触碰来自本诊断会话的 doctor 调用。

### 2) 失败日志（12:04:22 UTC，两条 mcp_request_failed，pid 39440）

- `bugx_tool_matrix`：knowledge_handlers.py L184 → knowledge_sources.tool_matrix →
  ledger/tool_experience.py L329 `_conn(readonly=True)` → ledger/schema.py L256 → L241
  → `LessonLedgerError: SQLite 运行库不存在: C:\Users\xinyan\.bugx\knowledge\lesson-ledger.sqlite3`
- `bugx_knowledge_route`：knowledge_handlers.py L106 → retrieve_patterns → ledger/patterns.py L580
  → 同一存在性检查 → 同一异常。
- 12:00:04 的 `initialize`/`tools/list` 与 12:03:44 的若干 `tools/call` 均成功——supervisor 面不需要
  账本，所以服务器"带病可用"，只有知识面暴露问题。

### 3) 代码语义（运行引擎 C:\Users\xinyan\.bugx\core）

- `ledger/schema.py L238-244 _regular`：`if not path.exists(): raise LessonLedgerError(f"{label} 不存在: …")`
  ——**纯存在性检查，根本没走到 SQLite 打开/加锁**。"被锁定"是宿主侧转述措辞，实际是"不存在"。
- `bugx_mcp_server.py L247-259 _error_payload`：所有 tools/call 异常统一包装为
  `interactive_required:true / next_action:"PROMPT_USER" / choices:["retry","fallback","later"]`，
  prompt="1) 立即重试 2) 本轮切换手动/回退 3) 下次再说（停止本轮）"——即"重试/降级/暂停"，协议行为正常。

### 4) root 为什么是家目录（关键机制）

- BugX dsh 预设 `agent.cordis.yml` 的 MCP 条目：`command: bugx, args: [mcp, serve, --root, .]`，
  **没有配置 `cwd`**。
- dsh-mcp-client（lib/index.js L42-47 + schema L744 `cwd: z.string().default("")`）：stdio 传输
  只透传 config，**代码中不存在 workspace/会话根概念**（grep 无命中）；`cwd` 默认空串（falsy）
  → Node spawn 不设置 → 子进程**继承 dsh GUI 服务器进程的工作目录**。
- GUI 由一个普通 PowerShell（61772）在 19:57:47 启动，其工作目录为 C:\Users\xinyan →
  整条 MCP 子进程链 cwd=家目录 → `--root .` 被解析为 `C:\Users\xinyan`。
  **即使会话工作区选的是仓库，MCP 子进程的 cwd 也不由会话工作区决定。**

### 5) 防呆早已拦截（设计行为，非故障）

- 19:59:39（12:00 前）有一次 `bugx init` root=家目录 → `command_failed`：
  `"Project lesson ledger SQLite runtime missing; refusing silent rebuild from JSON seed,
  please restore from backup or run explicit recovery"`——因为 `~/.bugx/knowledge` 里恰有全局种子
  `lesson-ledger.json`（与 `global.json` 同为 11,237 B 引擎种子），"有种子、无运行库"触发
  **拒绝静默重建**保护（AGENT_GUIDE §13：不得空账本冒充历史；同时防止项目化操作覆盖全局库）。
- 19:59:15 / 19:59:32 另有两次 `bugx mcp`（root=家目录）core script exit 2，先于成功 serve，
  与"家目录不是合法项目"一致（未深究具体 gate，不影响根因）。

## 三个交互选项的正确读法

- **重试**：无意义——文件受保护逻辑约束不会凭空出现，`bugx init` 也已拒绝创建。
- **降级**：本轮放弃知识工具，走内置回退继续。
- **暂停**：停止本轮。

## 修复方向（本次未执行任何一项）

1. 用户侧（零改动）：让 MCP 的 cwd 落到目标仓库——从仓库目录的 shell 启动 dsh GUI；和/或
2. 预设侧：`~/.dsh/.agent-presets/bugx/agent.cordis.yml` 的 mcp-client 条目显式配置
   `cwd: D:\Env\Android\ReverseTools\ReverseEng\xmdevenvmt`（dsh-mcp-client stdio 原生支持 cwd；
   注意相对路径仍相对 GUI cwd，无效）；和/或把 `--root .` 显式化为绝对路径。
3. 上游建议（bugx）：`mcp serve` 在 root 解析到全局根（`.bugx/knowledge/manifest.json` 的
   `scope=global`）时启动即 fail-fast 报"家目录不是合法项目"，而不是带病启动后仅在知识工具上失败。

## 附带异常（记录待查，与本次失败无关）

`C:\Users\xinyan\.bugx\knowledge\.bugx`：1 字节文件，20:10:26 出现，读取被拒（UnauthorizedAccess）。
失败路径是纯存在性检查，与此文件无关；来源未明，留档。

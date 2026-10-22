package isolator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrStateCorrupt marks a persisted isolator session that **exists but cannot be
// parsed** (第九轮 A-02). Callers must never mistake it for "no session": the
// container handle / file-ownership record is unreadable, so reporting success
// leaks a container (or leaves generated files behind) while telling the user
// the environment was destroyed.
var ErrStateCorrupt = errors.New("isolator state file is corrupt")

// persistedState is the on-disk isolator session (Spec 032). It lets
// `xdev isolate setup` and `xdev isolate exec` share state across
// processes instead of losing the container ID / flake dir.
type persistedState struct {
	ContainerID string `json:"containerId,omitempty"`
	FlakeDir    string `json:"flakeDir,omitempty"`
	ProjectDir  string `json:"projectDir,omitempty"`
	Image       string `json:"image,omitempty"`

	// WrittenFiles maps a normalized path of every file this isolator wrote
	// during Setup to the hex SHA-256 of the exact bytes written
	// (C-07 / P1-34). Teardown deletes a path only while its on-disk content
	// still hashes to the recorded value, so a file the user edited or
	// replaced in the meantime survives — the pre-fix code removed
	// flake.nix blindly, destroying hand-written user flakes.
	//
	// Setup and Teardown run in separate processes, so this record (not
	// in-memory state) is the only proof of ownership. A state file written
	// before C-07 carries no map at all: Teardown then degrades to
	// "delete nothing + one log line" instead of guessing.
	WrittenFiles map[string]string `json:"writtenFiles,omitempty"`
}

func statePath(xmdeDir, mode string) string {
	return filepath.Join(xmdeDir, "isolate", mode+".json")
}

// saveState 原子写入隔离会话状态（第七轮审计 F-51）。
//
// 该文件是 setup/teardown **跨进程唯一的**所有权凭证与容器句柄，旧实现用裸
// os.WriteFile：写盘瞬间崩溃/断电/磁盘满会留下截断的 JSON，而 loadState 又
// 静默吞掉解析错误并返回零值 —— 后果是
//   - 容器模式：containerID 为空 → Teardown 直接 return → **容器永久泄漏**；
//   - nix 模式：WrittenFiles 丢失 → 走 legacy 分支只打日志不删 → flake.nix 残留，
//     用户重跑 setup 又被 ensureOverwritable 拒绝。
//
// 写侧（F-51）用原子写把「产生截断文件」的窗口关掉；读侧（第九轮 A-02）让
// loadState 对已损坏的文件返回 ErrStateCorrupt 而不是零值，于是即使文件真的
// 坏了，Teardown 也会报错而不是谎报成功。
//
// 现在写临时文件 + fsync + rename（同目录内 rename 在 Windows/POSIX 都是原子的）。
//
// 第十轮 P2-2：**返回 error**。修复前本函数无返回值，MkdirAll / Marshal /
// CreateTemp / Write / Close / Rename 任一步失败都只 log 一行就返回 nil，调用方
// （container.go 的 Setup、nix.go 的 Setup）于是照常报成功：
//
//	container：`docker run` 真的启动了容器，句柄却没落盘 → Teardown 认为
//	  「没有容器」→ 容器/端口/磁盘永久泄漏；
//	nix：flake.nix 已写进项目，归属记录却没落盘 → Teardown 静默成功 →
//	  flake.nix 残留且重跑 setup 被 ensureOverwritable 拒绝。
//
// 这是第九轮 A-02（读侧：损坏状态必须报错）在写侧的对应缺陷：读侧已经不再
// 把「读不出来」伪装成「没有会话」，写侧同样不能再把「没写下去」伪装成成功。
// 调用方必须检查返回值并回滚（Setup 的两条路径都已如此）。
func saveState(xmdeDir, mode string, s persistedState) error {
	if xmdeDir == "" || mode == "" {
		return nil
	}
	p := statePath(xmdeDir, mode)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create isolator state dir %s: %w", dir, err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal isolator state for %s: %w", p, err)
	}
	// 唯名临时文件：固定名会被并发 setup/teardown 互相截断
	//（internal/config 的 P1-24 守卫禁止生产代码出现固定 ".tmp" 中间文件）。
	tmp, err := os.CreateTemp(dir, ".isolate-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp isolator state file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后为 no-op
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp isolator state %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		// fsync 失败不致命（部分文件系统不支持），记录后继续。
		log.Printf("isolate: fsync of %s failed (continuing): %v", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp isolator state %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		return fmt.Errorf("publish isolator state %s: %w", p, err)
	}
	return nil
}

// loadState 读取隔离会话状态（第七轮审计 F-51 加日志，第九轮 A-02 补齐语义）。
//
// 返回值把三种情况彻底分开，调用方**必须**区别对待：
//
//   - 文件不存在 → (零值, nil)。首次运行前的正常情况，静默，不是错误。
//   - 文件存在但解析失败 → (零值, ErrStateCorrupt 包装的错误)。这里返回零值
//     是为了让调用方绝不拿着半截状态去删文件；但错误必须一路冒到用户面前，
//     因为**零值不等于「没有会话」**——容器句柄与文件归属记录都在里面，丢了
//     就意味着有资源收不回来（A-02 的容器泄漏正是调用方把零值当「没有容器」）。
//   - 其它读错误（权限/IO） → (零值, 错误)。同样不能静默当成「没有会话」。
//
// 修复前的行为（F-51）：解析失败只 log.Printf 一条 WARNING 再返回零值，
// containerIsolator.Teardown 于是 `containerID == ""` → return nil → CLI 打印
// `✔ isolated environment destroyed`，而 state 里记着的容器仍在运行。
func loadState(xmdeDir, mode string) (persistedState, error) {
	var s persistedState
	if xmdeDir == "" || mode == "" {
		return s, nil
	}
	p := statePath(xmdeDir, mode)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil // 首次运行：没有状态是正常的
		}
		return s, fmt.Errorf("read isolator state %s: %w", p, err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return persistedState{}, fmt.Errorf("%w: %s: %v", ErrStateCorrupt, p, err)
	}
	return s, nil
}

// corruptStateHint 是状态损坏时给用户的可操作指引（A-02 要求 2：错误信息必须
// 给出可操作指引）。mode 决定「丢了什么」与「怎么恢复」——container 丢的是容器
// 句柄（要 docker ps/rm），nix 丢的是文件归属记录（flake.nix 可能残留）。
// quarantineNote 为空时省略尾句（只读路径不改动文件，见 containerIsolator.Exec）。
func corruptStateHint(mode IsolationMode, quarantineNote string) string {
	var lost, recover string
	switch mode {
	case ModeContainer:
		lost = "the container handle it held is unreadable, so xdev will NOT report the environment as destroyed — " +
			"the container (if any) is still running"
		recover = "list it with `docker ps -a`, remove it with `docker rm -f <id>`, then re-run setup"
	default:
		lost = "the record of which files xdev wrote is unreadable, so xdev will NOT report the environment as destroyed — " +
			"a generated flake.nix may still be on disk"
		recover = "check the project for a leftover flake.nix, then re-run setup"
	}
	hint := fmt.Sprintf("%s; recover by: %s", lost, recover)
	if quarantineNote != "" {
		hint += "; " + quarantineNote
	}
	return hint
}

// quarantineState 把损坏的 state 文件改名保留为 <mode>.json.corrupt-<时间戳>
// （A-02），并返回可读的说明。**绝不删除**：损坏文件是唯一的残留证据（里面
// 就有泄漏容器的 ID 或 writtenFiles 归属记录），改名同时清空 statePath 让后续
// setup/teardown 可以正常推进（否则每次调用都在同一个坏文件上失败）。
//
// 第十轮 P3-1：文件名旧实现只有**秒级**精度
// （`20060102T150405Z`），同一秒内发生两次 quarantine 时目标名完全相同，
// os.Rename 于是**覆盖**前一份证据 —— 审计实测 `quarantine_files=1`，第一份里
// 泄漏容器的 ID 被销毁。损坏的 state 是残留资源的唯一线索，覆盖等于毁证。
// 现在加纳秒后缀，并且仍用 O_CREATE|O_EXCL 抢占名字：万一时间戳碰撞（或系统
// 时钟被回拨/精度不足），抢占失败就换一个后缀重试，绝不覆盖已存在的证据文件。
//
// 改名失败（权限/占用）不致命：statePath 保持原样，调用方照常报错。
func quarantineState(xmdeDir, mode string) string {
	if xmdeDir == "" || mode == "" {
		return "the corrupt state file was left in place"
	}
	p := statePath(xmdeDir, mode)
	if _, err := os.Stat(p); err != nil {
		return "the corrupt state file was left in place"
	}
	dst, err := quarantineDst(p)
	if err != nil {
		// 无法在不覆盖既有证据的前提下腾出目标名：保留原文件（statePath 不变），
		// 调用方照常报错——宁可让用户再看到一次同一个错误，也不能销毁证据。
		log.Printf("isolate: could not quarantine corrupt state %s: %v", p, err)
		return "the corrupt state file was left in place (could not reserve a quarantine name)"
	}
	if err := os.Rename(p, dst); err != nil {
		// 抢占用的占位文件此刻还是空的：不清理的话会留下一个「看起来像证据却
		// 没有内容」的 .corrupt-* 文件，误导排查。删掉它，statePath 保持原样。
		_ = os.Remove(dst)
		log.Printf("isolate: could not quarantine corrupt state %s: %v", p, err)
		return "the corrupt state file was left in place (rename failed)"
	}
	log.Printf("isolate: quarantined corrupt state file as %s (kept for diagnosis; delete it once the container is gone)", dst)
	return fmt.Sprintf("the corrupt state file was preserved as %s for diagnosis", dst)
}

// quarantineDst 为 p 抢占一个**尚不存在**的 quarantine 目标名并返回它。
//
// 用 os.OpenFile(O_CREATE|O_EXCL) 创建占位文件来「预订」名字：POSIX 与 Windows
// 上 O_EXCL 都是原子的「不存在才创建」，因此并发/同秒的两次 quarantine 不会选中
// 同一个名字（抢不到就换后缀重试）。占位文件随后被 os.Rename 覆盖成真正的证据
// 文件（rename 覆盖同目录内自己的占位文件是安全的——那个文件是本次调用刚创建的）。
func quarantineDst(p string) (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	for attempt := 0; attempt < 64; attempt++ {
		candidate := fmt.Sprintf("%s.corrupt-%s", p, stamp)
		if attempt > 0 {
			candidate = fmt.Sprintf("%s.corrupt-%s-%d", p, stamp, attempt)
		}
		f, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return candidate, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
		// 名字已被占用（同秒的另一次 quarantine，或时钟精度不足）→ 换一个。
	}
	return "", fmt.Errorf("could not find an unused quarantine name for %s", p)
}

// clearState 删除状态文件。
//
// 第十轮 P2-3：guard 用来回答「**此刻磁盘上的**状态仍然属于本次操作吗」。
// 修复前 container 的 Teardown 无条件删除 statePath，而它只持有**构造时**的快照：
// 另一个进程在两者之间跑了一次 setup（写入新容器 ID）时，本次 Teardown 会把
// 新会话的 state 一起删掉 → 新容器彻底失管（再没人知道它存在）。传入 guard 后，
// 只有「磁盘上的 ID == 本次删掉的 ID」才清除；并发写入的新会话会被保留。
// guard 为 nil 表示调用方已经重新读取并确认了归属（nix 侧的 Teardown 就是
// 每次重新 loadState，它删的是本次真正处理的那个会话）。
//
// guard 读取失败（文件损坏/IO 错误）时**不删除**：读不出归属就无权清除，
// 宁可留下文件让下一次操作继续报错（A-02 的读侧语义），也不能删掉别人的句柄。
func clearState(xmdeDir, mode string, guard func(persistedState) bool) {
	if xmdeDir == "" || mode == "" {
		return
	}
	p := statePath(xmdeDir, mode)
	if guard != nil {
		s, err := loadState(xmdeDir, mode)
		if err != nil {
			log.Printf("isolate: not clearing state %s: cannot re-read it to confirm ownership (%v)", p, err)
			return
		}
		if !guard(s) {
			log.Printf("isolate: keeping state %s — it was rewritten by another xdev process since this operation started (its session is not ours to clear)", p)
			return
		}
	}
	_ = os.Remove(p)
}

// contentSHA256 returns the hex SHA-256 of content.
func contentSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// stateKey normalizes a path for use as a WrittenFiles key. Setup and
// Teardown run in separate processes and may see the same directory through
// differently-cased spellings (Windows paths are case-insensitive), so keys
// are folded on Windows.
func stateKey(path string) string {
	key := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

// recordWrittenFile remembers that this isolator wrote path with content and
// returns the (possibly newly created) map.
func recordWrittenFile(files map[string]string, path string, content []byte) map[string]string {
	if path == "" {
		return files
	}
	if files == nil {
		files = map[string]string{}
	}
	files[stateKey(path)] = contentSHA256(content)
	return files
}

// writtenHash returns the recorded hash for path ("" when unknown).
func writtenHash(files map[string]string, path string) string {
	if len(files) == 0 || path == "" {
		return ""
	}
	return files[stateKey(path)]
}

// removeIfContentUnchanged deletes path when its current content still hashes
// to want. Nothing is deleted when the hash differs (the user took the file
// over) or when the file is gone already; both cases are logged so a
// surprising leftover is diagnosable.
func removeIfContentUnchanged(path, want string) bool {
	if path == "" || want == "" {
		return false
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return false // already gone, or unreadable → never delete blindly
	}
	if contentSHA256(current) != want {
		log.Printf("isolate: leaving %s in place — its content changed since setup, so xdev no longer owns it", path)
		return false
	}
	if err := os.Remove(path); err != nil {
		log.Printf("isolate: could not remove %s: %v", path, err)
		return false
	}
	return true
}

// logLegacyTeardownSkip reports the C-07 degradation for state files written
// before the content hashes existed: ownership cannot be proven, so the file
// is left alone (deleting it would risk a user's own flake.nix).
func logLegacyTeardownSkip(mode, path string) {
	log.Printf("isolate teardown (%s): %s has no recorded content hash (state predates C-07) — skipping removal to avoid deleting a user file", mode, path)
}

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
func saveState(xmdeDir, mode string, s persistedState) {
	if xmdeDir == "" || mode == "" {
		return
	}
	p := statePath(xmdeDir, mode)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("isolate: could not create state dir %s: %v", dir, err)
		return
	}
	data, err := json.Marshal(s)
	if err != nil {
		log.Printf("isolate: could not marshal state for %s: %v", p, err)
		return
	}
	// 唯名临时文件：固定名会被并发 setup/teardown 互相截断
	//（internal/config 的 P1-24 守卫禁止生产代码出现固定 ".tmp" 中间文件）。
	tmp, err := os.CreateTemp(dir, ".isolate-state-*.tmp")
	if err != nil {
		log.Printf("isolate: could not create temp state file in %s: %v", dir, err)
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后为 no-op
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		log.Printf("isolate: could not write temp state %s: %v", tmpName, err)
		return
	}
	if err := tmp.Sync(); err != nil {
		// fsync 失败不致命（部分文件系统不支持），记录后继续。
		log.Printf("isolate: fsync of %s failed (continuing): %v", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		log.Printf("isolate: could not close temp state %s: %v", tmpName, err)
		return
	}
	if err := os.Rename(tmpName, p); err != nil {
		log.Printf("isolate: could not publish state %s: %v", p, err)
	}
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
// 改名失败（权限/占用）不致命：statePath 保持原样，调用方照常报错。
func quarantineState(xmdeDir, mode string) string {
	if xmdeDir == "" || mode == "" {
		return "the corrupt state file was left in place"
	}
	p := statePath(xmdeDir, mode)
	if _, err := os.Stat(p); err != nil {
		return "the corrupt state file was left in place"
	}
	dst := fmt.Sprintf("%s.corrupt-%s", p, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.Rename(p, dst); err != nil {
		log.Printf("isolate: could not quarantine corrupt state %s: %v", p, err)
		return "the corrupt state file was left in place (rename failed)"
	}
	log.Printf("isolate: quarantined corrupt state file as %s (kept for diagnosis; delete it once the container is gone)", dst)
	return fmt.Sprintf("the corrupt state file was preserved as %s for diagnosis", dst)
}

func clearState(xmdeDir, mode string) {
	if xmdeDir == "" || mode == "" {
		return
	}
	_ = os.Remove(statePath(xmdeDir, mode))
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

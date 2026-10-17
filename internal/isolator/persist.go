package isolator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

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

// loadState 读取隔离会话状态（第七轮审计 F-51）。
//
// 旧实现 `_ = json.Unmarshal(data, &s)` 静默吞掉解析错误并返回零值——调用方
// 无法区分「从未 setup 过」与「状态文件损坏」。现在区分两者：文件不存在是正常
// 情况（静默返回零值），解析失败则明确告警，让用户/排障者知道所有权凭证丢了
// （容器可能泄漏，需要手工 docker rm）。
func loadState(xmdeDir, mode string) persistedState {
	var s persistedState
	if xmdeDir == "" || mode == "" {
		return s
	}
	p := statePath(xmdeDir, mode)
	data, err := os.ReadFile(p)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("isolate: could not read state %s: %v", p, err)
		}
		return s
	}
	if err := json.Unmarshal(data, &s); err != nil {
		log.Printf("isolate: WARNING: state file %s is corrupt (%v) — ownership/container handle lost; "+
			"if a container is still running, remove it manually with `docker rm -f`", p, err)
		return persistedState{}
	}
	return s
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

package downloader

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xmdemt/xdev/internal/util"
)

// DownloadStatus 表示下载任务的状态
type DownloadStatus string

const (
	StatusDownloading DownloadStatus = "downloading"
	StatusPaused      DownloadStatus = "paused"
	StatusCompleted   DownloadStatus = "completed"
	StatusFailed      DownloadStatus = "failed"
	StatusCancelled   DownloadStatus = "cancelled"
)

// Task 表示一个下载任务
type Task struct {
	ID           string         `json:"id"`
	Tool         string         `json:"tool"`
	Version      string         `json:"version"`
	URL          string         `json:"url"`
	Dest         string         `json:"dest"`
	Status       DownloadStatus `json:"status"`
	Progress     float64        `json:"progress"`
	Speed        string         `json:"speed"`
	ETA          string         `json:"eta"`
	Downloaded   int64          `json:"downloaded"`
	Total        int64          `json:"total"`
	ErrorMessage string         `json:"errorMessage,omitempty"`
	BatchIndex   int            `json:"batchIndex,omitempty"`
	BatchTotal   int            `json:"batchTotal,omitempty"`

	// 内部字段
	ctx      context.Context
	cancel   context.CancelFunc
	opts     Options
	mu       sync.Mutex
	pausedAt int64 // 暂停时已下载的字节数

	// pauseRequested 是「暂停 vs 取消」的**权威判据**（F-14）。
	//
	// 修复前判据是 `pausedAt > 0`，而 pausedAt 取自 task.Downloaded —— 后者
	// 只由 progressWriter 的进度回调写入，回调又被 `p.total > 0` 门控。服务器
	// 不返回 Content-Length（chunked，ContentLength == -1）时 total == -1，
	// 回调永不触发 → pausedAt 恒 0 → 收尾时判成 Cancelled：覆盖刚写入的
	// StatusPaused，并 os.Remove 掉用户只想暂停的那个 .part。
	//
	// 标志由 Pause 在锁内同步置位，不依赖任何进度回调，因此 chunked 响应、
	// 以及「首个 200ms 节流窗口内就暂停」都能正确判为暂停。Cancel 会清掉它。
	pauseRequested bool
}

// Manager 管理所有下载任务
type Manager struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// GetStatus 安全获取任务状态
func (t *Task) GetStatus() DownloadStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Status
}

// GetSnapshot 安全获取任务快照（用于序列化）
func (t *Task) GetSnapshot() Task {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Task{
		ID:           t.ID,
		Tool:         t.Tool,
		Version:      t.Version,
		URL:          t.URL,
		Dest:         t.Dest,
		Status:       t.Status,
		Progress:     t.Progress,
		Speed:        t.Speed,
		ETA:          t.ETA,
		Downloaded:   t.Downloaded,
		Total:        t.Total,
		ErrorMessage: t.ErrorMessage,
	}
}

// NewManager 创建下载管理器
func NewManager() *Manager {
	return &Manager{
		tasks: make(map[string]*Task),
	}
}

// StartDownload 启动一个新的下载任务
func (m *Manager) StartDownload(id, tool, ver string, opts Options) error {
	m.mu.Lock()
	if _, exists := m.tasks[id]; exists {
		m.mu.Unlock()
		return fmt.Errorf("download task %s already exists", id)
	}

	ctx, cancel := context.WithCancel(context.Background())
	task := &Task{
		ID:      id,
		Tool:    tool,
		Version: ver,
		URL:     opts.URL,
		Dest:    opts.Dest,
		Status:  StatusDownloading,
		ctx:     ctx,
		cancel:  cancel,
		opts:    opts,
	}
	m.tasks[id] = task
	m.mu.Unlock()

	go m.runDownload(task)
	return nil
}

// runDownload 执行下载
func (m *Manager) runDownload(task *Task) {
	// 如果批量下载，在 ProgressDesc 前加 [N/M] 前缀
	if task.BatchTotal > 0 {
		task.opts.ProgressDesc = fmt.Sprintf("[%d/%d] %s", task.BatchIndex, task.BatchTotal, task.opts.ProgressDesc)
	}

	// 包装 OnProgress 回调以更新任务状态
	originalProgress := task.opts.OnProgress
	task.opts.OnProgress = func(downloaded, total int64, elapsed time.Duration) {
		task.mu.Lock()
		task.Downloaded = downloaded
		task.Total = total
		if total > 0 {
			task.Progress = float64(downloaded) / float64(total) * 100
		}
		task.Speed = util.HumanSpeedStr(downloaded, elapsed)
		task.ETA = util.HumanETAStr(downloaded, total, elapsed)
		task.mu.Unlock()

		if originalProgress != nil {
			originalProgress(downloaded, total, elapsed)
		}
	}

	// 包装 OnProgressEx 回调以更新任务状态（含 speed/eta）
	originalProgressEx := task.opts.OnProgressEx
	task.opts.OnProgressEx = func(downloaded, total int64, speed int64, eta time.Duration) {
		task.mu.Lock()
		task.Downloaded = downloaded
		task.Total = total
		if total > 0 {
			task.Progress = float64(downloaded) / float64(total) * 100
		}
		if speed > 0 {
			task.Speed = util.HumanBytesStr(speed) + "/s"
		}
		if eta > 0 {
			task.ETA = util.FormatDurationStr(eta)
		}
		task.mu.Unlock()

		if originalProgressEx != nil {
			originalProgressEx(downloaded, total, speed, eta)
		}
	}

	// Ticket 21 (P1-02): capture the context THIS goroutine runs under.
	// Pause/Resume/Cancel replace task.ctx; a late-finishing goroutine from
	// a previous generation must not overwrite the state the newer
	// generation has already established.
	task.mu.Lock()
	dlCtx := task.ctx
	task.mu.Unlock()

	err := Download(dlCtx, task.opts)

	task.mu.Lock()
	// Superseded generation: the task moved on (Pause → Resume, or Cancel
	// already wrote a terminal state). Stay silent — writing here is what
	// used to resurrect "Cancelled" after a pause, deleting the .part file
	// the resumed download was using.
	if task.ctx != dlCtx {
		task.mu.Unlock()
		log.Printf("[download] 上一代下载 goroutine 收尾（已过期，跳过终态写入）")
		return
	}
	if err != nil {
		if dlCtx.Err() == context.Canceled {
			// F-14：暂停 vs 取消由 pauseRequested 判定（不再依赖进度回调
			// 写入的 pausedAt —— chunked 响应下它恒为 0）。pausedAt 只用于
			// 报告「暂停时已下载多少字节」，取盘上 .part 的真实大小兜底。
			if task.pauseRequested {
				task.Status = StatusPaused
				if onDisk := partialBytesForTask(task); onDisk > task.pausedAt {
					task.pausedAt = onDisk
				}
				task.Downloaded = task.pausedAt
			} else {
				task.Status = StatusCancelled
				// 取消时清理临时文件（F-12：按 URL 分代，全部代 + 历史命名）。
				// Ticket 21 (N-06): log instead of silently ignoring — on
				// Windows the writer may still hold the handle. The
				// .part.lock takeover semantics make the residue harmless.
				for _, partPath := range partPathsFor(task.opts.Dest, task.opts.Fallbacks()...) {
					// P2-5（Round 10）：走 discardPart，连带清掉 .meta。
					discardPart(partPath)
					if _, statErr := os.Stat(partPath); statErr == nil {
						log.Printf("[download] WARNING: 取消后删除临时文件失败 %s", partPath)
					}
				}
				log.Printf("[download] 已取消下载并清理临时文件: %s", task.opts.Dest)
			}
		} else {
			task.Status = StatusFailed
			task.ErrorMessage = err.Error()
		}
	} else {
		task.Status = StatusCompleted
		task.Progress = 100
	}
	task.mu.Unlock()
}

// Pause 暂停下载任务
//
// Ordering (ticket 21 / P1-02): the paused state is written BEFORE the
// context is cancelled. runDownload decides between "paused" and
// "cancelled" by reading pausedAt after the cancellation lands, so the
// reverse order left a window where it observed pausedAt==0 and deleted
// the .part file of a download the user only paused.
//
// The status gate (`!= StatusDownloading`) now also covers repeated Pause
// calls for free: the first call flips the status synchronously, so a
// second Pause is rejected and cannot overwrite pausedAt with a later
// byte count (N-07).
func (m *Manager) Pause(id string) error {
	m.mu.RLock()
	task, exists := m.tasks[id]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("download task %s not found", id)
	}

	task.mu.Lock()
	defer task.mu.Unlock()

	if task.Status != StatusDownloading {
		return fmt.Errorf("cannot pause task in status %s", task.Status)
	}

	// 记录暂停时已下载的字节数（必须先于 cancel —— 见上）。
	//
	// F-14：进度回调在 chunked（无 Content-Length）响应下永不触发，所以
	// task.Downloaded 会是 0。这里直接取盘上 .part 的真实大小作为兜底，
	// 让「已下载 N 字节」的报告对用户仍然有意义。
	task.pauseRequested = true
	task.pausedAt = task.Downloaded
	if onDisk := partialBytesForTask(task); onDisk > task.pausedAt {
		task.pausedAt = onDisk
	}
	task.Status = StatusPaused
	task.cancel()
	log.Printf("[download] 暂停下载: %s, 已下载 %d 字节", id, task.pausedAt)
	return nil
}

// Resume 恢复暂停的下载任务
func (m *Manager) Resume(id string) error {
	m.mu.RLock()
	task, exists := m.tasks[id]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("download task %s not found", id)
	}

	task.mu.Lock()
	if task.Status != StatusPaused {
		task.mu.Unlock()
		return fmt.Errorf("cannot resume task in status %s", task.Status)
	}

	// 创建新的 context
	ctx, cancel := context.WithCancel(context.Background())
	task.ctx = ctx
	task.cancel = cancel
	task.Status = StatusDownloading
	task.pausedAt = 0
	task.pauseRequested = false
	task.mu.Unlock()

	// Resume starts a NEW download goroutine generation while the paused one
	// may still be unwinding: its final Write can land after the fresh
	// goroutine seeks, interleaving one stale block into the new transfer.
	// This residual window is accepted and documented (ticket 21): the
	// SHA-256 gate (or the TOFU pin) rejects the resulting archive, and the
	// .part.lock (P1-01) keeps two live writers out of the same file.
	go m.runDownload(task)
	log.Printf("[download] 恢复下载: %s", id)
	return nil
}

// Cancel 取消下载任务并清理临时文件
//
// Ticket 21 (N-06): task.cancel is read and invoked under task.mu — Resume
// reassigns that field, so the previous unlocked read raced with it. The
// partial file is removed outside the lock (I/O), and a failed removal is
// logged rather than ignored: on Windows the writer may still hold the
// handle (no FILE_SHARE_DELETE), and the .part.lock takeover semantics
// make the residue harmless.
func (m *Manager) Cancel(id string) error {
	m.mu.RLock()
	task, exists := m.tasks[id]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("download task %s not found", id)
	}

	task.mu.Lock()
	if task.Status == StatusCompleted || task.Status == StatusCancelled {
		task.mu.Unlock()
		return nil
	}

	// 取消 context（锁内：与 Resume 的重新赋值互斥）
	task.cancel()
	task.Status = StatusCancelled
	task.pausedAt = 0 // 确保 pausedAt 为 0，这样 runDownload 不会将其标记为 paused
	// F-14：取消必须清掉暂停标志，否则收尾会走 Paused 分支。
	task.pauseRequested = false
	task.mu.Unlock()

	// 清理临时文件（F-12：按 URL 分代 —— 本 URL 的代 + 回退 URL 的代 +
	// 历史未分代命名，全部删掉，否则会留下孤儿 .part 占着缓存目录）。
	for _, partPath := range partPathsFor(task.opts.Dest, task.opts.Fallbacks()...) {
		// P2-5（Round 10）：走 discardPart，连带清掉 .meta。
		discardPart(partPath)
		if _, statErr := os.Stat(partPath); statErr == nil {
			log.Printf("[download] WARNING: 删除临时文件失败 %s（下次下载将接管该残留）", partPath)
		}
	}

	log.Printf("[download] 取消下载并清理临时文件: %s", id)
	return nil
}

// GetTask 获取下载任务状态
func (m *Manager) GetTask(id string) (*Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, exists := m.tasks[id]
	if !exists {
		return nil, fmt.Errorf("download task %s not found", id)
	}
	return task, nil
}

// GetAllTasks 获取所有下载任务
func (m *Manager) GetAllTasks() []*Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tasks := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		tasks = append(tasks, t)
	}
	return tasks
}

// RemoveTask 移除已完成的下载任务记录
func (m *Manager) RemoveTask(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, exists := m.tasks[id]
	if !exists {
		return fmt.Errorf("download task %s not found", id)
	}
	task.mu.Lock()
	status := task.Status
	task.mu.Unlock()

	if status == StatusDownloading || status == StatusPaused {
		return fmt.Errorf("cannot remove active task, cancel it first")
	}

	delete(m.tasks, id)
	return nil
}

// CleanupOnExit 在程序退出时清理所有活跃的下载任务
//
// Ticket 21 (N-06): status read and cancel happen under one task.mu
// acquisition (previously the field was read unlocked). task.mu is a leaf
// lock — nothing else is acquired while holding it — so taking it under
// m.mu.RLock cannot deadlock.
func (m *Manager) CleanupOnExit() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for id, task := range m.tasks {
		task.mu.Lock()
		active := task.Status == StatusDownloading || task.Status == StatusPaused
		if active {
			task.cancel()
		}
		task.mu.Unlock()

		if active {
			// 清理临时文件（F-12：按 URL 分代，全部代 + 历史命名）
			// P2-5（Round 10）：走 discardPart，连带清掉 .meta。
			for _, partPath := range partPathsFor(task.opts.Dest, task.opts.Fallbacks()...) {
				discardPart(partPath)
				if _, statErr := os.Stat(partPath); statErr == nil {
					log.Printf("[download] WARNING: 退出清理删除临时文件失败 %s", partPath)
				}
			}
			log.Printf("[download] 退出清理: 取消下载 %s 并删除临时文件 %s", id, task.opts.Dest)
		}
	}
}

// GetPartialFileSize 获取 .part 文件的大小（用于断点续传信息）。
//
// F-12：.part 按候选 URL 分代（<dest>.<sha256(url)[:16]>.part），单看 dest
// 无法定位文件。本函数（及 HasPartialFile/CleanupPartialFile）保留原签名以
// 兼容既有调用方与测试，语义为「该 dest 下**任意**分代的 .part 中最大的那
// 一个」——GUI/CLI 只关心「有没有残留、有多少字节」。精确到某个 URL 的
// 查询请用 GetPartialFileSizeForURL。
func GetPartialFileSize(dest string) int64 {
	return maxPartialFileSize(partialPathsInDir(dest))
}

// GetPartialFileSizeForURL 返回某个具体候选 URL 的 .part 大小。
func GetPartialFileSizeForURL(dest, rawURL string) int64 {
	fi, err := os.Stat(partPathFor(dest, rawURL))
	if err != nil {
		return 0
	}
	return fi.Size()
}

// HasPartialFile 检查是否有部分下载的文件（任意分代）。
func HasPartialFile(dest string) bool {
	return len(partialPathsInDir(dest)) > 0
}

// CleanupPartialFile 清理该 dest 下所有分代的 .part 残留（含历史未分代命名）
// 以及它们的 .part.lock。不存在时返回 nil，方便调用方幂等重试。
//
// P2-5：锁文件也必须清 —— util.AcquireFileLock 创建的 <part>.lock 修复前没有
// 任何代码删除（H15：清理后仍剩 2 个 .part.lock）。被活跃下载持有的锁不会被删
// （见 removePartLockIfUnheld）。
func CleanupPartialFile(dest string) error {
	var firstErr error
	for _, p := range partialPathsInDir(dest) {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
		if err := removePartLockIfUnheld(p + ".lock"); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// 「.part 已删但锁还在」的孤儿锁：按目录扫描分代锁名。
	for _, l := range partialLockPathsInDir(dest) {
		if err := removePartLockIfUnheld(l); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// P2-5（Round 10）：孤儿 .meta 同样要清 —— 否则「GUI 清缓存」之后目录里
	// 仍残留上一代内容的 ETag，下一次续传会误报 content changed。
	for _, mp := range partialMetaPathsInDir(dest) {
		if err := os.Remove(mp); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// partialPathsInDir 返回该 dest 下存在的全部 .part 路径：
// 分代命名 <dest>.<16hex>.part，以及历史未分代命名 <dest>.part。
//
// 调用方（Pause/Cancel/辅助函数）手里只有 dest，不知道当初是哪个候选 URL
// 生成的，所以按目录扫描 + 前缀匹配来找。刻意不用 filepath.Glob：glob 会把
// dest 里的 `[`、`*`、`?` 当元字符，而 dest 是拼出来的文件名（缓存目录 +
// 归档名），含这类字符时匹配会静默失配 → 留下孤儿 .part。
//
// 只接受 16 位十六进制分代，避免误伤 <dest>.something.part 这类无关文件；
// 也刻意不匹配 <dest>.<gen>.part.lock（锁文件不该在这里被删）。
func partialPathsInDir(dest string) []string {
	paths := make([]string, 0, 2)
	if _, err := os.Stat(dest + ".part"); err == nil {
		paths = append(paths, dest+".part")
	}
	dir := filepath.Dir(dest)
	prefix := filepath.Base(dest) + "."
	const suffix = ".part"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return paths
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		if isPartGeneration(strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)) {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths
}

// isPartGeneration 报告 gen 是否是合法的分代后缀（16 位小写十六进制）。
func isPartGeneration(gen string) bool {
	if len(gen) != partGenerationLen {
		return false
	}
	for _, r := range gen {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// partialLockPathsInDir 返回该 dest 下存在的全部分代 .part.lock 路径
// （含历史未分代命名 <dest>.part.lock）。
//
// P2-5：与 partialPathsInDir 同款「按目录扫描 + 前缀匹配」策略（刻意不用
// filepath.Glob —— dest 里的 `[`、`*`、`?` 会被当元字符而静默失配）。
// 单独存在是因为「.part 已被删、只剩锁」是最常见的孤儿形态：只扫 .part 的
// partialPathsInDir 在那种情况下返回空列表，锁就永远清不掉。
func partialLockPathsInDir(dest string) []string {
	paths := make([]string, 0, 2)
	if _, err := os.Stat(dest + ".part.lock"); err == nil {
		paths = append(paths, dest+".part.lock")
	}
	dir := filepath.Dir(dest)
	prefix := filepath.Base(dest) + "."
	const suffix = ".part.lock"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return paths
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		if isPartGeneration(strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)) {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths
}

// partialMetaPathsInDir 返回该 dest 下存在的全部内容身份旁路文件
// （`<dest>.part.meta` 与分代 `<dest>.<16hex>.part.meta`）。
//
// 第十轮审计 P2-5（P2）：`.meta` 此前没有任何目录级清扫路径 ——
// `CleanupPartialFile` 只扫 `*.part`、`CleanupAllPartialsInDir` 只扫
// `*.part` + `*.part.lock`。审计实测「GUI 清缓存」后目录仍残留
// `archive.zip.<gen>.part.meta`。
//
// 与 partialPathsInDir 同款「按目录扫描 + 前缀匹配」策略（刻意不用
// filepath.Glob —— dest 里的 `[`、`*`、`?` 会被当元字符而静默失配）。
func partialMetaPathsInDir(dest string) []string {
	paths := make([]string, 0, 2)
	if _, err := os.Stat(partValidatorFor(dest + ".part")); err == nil {
		paths = append(paths, partValidatorFor(dest+".part"))
	}
	dir := filepath.Dir(dest)
	prefix := filepath.Base(dest) + "."
	const suffix = ".part.meta"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return paths
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		if isPartGeneration(strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)) {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths
}

// maxPartialFileSize 返回给定路径中最大的文件大小（都不存在则为 0）。
func maxPartialFileSize(paths []string) int64 {
	var max int64
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.Size() > max {
			max = fi.Size()
		}
	}
	return max
}

// partialBytesForTask 返回该任务在盘上真实的 .part 字节数（F-14 的兜底来源：
// 进度回调在 chunked 响应下永不触发，只有盘上大小是可信的）。
//
// P2-7：必须**逐代查询本任务自己的候选 URL**（主 URL + FallbackURLs），
// 不能用「所有代中最大的那个」——修复前 A 代残留 10 000 000 字节、当前
// URL(B) 只剩 1024 字节时会把 A 的字节数当成 B 的进度上报给用户（H4）。
// 回退 URL 的代仍要计入：回退 URL 正在下载时 Pause 要能报出真实进度。
func partialBytesForTask(task *Task) int64 {
	var max int64
	for _, u := range task.opts.Fallbacks() {
		if size := GetPartialFileSizeForURL(task.opts.Dest, u); size > max {
			max = size
		}
	}
	return max
}

// CleanupAllPartialsInDir 清理指定目录下所有 .part 文件及其 .part.lock。
//
// F-12：分代命名形如 <archive>.zip.<16hex>.part，仍以 .part 结尾，因此原来
// 的 "*.part" glob 依然同时覆盖分代命名与历史未分代命名，无需改动。
//
// P2-5：锁文件（<part>.lock）也要清 —— 修复前它们没有任何代码删除，
// 目录级清理后仍会剩下孤儿锁。被活跃下载持有的锁不会被删
// （见 removePartLockIfUnheld）。
func CleanupAllPartialsInDir(dir string) error {
	matches, err := filepath.Glob(filepath.Join(dir, "*.part"))
	if err != nil {
		return err
	}
	for _, m := range matches {
		os.Remove(m)
		log.Printf("[download] 清理临时文件: %s", m)
		if lockErr := removePartLockIfUnheld(m + ".lock"); lockErr != nil {
			log.Printf("[download] WARNING: 清理锁文件失败 %s.lock: %v", m, lockErr)
		}
	}
	// 「.part 已删但锁还在」的孤儿锁：glob "*.part.lock" 不匹配 "*.part"，
	// 必须单独扫一遍。
	locks, err := filepath.Glob(filepath.Join(dir, "*.part.lock"))
	if err != nil {
		return err
	}
	for _, l := range locks {
		if err := removePartLockIfUnheld(l); err != nil {
			log.Printf("[download] WARNING: 清理锁文件失败 %s: %v", l, err)
		}
	}
	// P2-5（Round 10）：孤儿 .meta（内容身份校验器）也要清。它不匹配 "*.part"
	// 也不匹配 "*.part.lock"，此前**没有任何**目录级清扫路径覆盖它 —— 审计实测
	// 「GUI 清缓存」（本函数）之后目录里仍残留 `archive.zip.<gen>.part.meta`。
	metas, err := filepath.Glob(filepath.Join(dir, "*.part.meta"))
	if err != nil {
		return err
	}
	for _, mp := range metas {
		if err := os.Remove(mp); err != nil && !os.IsNotExist(err) {
			log.Printf("[download] WARNING: 清理校验器旁路文件失败 %s: %v", mp, err)
		} else {
			log.Printf("[download] 清理校验器旁路文件: %s", mp)
		}
	}
	return nil
}

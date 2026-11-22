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
	parent   context.Context // survives pause/resume for this logical request
	ctx      context.Context
	cancel   context.CancelFunc
	opts     Options
	mu       sync.Mutex
	cancelMu sync.Mutex // serialize UI cancellation with the request owner's final cleanup
	pausedAt int64      // 暂停时已下载的字节数

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

	// done 在 runDownload 退出时被 close，供 Cancel 等待「下载 goroutine
	// 确实已退出」后再删除 .part。
	//
	// 为什么需要它（2026-10-25，CI flaky：TestF14_Control_CancelOnChunkedDownload_DeletesPart）：
	// Cancel 取消 context 后**立即**调 discardPart 删除 .part，但此刻下载
	// goroutine 可能仍持有写入句柄（context 取消是异步的，正在写盘的
	// Write 调用还没返回）。Windows 上文件被占用时删除失败，留下 .part
	// 残留，测试断言 GetPartialFileSize==0 就红了。
	//
	// 旧实现在 manager.go 里把该失败降级为 log（Ticket 21 / N-06 注释
	// “on Windows the writer may still hold the handle”），属于已知缺口。
	// 这里用 done 等待收口，而不是靠日志掩盖。
	done chan struct{}
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
	return m.StartDownloadContext(context.Background(), id, tool, ver, opts)
}

// StartDownloadContext binds every generation of the task to its request.
func (m *Manager) StartDownloadContext(parent context.Context, id, tool, ver string, opts Options) error {
	if err := parent.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	if _, exists := m.tasks[id]; exists {
		m.mu.Unlock()
		return fmt.Errorf("download task %s already exists", id)
	}

	if opts.ToolName == "" {
		opts.ToolName = tool
	}
	if opts.Version == "" {
		opts.Version = ver
	}
	ctx, cancel := context.WithCancel(parent)
	task := &Task{
		parent:  parent,
		ID:      id,
		Tool:    tool,
		Version: ver,
		URL:     opts.URL,
		Dest:    opts.Dest,
		Status:  StatusDownloading,
		ctx:     ctx,
		cancel:  cancel,
		opts:    opts,
		done:    make(chan struct{}),
	}
	m.tasks[id] = task
	go m.runDownload(task, ctx, task.done, opts)
	m.mu.Unlock()
	return nil
}

// runDownload 执行下载
func (m *Manager) runDownload(task *Task, dlCtx context.Context, done chan struct{}, opts Options) {
	// 退出时关闭 done，让 Cancel 知道「写入句柄已释放、可以安全删 .part」。
	// 必须在函数最开头 defer，保证任何 return 路径（含 superseded 分支）都关闭。
	defer close(done)
	task.mu.Lock()
	if task.BatchTotal > 0 {
		opts.ProgressDesc = fmt.Sprintf("[%d/%d] %s", task.BatchIndex, task.BatchTotal, opts.ProgressDesc)
	}
	task.mu.Unlock()

	// 包装 OnProgress 回调以更新任务状态
	originalProgress := opts.OnProgress
	opts.OnProgress = func(downloaded, total int64, elapsed time.Duration) {
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
	originalProgressEx := opts.OnProgressEx
	opts.OnProgressEx = func(downloaded, total int64, speed int64, eta time.Duration) {
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

	err := Download(dlCtx, opts)

	task.mu.Lock()
	// Superseded generation: the task moved on (Pause → Resume, or Cancel
	// already wrote a terminal state). Stay silent — writing here is what
	// used to resurrect "Cancelled" after a pause, deleting the .part file
	// the resumed download was using.
	if task.ctx != dlCtx || task.Status == StatusCancelled {
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
				if err := removePartialFiles(task.opts.Dest, task.opts.Fallbacks()...); err != nil {
					task.ErrorMessage = err.Error()
					log.Printf("[download] cancel cleanup: %v", err)
				}
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

	// Resume inherits the same request lifetime as the original generation.
	parent := task.parent
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		task.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	task.ctx = ctx
	task.cancel = cancel
	task.Status = StatusDownloading
	task.pausedAt = 0
	task.pauseRequested = false
	// Resume 会为同一个 Task 启动**新一代** goroutine，必须重建 done，
	// 否则新 goroutine 退出时会对已关闭的 channel 再 close → panic。
	// （2026-10-25：首版修复未重建，TestPause_ImmediatelyResume_Works 捕获）
	task.done = make(chan struct{})
	go m.runDownload(task, ctx, task.done, task.opts)
	task.mu.Unlock()
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

	task.cancelMu.Lock()
	defer task.cancelMu.Unlock()
	task.mu.Lock()
	if task.Status == StatusCompleted {
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
	//
	// 2026-10-25 修复（CI flaky）：必须先等下载 goroutine 真正退出，否则
	// 它的写入句柄还没释放（context 取消是异步的），Windows 上删除会失败、
	// 留下 .part 残留（旧实现只把这个失败降级为 log）。
	if task.done != nil {
		select {
		case <-task.done:
		case <-time.After(5 * time.Second):
			log.Printf("[download] WARNING: 等待下载 goroutine 退出超时，仍尝试清理 %s", task.opts.Dest)
		}
	}
	return removePartialFiles(task.opts.Dest, task.opts.Fallbacks()...)
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
			if err := removePartialFiles(task.opts.Dest, task.opts.Fallbacks()...); err != nil {
				log.Printf("[download] exit cleanup %s: %v", id, err)
			}
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

// CleanupPartialFile removes data and validator files under the writer's lock.
func CleanupPartialFile(dest string) error {
	_, err := CleanupPartialFileResult(dest)
	return err
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

// CleanupAllPartialsInDir retains the historical error-only API.
func CleanupAllPartialsInDir(dir string) error {
	_, err := CleanupAllPartialsInDirResult(dir)
	return err
}

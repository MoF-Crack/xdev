package downloader

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
			// 检查是否是暂停（pausedAt > 0）还是取消
			if task.pausedAt > 0 {
				task.Status = StatusPaused
				task.Downloaded = task.pausedAt
			} else {
				task.Status = StatusCancelled
				// 取消时清理临时文件
				partPath := task.opts.Dest + ".part"
				// Ticket 21 (N-06): log instead of silently ignoring — on
				// Windows the writer may still hold the handle. The
				// .part.lock takeover semantics make the residue harmless.
				if rmErr := os.Remove(partPath); rmErr != nil && !os.IsNotExist(rmErr) {
					log.Printf("[download] WARNING: 取消后删除临时文件失败 %s: %v", partPath, rmErr)
				}
				log.Printf("[download] 已取消下载并清理临时文件: %s", partPath)
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

	// 记录暂停时已下载的字节数（必须先于 cancel —— 见上）
	task.pausedAt = task.Downloaded
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
	task.mu.Unlock()

	// 清理临时文件
	partPath := task.opts.Dest + ".part"
	if err := os.Remove(partPath); err != nil && !os.IsNotExist(err) {
		log.Printf("[download] WARNING: 删除临时文件失败 %s: %v（下次下载将接管该残留）", partPath, err)
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
			// 清理临时文件
			partPath := task.opts.Dest + ".part"
			if err := os.Remove(partPath); err != nil && !os.IsNotExist(err) {
				log.Printf("[download] WARNING: 退出清理删除临时文件失败 %s: %v", partPath, err)
			}
			log.Printf("[download] 退出清理: 取消下载 %s 并删除临时文件 %s", id, partPath)
		}
	}
}

// GetPartialFileSize 获取 .part 文件的大小（用于断点续传信息）
func GetPartialFileSize(dest string) int64 {
	partPath := dest + ".part"
	fi, err := os.Stat(partPath)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// HasPartialFile 检查是否有部分下载的文件
func HasPartialFile(dest string) bool {
	partPath := dest + ".part"
	_, err := os.Stat(partPath)
	return err == nil
}

// CleanupPartialFile 清理部分下载的临时文件
func CleanupPartialFile(dest string) error {
	partPath := dest + ".part"
	return os.Remove(partPath)
}

// CleanupAllPartialsInDir 清理指定目录下所有 .part 文件
func CleanupAllPartialsInDir(dir string) error {
	matches, err := filepath.Glob(filepath.Join(dir, "*.part"))
	if err != nil {
		return err
	}
	for _, m := range matches {
		os.Remove(m)
		log.Printf("[download] 清理临时文件: %s", m)
	}
	return nil
}

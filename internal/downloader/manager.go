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

	err := Download(task.ctx, task.opts)

	task.mu.Lock()
	if err != nil {
		if task.ctx.Err() == context.Canceled {
			// 检查是否是暂停（pausedAt > 0）还是取消
			if task.pausedAt > 0 {
				task.Status = StatusPaused
				task.Downloaded = task.pausedAt
			} else {
				task.Status = StatusCancelled
				// 取消时清理临时文件
				partPath := task.opts.Dest + ".part"
				os.Remove(partPath)
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

	// 记录暂停时已下载的字节数
	task.pausedAt = task.Downloaded
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

	go m.runDownload(task)
	log.Printf("[download] 恢复下载: %s", id)
	return nil
}

// Cancel 取消下载任务并清理临时文件
func (m *Manager) Cancel(id string) error {
	m.mu.RLock()
	task, exists := m.tasks[id]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("download task %s not found", id)
	}

	task.mu.Lock()
	status := task.Status
	task.mu.Unlock()

	if status == StatusCompleted || status == StatusCancelled {
		return nil
	}

	// 取消 context
	task.cancel()

	// 清理临时文件
	partPath := task.opts.Dest + ".part"
	os.Remove(partPath)

	task.mu.Lock()
	task.Status = StatusCancelled
	task.pausedAt = 0 // 确保 pausedAt 为 0，这样 runDownload 不会将其标记为 paused
	task.mu.Unlock()

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
func (m *Manager) CleanupOnExit() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for id, task := range m.tasks {
		task.mu.Lock()
		status := task.Status
		task.mu.Unlock()

		if status == StatusDownloading || status == StatusPaused {
			task.cancel()
			// 清理临时文件
			partPath := task.opts.Dest + ".part"
			os.Remove(partPath)
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

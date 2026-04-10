package tasks

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID        string
	Name      string
	StartedAt time.Time
	Cancel    context.CancelFunc
	Done      <-chan struct{}
	Killed    chan struct{}
	Info      TaskInfo
}

type TaskInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	StartedAt   time.Time `json:"started_at"`
	Status      string    `json:"status"`
	Progress    float32   `json:"progress"`
	Description string    `json:"description"`
}

type Manager struct {
	running    map[string]*Task
	mu         sync.RWMutex
	killChan   chan string
	killDone   chan string
	progress   map[string]float32
	progressMu sync.RWMutex
}

func NewManager() *Manager {
	return &Manager{
		running:  make(map[string]*Task),
		killChan: make(chan string, 10),
		killDone: make(chan string, 10),
		progress: make(map[string]float32),
	}
}

func (m *Manager) NewTask(ctx context.Context, name string, fn func(ctx context.Context) error) *Task {
	taskCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	task := &Task{
		ID:        uuid.New().String(),
		Name:      name,
		StartedAt: time.Now(),
		Cancel:    cancel,
		Done:      done,
		Killed:    make(chan struct{}),
		Info: TaskInfo{
			ID:        uuid.New().String(),
			Name:      name,
			StartedAt: time.Now(),
			Status:    "running",
		},
	}

	go func() {
		defer close(done)
		err := fn(taskCtx)
		if err != nil && err != context.Canceled {
			slog.Debug("task error", "task", name, "error", err)
		}
	}()

	m.mu.Lock()
	m.running[task.ID] = task
	m.mu.Unlock()

	go func() {
		<-done
		m.mu.Lock()
		t, ok := m.running[task.ID]
		if ok {
			t.Info.Status = "completed"
		}
		delete(m.running, task.ID)
		m.mu.Unlock()

		m.progressMu.Lock()
		delete(m.progress, task.ID)
		m.progressMu.Unlock()

		select {
		case m.killDone <- task.ID:
		default:
		}
	}()

	go func() {
		for {
			select {
			case <-taskCtx.Done():
				return
			case killID := <-m.killChan:
				if killID == task.ID {
					task.Cancel()
					close(task.Killed)
					task.Info.Status = "killed"
					return
				}
			}
		}
	}()

	return task
}

func (m *Manager) Kill(taskID string) error {
	m.mu.RLock()
	_, ok := m.running[taskID]
	m.mu.RUnlock()

	if !ok {
		return nil
	}

	m.killChan <- taskID
	<-m.killDone

	return nil
}

func (m *Manager) List() []TaskInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]TaskInfo, 0, len(m.running))
	for _, t := range m.running {
		m.progressMu.RLock()
		progress := m.progress[t.ID]
		m.progressMu.RUnlock()

		info := t.Info
		info.Progress = progress
		infos = append(infos, info)
	}

	return infos
}

func (m *Manager) Get(taskID string) *Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running[taskID]
}

func (m *Manager) SetProgress(taskID string, progress float32) {
	m.progressMu.Lock()
	m.progress[taskID] = progress
	m.progressMu.Unlock()

	m.mu.RLock()
	if t, ok := m.running[taskID]; ok {
		t.Info.Progress = progress
	}
	m.mu.RUnlock()
}

func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.RLock()
	tasks := make([]*Task, 0, len(m.running))
	for _, t := range m.running {
		tasks = append(tasks, t)
	}
	m.mu.RUnlock()

	for _, t := range tasks {
		slog.Info("stopping task", "task", t.Name)
		m.Kill(t.ID)
	}

	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		m.mu.RLock()
		remaining := len(m.running)
		m.mu.RUnlock()

		if remaining == 0 {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-timeout.C:
			slog.Warn("tasks did not stop in time", "remaining", remaining)
			return
		case <-ticker.C:
		}
	}
}

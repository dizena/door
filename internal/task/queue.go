package task

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

var (
	ErrTaskNotFound = errors.New("task not found")
	ErrQueueFull    = errors.New("task queue is full")
)

type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
)

// Task represents a persisted asynchronous operation (install, backup, build, etc.).
type Task struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Title      string     `json:"title"`
	Status     Status     `json:"status"`
	Progress   int        `json:"progress"`
	ErrorMsg   string     `json:"error_msg"`
	LogPath    string     `json:"log_path"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// LogSink allows running jobs to emit incremental log lines and progress updates.
type LogSink interface {
	Logf(format string, args ...any)
	SetProgress(pct int)
}

// Handler executes a queued task.
type Handler func(ctx context.Context, sink LogSink) error

type queuedJob struct {
	id      string
	handler Handler
}

// Queue manages bounded background workers backed by SQLite and streaming log files.
type Queue struct {
	db      *sql.DB
	logsDir string
	logger  *slog.Logger

	ch     chan queuedJob
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu   sync.RWMutex
	subs map[string][]chan string
}

// NewQueue starts a bounded worker pool for async panel tasks.
func NewQueue(db *sql.DB, logsDir string, workers int, logger *slog.Logger) (*Queue, error) {
	if workers <= 0 {
		workers = 2
	}
	taskLogsDir := filepath.Join(logsDir, "tasks")
	if err := os.MkdirAll(taskLogsDir, 0o750); err != nil {
		return nil, fmt.Errorf("create task logs dir: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{
		db:      db,
		logsDir: taskLogsDir,
		logger:  logger,
		ch:      make(chan queuedJob, 64),
		ctx:     ctx,
		cancel:  cancel,
		subs:    make(map[string][]chan string),
	}
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}
	return q, nil
}

// Submit persists a new task and enqueues it for execution.
func (q *Queue) Submit(ctx context.Context, kind, title string, fn Handler) (Task, error) {
	id, err := newTaskID()
	if err != nil {
		return Task{}, err
	}
	logPath := filepath.Join(q.logsDir, id+".log")

	_, err = q.db.ExecContext(ctx, `
INSERT INTO tasks (id, kind, title, status, progress, log_path)
VALUES (?, ?, ?, ?, 0, ?)
`, id, kind, title, string(StatusPending), logPath)
	if err != nil {
		return Task{}, fmt.Errorf("insert task: %w", err)
	}

	select {
	case q.ch <- queuedJob{id: id, handler: fn}:
	default:
		_, _ = q.db.ExecContext(ctx, `
UPDATE tasks SET status = ?, error_msg = ?, finished_at = CURRENT_TIMESTAMP WHERE id = ?
`, string(StatusFailed), ErrQueueFull.Error(), id)
		return Task{}, ErrQueueFull
	}

	return q.Get(ctx, id)
}

// Get returns a task by ID.
func (q *Queue) Get(ctx context.Context, id string) (Task, error) {
	var (
		t          Task
		status     string
		startedAt  sql.NullTime
		finishedAt sql.NullTime
	)
	err := q.db.QueryRowContext(ctx, `
SELECT id, kind, title, status, progress, error_msg, log_path, created_at, started_at, finished_at
FROM tasks WHERE id = ?
`, id).Scan(&t.ID, &t.Kind, &t.Title, &status, &t.Progress, &t.ErrorMsg, &t.LogPath, &t.CreatedAt, &startedAt, &finishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrTaskNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("query task %s: %w", id, err)
	}
	t.Status = Status(status)
	if startedAt.Valid {
		t.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		t.FinishedAt = &finishedAt.Time
	}
	return t, nil
}

// List returns the most recent tasks.
func (q *Queue) List(ctx context.Context, limit int) ([]Task, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := q.db.QueryContext(ctx, `
SELECT id, kind, title, status, progress, error_msg, log_path, created_at, started_at, finished_at
FROM tasks ORDER BY created_at DESC LIMIT ?
`, limit)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var out []Task
	for rows.Next() {
		var (
			t          Task
			status     string
			startedAt  sql.NullTime
			finishedAt sql.NullTime
		)
		if err := rows.Scan(&t.ID, &t.Kind, &t.Title, &status, &t.Progress, &t.ErrorMsg, &t.LogPath, &t.CreatedAt, &startedAt, &finishedAt); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		t.Status = Status(status)
		if startedAt.Valid {
			t.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			t.FinishedAt = &finishedAt.Time
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ReadLog returns the persisted log file content for a task (capped safely).
func (q *Queue) ReadLog(ctx context.Context, id string) (string, error) {
	t, err := q.Get(ctx, id)
	if err != nil {
		return "", err
	}
	f, err := os.Open(t.LogPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("open task log: %w", err)
	}
	defer f.Close()

	const maxBytes = 1 << 20 // 1 MiB tail cap
	data, err := io.ReadAll(io.LimitReader(f, maxBytes))
	if err != nil {
		return "", fmt.Errorf("read task log: %w", err)
	}
	return string(data), nil
}

// Subscribe registers a live log listener for SSE streaming.
func (q *Queue) Subscribe(taskID string) (<-chan string, func()) {
	ch := make(chan string, 32)
	q.mu.Lock()
	q.subs[taskID] = append(q.subs[taskID], ch)
	q.mu.Unlock()

	cancel := func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		list := q.subs[taskID]
		for i, c := range list {
			if c == ch {
				q.subs[taskID] = append(list[:i], list[i+1:]...)
				close(ch)
				break
			}
		}
		if len(q.subs[taskID]) == 0 {
			delete(q.subs, taskID)
		}
	}
	return ch, cancel
}

// Close stops accepting work and waits for active workers to finish.
func (q *Queue) Close() {
	q.cancel()
	q.wg.Wait()
}

func (q *Queue) worker(workerID int) {
	defer q.wg.Done()
	for {
		select {
		case <-q.ctx.Done():
			return
		case job := <-q.ch:
			q.runJob(workerID, job)
		}
	}
}

func (q *Queue) runJob(workerID int, job queuedJob) {
	defer func() {
		if r := recover(); r != nil {
			q.logger.Error("task worker panicked",
				"worker_id", workerID,
				"task_id", job.id,
				"panic", fmt.Sprintf("%v", r),
				"stack", string(debug.Stack()),
			)
			_, _ = q.db.ExecContext(context.Background(), `
UPDATE tasks SET status = ?, error_msg = ?, finished_at = CURRENT_TIMESTAMP WHERE id = ?
`, string(StatusFailed), fmt.Sprintf("internal panic: %v", r), job.id)
		}
	}()

	t, err := q.Get(q.ctx, job.id)
	if err != nil {
		return
	}

	_, _ = q.db.ExecContext(q.ctx, `
UPDATE tasks SET status = ?, started_at = CURRENT_TIMESTAMP WHERE id = ?
`, string(StatusRunning), job.id)

	logFile, err := os.OpenFile(t.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		_, _ = q.db.ExecContext(context.Background(), `
UPDATE tasks SET status = ?, error_msg = ?, finished_at = CURRENT_TIMESTAMP WHERE id = ?
`, string(StatusFailed), err.Error(), job.id)
		return
	}
	defer logFile.Close()

	sink := &taskSink{
		q:       q,
		taskID:  job.id,
		logFile: logFile,
	}
	sink.Logf("task started: %s", t.Title)

	err = job.handler(q.ctx, sink)
	if err != nil {
		sink.Logf("task failed: %v", err)
		_, _ = q.db.ExecContext(context.Background(), `
UPDATE tasks SET status = ?, error_msg = ?, finished_at = CURRENT_TIMESTAMP WHERE id = ?
`, string(StatusFailed), err.Error(), job.id)
		return
	}

	sink.SetProgress(100)
	sink.Logf("task completed successfully")
	_, _ = q.db.ExecContext(context.Background(), `
UPDATE tasks SET status = ?, progress = 100, finished_at = CURRENT_TIMESTAMP WHERE id = ?
`, string(StatusSuccess), job.id)
}

func (q *Queue) publishLine(taskID, line string) {
	q.mu.RLock()
	subs := append([]chan string(nil), q.subs[taskID]...)
	q.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- line:
		default:
		}
	}
}

type taskSink struct {
	q       *Queue
	taskID  string
	logFile *os.File
	mu      sync.Mutex
}

func (s *taskSink) Logf(format string, args ...any) {
	line := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	s.mu.Lock()
	_, _ = fmt.Fprintln(s.logFile, line)
	s.mu.Unlock()
	s.q.publishLine(s.taskID, line)
}

func (s *taskSink) SetProgress(pct int) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	_, _ = s.q.db.ExecContext(context.Background(), `UPDATE tasks SET progress = ? WHERE id = ?`, pct, s.taskID)
}

func newTaskID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "tsk_" + hex.EncodeToString(buf), nil
}

package task

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"door/internal/store"
)

func TestQueueExecutionAndLog(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	db, err := store.Open(ctx, filepath.Join(tmp, "door.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	q, err := NewQueue(db, tmp, 1, logger)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}
	defer q.Close()

	tsk, err := q.Submit(ctx, "demo", "Demo job", func(_ context.Context, sink LogSink) error {
		sink.SetProgress(50)
		sink.Logf("working on step 1")
		return nil
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, err := q.Get(ctx, tsk.ID)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got.Status == StatusSuccess {
			logText, err := q.ReadLog(ctx, tsk.ID)
			if err != nil {
				t.Fatalf("ReadLog failed: %v", err)
			}
			if !strings.Contains(logText, "working on step 1") {
				t.Fatalf("expected task log line, got %q", logText)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("task did not finish in time")
}

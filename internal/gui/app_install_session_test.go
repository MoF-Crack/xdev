package gui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/installer"
)

func awaitInstall(t *testing.T, s *installSession) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
		t.Fatal("session did not terminate")
	}
}

func TestInstallSessionResourceOwnershipAndRetryIdentity(t *testing.T) {
	var sessions installSessions
	first, err := sessions.begin(context.Background(), InstallSpec{Tool: "node", Version: "22"}, "node")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.begin(context.Background(), InstallSpec{Tool: "node", Version: "24"}, "node"); err == nil {
		t.Fatal("same tool accepted concurrently")
	}
	other, err := sessions.begin(context.Background(), InstallSpec{Tool: "go", Version: "1.27"}, "go")
	if err != nil {
		t.Fatal("different tool blocked:", err)
	}
	sessions.finish(other.id, nil)
	view, ok := sessions.finish(first.id, installer.ErrIncompleteInstall)
	if !ok || view.Status != "needs-repair" || !view.Terminal {
		t.Fatalf("bad incomplete outcome: %+v", view)
	}
	if _, ok := sessions.finish(first.id, nil); ok {
		t.Fatal("second terminal accepted")
	}
	if _, err := sessions.begin(context.Background(), InstallSpec{SessionID: first.id, Tool: "node"}, "node"); err == nil {
		t.Fatal("old identity reused")
	}
	retry, err := sessions.begin(context.Background(), InstallSpec{Tool: "node", Version: "22", Force: true}, "node")
	if err != nil || retry.id == first.id {
		t.Fatalf("retry: %v %+v", err, retry)
	}
	if _, ok := sessions.update(first.id, func(v *InstallSession) { v.Phase = "late" }); ok {
		t.Fatal("late progress accepted")
	}
	sessions.finish(retry.id, nil)
}

func TestInstallSessionCancelCommitArbitration(t *testing.T) {
	for n := 0; n < 50; n++ {
		var sessions installSessions
		s, _ := sessions.begin(context.Background(), InstallSpec{Tool: "node"}, "node")
		var cancelErr, commitErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _, cancelErr = sessions.requestCancel(s.id) }()
		go func() { defer wg.Done(); _, commitErr = sessions.commit(s.id) }()
		wg.Wait()
		if (cancelErr == nil) == (commitErr == nil) {
			t.Fatalf("both or neither won: cancel=%v commit=%v", cancelErr, commitErr)
		}
		view, _ := sessions.finish(s.id, nil)
		want := "completed"
		if cancelErr == nil {
			want = "cancelled"
		}
		if view.Status != want || view.CanCancel || view.CanPause {
			t.Fatalf("outcome %+v", view)
		}
	}
}

func TestInstallSessionAcceptedIsNotCompletedAndCancelWaitsForWorker(t *testing.T) {
	app := &App{}
	entered, cleanup := make(chan struct{}), make(chan struct{})
	s, err := app.launchInstallSession(InstallSpec{Tool: "node"}, "node", func(ctx context.Context, id string) error {
		close(entered)
		<-ctx.Done()
		<-cleanup
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := app.CancelInstall(s.id); err != nil {
		t.Fatal(err)
	}
	view, _ := app.GetInstallSession(s.id)
	if view.Terminal || view.Status != "cancelling" {
		t.Fatalf("ACK claimed completion: %+v", view)
	}
	if _, err := app.launchInstallSession(InstallSpec{Tool: "node"}, "node", func(context.Context, string) error { return nil }); err == nil {
		t.Fatal("resource released before cleanup")
	}
	close(cleanup)
	awaitInstall(t, s)
	view, _ = app.GetInstallSession(s.id)
	if view.Status != "cancelled" || !errors.Is(s.err, context.Canceled) {
		t.Fatalf("bad cancel outcome %+v", view)
	}
}

func TestInstallSessionTerminalOutcomesAndCacheInvalidation(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want string
	}{{nil, "completed"}, {installer.ErrIncompleteInstall, "needs-repair"}, {context.Canceled, "cancelled"}, {fmt.Errorf("broken"), "error"}} {
		t.Run(tt.want, func(t *testing.T) {
			app := &App{dashCache: &DashboardStats{}, dashExpire: time.Now().Add(time.Hour), toolsCache: map[string]toolsCacheEntry{"x": {}}, installSizeCache: []DiskUsageEntry{{}}, cacheSizeCache: []DiskUsageEntry{{}}}
			s, err := app.launchInstallSession(InstallSpec{Tool: "x"}, "x", func(context.Context, string) error { return tt.err })
			if err != nil {
				t.Fatal(err)
			}
			awaitInstall(t, s)
			v, err := app.GetInstallSession(s.id)
			if err != nil || v.Status != tt.want || !v.Terminal {
				t.Fatalf("terminal snapshot %+v %v", v, err)
			}
			if !app.dashExpire.IsZero() || len(app.toolsCache) != 0 || !app.installSizeExp.IsZero() || !app.cacheSizeExp.IsZero() {
				t.Fatal("mutation caches remained valid")
			}
		})
	}
}

func TestInstallSessionPostDownloadCancellationCannotCommit(t *testing.T) {
	var sessions installSessions
	s, _ := sessions.begin(context.Background(), InstallSpec{Tool: "node"}, "node")
	// The transfer has gone, but installation has not crossed BeforeCommit.
	sessions.update(s.id, func(v *InstallSession) { v.CanPause = false })
	if _, err := sessions.requestCancel(s.id); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.commit(s.id); !errors.Is(err, context.Canceled) {
		t.Fatalf("commit after cancel: %v", err)
	}
	v, _ := sessions.finish(s.id, context.Canceled)
	if v.Status != "cancelled" {
		t.Fatal(v)
	}
}

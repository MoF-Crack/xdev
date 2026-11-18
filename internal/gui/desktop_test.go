package gui

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDesktopConfirmationWaitsForAsyncAnswer(t *testing.T) {
	for _, want := range []bool{true, false} {
		callback := make(chan func(bool), 1)
		result := make(chan bool, 1)
		go func() {
			got, err := awaitDesktopConfirmation(context.Background(), func(answer func(bool)) { callback <- answer })
			if err != nil {
				result <- !want
				return
			}
			result <- got
		}()
		answer := <-callback
		select {
		case <-result:
			t.Fatal("dialog completed before asynchronous answer")
		default:
		}
		answer(want)
		answer(!want)
		select {
		case got := <-result:
			if got != want {
				t.Fatalf("answer=%v want=%v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("answer did not release waiter")
		}
	}
}
func TestDesktopConfirmationShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errResult := make(chan error, 1)
	callback := make(chan func(bool), 1)
	go func() {
		_, err := awaitDesktopConfirmation(ctx, func(answer func(bool)) { callback <- answer })
		errResult <- err
	}()
	answer := <-callback
	cancel()
	select {
	case err := <-errResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown left waiter")
	}
	answer(true)
	answer(false) // late/duplicate native callbacks must not block
}
func TestNoEnvWriteRejectsPersistentEditorsBeforeAccess(t *testing.T) {
	app := &App{noEnvWrite: true}
	calls := []func() error{
		func() error { return app.SetPathMode("direct") },
		func() error { return app.SetEnvVar("go", "GOPATH", "unused", "user") },
		func() error { return app.DeleteEnvVar("GOPATH", "user") },
		func() error { return app.DeleteEnvVarEntry("GOPATH", "machine") },
		func() error { return app.CleanEnvVarPath("PATH", "user") },
		func() error { return app.ActivateHook("bash") },
		func() error { return app.DeactivateHook() },
	}
	for i, call := range calls {
		if err := call(); err == nil {
			t.Fatalf("operation %d accepted host write", i)
		}
	}
	if r := app.CleanAllInvalidEnvVars("user"); len(r) != 1 || r[0].Action != "skipped" {
		t.Fatalf("batch cleanup: %+v", r)
	}
	// No configuration or environment enumeration is required to refuse writes.
}

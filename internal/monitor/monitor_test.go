package monitor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	testDebounce = 50 * time.Millisecond
	waitTimeout  = 2 * time.Second
	quietPeriod  = 400 * time.Millisecond
)

func startWatcher(t *testing.T, paths ...string) *Watcher {
	t.Helper()

	w, err := New(paths, testDebounce)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = w.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return w
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

func expectEvent(t *testing.T, w *Watcher, wantPath string) {
	t.Helper()

	select {
	case ev := <-w.Events():
		if ev.Path != wantPath {
			t.Errorf("expected event for %q, got %q", wantPath, ev.Path)
		}
	case <-time.After(waitTimeout):
		t.Fatalf("timed out waiting for event on %q", wantPath)
	}
}

func expectQuiet(t *testing.T, w *Watcher) {
	t.Helper()

	select {
	case ev := <-w.Events():
		t.Fatalf("unexpected event: %+v", ev)
	case <-time.After(quietPeriod):
	}
}

func TestNew_Errors(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
	}{
		{name: "no paths", paths: nil},
		{name: "missing parent dir", paths: []string{"/definitely/not/a/real/dir/file"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := New(tt.paths, testDebounce)
			if err == nil {
				_ = w.fsw.Close()

				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestWatcher_Events(t *testing.T) {
	tests := []struct {
		name   string
		action func(t *testing.T, target, other string)
		want   bool
	}{
		{
			name: "write to watched file",
			action: func(t *testing.T, target, _ string) {
				writeFile(t, target, "changed")
			},
			want: true,
		},
		{
			name: "burst of writes is debounced to one event",
			action: func(t *testing.T, target, _ string) {
				for range 5 {
					writeFile(t, target, "burst")
				}
			},
			want: true,
		},
		{
			name: "atomic replace via rename",
			action: func(t *testing.T, target, _ string) {
				tmp := target + ".tmp"
				writeFile(t, tmp, "replaced")

				err := os.Rename(tmp, target)
				if err != nil {
					t.Fatal(err)
				}
			},
			want: true,
		},
		{
			name: "removal",
			action: func(t *testing.T, target, _ string) {
				err := os.Remove(target)
				if err != nil {
					t.Fatal(err)
				}
			},
			want: true,
		},
		{
			name: "unrelated file in same dir is ignored",
			action: func(t *testing.T, _, other string) {
				writeFile(t, other, "noise")
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "sshd_config")
			other := filepath.Join(dir, "other")

			writeFile(t, target, "initial")

			w := startWatcher(t, target)

			tt.action(t, target, other)

			if !tt.want {
				expectQuiet(t, w)

				return
			}

			expectEvent(t, w, target)
			expectQuiet(t, w)
		})
	}
}

func TestWatcher_StopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f")

	writeFile(t, target, "x")

	w, err := New([]string{target}, testDebounce)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() { errCh <- w.Run(ctx) }()

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("Run did not return after cancel")
	}

	select {
	case _, ok := <-w.Events():
		if ok {
			t.Error("expected Events channel to be closed")
		}
	case <-time.After(waitTimeout):
		t.Fatal("Events channel was not closed")
	}
}

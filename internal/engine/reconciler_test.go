package engine

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// syncBuffer lets the test read logs while the worker writes them
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func newTestLogger() (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}

	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

func startReconciler(t *testing.T, r *Reconciler) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		r.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for condition")
		}

		time.Sleep(5 * time.Millisecond)
	}
}

// counting is a Resource that records concurrent use of itself
type counting struct {
	fakeResource
	inFlight atomic.Int32
	overlap  atomic.Bool
	calls    atomic.Int32
}

func (c *counting) Check() (Drift, error) {
	if c.inFlight.Add(1) > 1 {
		c.overlap.Store(true)
	}

	time.Sleep(5 * time.Millisecond)
	c.calls.Add(1)
	c.inFlight.Add(-1)

	return Drift{}, nil
}

func TestReconciler_Reports(t *testing.T) {
	tests := []struct {
		name    string
		res     *fakeResource
		wantLog string
	}{
		{
			name:    "no drift",
			res:     &fakeResource{id: "r", typ: "t"},
			wantLog: "no drift",
		},
		{
			name: "drift",
			res: &fakeResource{id: "r", typ: "t", drift: Drift{
				Resource: "r", Severity: SeverityHigh, Current: "yes", Desired: "no",
			}},
			wantLog: "drift detected",
		},
		{
			name:    "check error",
			res:     &fakeResource{id: "r", typ: "t", err: errors.New("disk on fire")},
			wantLog: "check failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, buf := newTestLogger()
			rec := NewReconciler(logger, nil, []Resource{tt.res})
			startReconciler(t, rec)

			rec.Enqueue("r")
			waitFor(t, func() bool { return strings.Contains(buf.String(), tt.wantLog) })
		})
	}
}

func TestReconciler_EnqueueAllChecksEveryResource(t *testing.T) {
	logger, _ := newTestLogger()
	a := &counting{fakeResource: fakeResource{id: "a"}}
	b := &counting{fakeResource: fakeResource{id: "b"}}

	rec := NewReconciler(logger, nil, []Resource{a, b})
	startReconciler(t, rec)

	rec.EnqueueAll()
	waitFor(t, func() bool { return a.calls.Load() == 1 && b.calls.Load() == 1 })
}

func TestReconciler_NeverChecksResourceConcurrently(t *testing.T) {
	logger, _ := newTestLogger()
	c := &counting{fakeResource: fakeResource{id: "c"}}

	rec := NewReconciler(logger, nil, []Resource{c})
	startReconciler(t, rec)

	var wg sync.WaitGroup

	for range 20 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range 10 {
				rec.Enqueue("c")
			}
		}()
	}

	wg.Wait()
	waitFor(t, func() bool { return c.calls.Load() >= 1 })
	time.Sleep(100 * time.Millisecond)

	if c.overlap.Load() {
		t.Error("resource was checked concurrently with itself")
	}
}

func TestReconciler_DeduplicatesWaitingIDs(t *testing.T) {
	logger, _ := newTestLogger()
	c := &counting{fakeResource: fakeResource{id: "c"}}
	rec := NewReconciler(logger, nil, []Resource{c})

	// worker not started: repeated Enqueue must collapse to one entry
	for range 50 {
		rec.Enqueue("c")
	}

	if n := len(rec.queue); n != 1 {
		t.Fatalf("queue length = %d, want 1", n)
	}

	startReconciler(t, rec)
	waitFor(t, func() bool { return c.calls.Load() == 1 })
}

func TestReconciler_UnknownIDIgnored(t *testing.T) {
	logger, _ := newTestLogger()
	rec := NewReconciler(logger, nil, []Resource{&fakeResource{id: "a"}})

	rec.Enqueue("ghost")

	if n := len(rec.queue); n != 0 {
		t.Errorf("queue length = %d, want 0", n)
	}
}

func TestReconciler_StopsOnCancel(t *testing.T) {
	logger, _ := newTestLogger()
	rec := NewReconciler(logger, nil, []Resource{&fakeResource{id: "a"}})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		rec.Run(ctx)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

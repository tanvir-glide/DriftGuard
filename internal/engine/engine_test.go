package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tanvir-glide/driftguard/internal/config"
)

type testFakeResource struct {
	id, typ string
	drift   bool
	err     error
	checks  atomic.Int32
	reverts atomic.Int32
}

func (f *testFakeResource) Check() (Drift, error) {
	f.checks.Add(1)

	if f.drift {
		return Drift{Resource: f.id}, nil
	}

	return Drift{}, f.err
}
func (f *testFakeResource) Revert() error { f.reverts.Add(1); return nil }
func (f *testFakeResource) ID() string    { return f.id }
func (f *testFakeResource) Type() string  { return f.typ }

func TestBuild(t *testing.T) {
	rb := &config.Rulebook{Resources: []config.Resource{
		{Type: "a", ID: "a1"},
		{Type: "b", ID: "b1"},
		{Type: "a", ID: "a2"},
		{Type: "bad", ID: "x1"},
	}}

	factories := map[string]Factory{
		"a": func(r config.Resource) (Resource, error) { return &testFakeResource{id: r.ID, typ: r.Type}, nil },
		"b": func(r config.Resource) (Resource, error) { return nil, errors.New("boom") },
	}

	rs, skipped, err := NewRegistry(factories).Build(rb)

	if err == nil {
		t.Error("expected error from failing factory")
	}

	if len(rs) != 2 || rs[0].ID() != "a1" || rs[1].ID() != "a2" {
		t.Errorf("unexpected resources: %v", rs)
	}

	wantSkipped := []config.Resource{{Type: "bad", ID: "x1"}}
	if !reflect.DeepEqual(skipped, wantSkipped) {
		t.Errorf("skipped: got %v, want %v", skipped, wantSkipped)
	}
}

func TestReconciler(t *testing.T) {
	rb := &config.Rulebook{Resources: []config.Resource{
		{Type: "t", ID: "r1", Path: "/etc/one"},
		{Type: "t", ID: "r2", Path: "/etc/two"},
		{Type: "t", ID: "r3", Path: "/etc/one"},
		{Type: "t", ID: "skipped", Path: "/etc/one"},
	}}

	newRec := func() (*Reconciler, map[string]*testFakeResource) {
		fakes := map[string]*testFakeResource{
			"r1": {id: "r1", typ: "t", drift: true},
			"r2": {id: "r2", typ: "t"},
			"r3": {id: "r3", typ: "t", err: errors.New("unreadable")},
		}
		rs := []Resource{fakes["r1"], fakes["r2"], fakes["r3"]}

		return NewReconciler(slog.New(slog.NewTextHandler(io.Discard, nil)), rb, rs), fakes
	}

	waitFor := func(cond func() bool) {
		deadline := time.Now().Add(2 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatal("timed out waiting for condition")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	tests := []struct {
		name string
		run  func(*Reconciler)
		want map[string]int
	}{
		{"EnqueueAll", func(r *Reconciler) { r.EnqueueAll() }, map[string]int{"r1": 1, "r2": 1, "r3": 1}},
		{"CheckPath only owners", func(r *Reconciler) { r.CheckPath("/etc/one") }, map[string]int{"r1": 1, "r2": 0, "r3": 1}},
		{"CheckPath unknown path", func(r *Reconciler) { r.CheckPath("/etc/none") }, map[string]int{"r1": 0, "r2": 0, "r3": 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, fakes := newRec()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go rec.Run(ctx)

			tt.run(rec)

			waitFor(func() bool {
				for id, wantChecks := range tt.want {
					if int(fakes[id].checks.Load()) < wantChecks {
						return false
					}
				}
				return true
			})

			for id, f := range fakes {
				gotChecks := int(f.checks.Load())
				if gotChecks != tt.want[id] {
					t.Errorf("%s: checks want %d, got %d", id, tt.want[id], gotChecks)
				}

				if gotReverts := int(f.reverts.Load()); gotReverts != 0 {
					t.Errorf("%s: reconciler must never revert, got %d", id, gotReverts)
				}
			}
		})
	}
}

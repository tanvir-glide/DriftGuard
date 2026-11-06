package resources

import (
	"testing"

	"github.com/tanvir-glide/driftguard/internal/engine"
)

// testResourceContract holds every engine.Resource to the same rules.
// setup returns a resource that currently drifts
func testResourceContract(t *testing.T, setup func(t *testing.T) engine.Resource) {
	t.Helper()

	t.Run("drifted before revert", func(t *testing.T) {
		res := setup(t)

		d, err := res.Check()
		if err != nil {
			t.Fatalf("Check: %v", err)
		}

		if !d.Drifted() {
			t.Fatal("setup must produce drift")
		}

		if d.Resource != res.ID() {
			t.Errorf("Drift.Resource = %q, want %q", d.Resource, res.ID())
		}
	})

	t.Run("check after revert reports no drift", func(t *testing.T) {
		res := setup(t)

		err := res.Revert()
		if err != nil {
			t.Fatalf("Revert: %v", err)
		}

		d, err := res.Check()
		if err != nil {
			t.Fatalf("Check: %v", err)
		}

		if d.Drifted() {
			t.Errorf("drift remains after revert: %+v", d)
		}
	})

	t.Run("revert is idempotent", func(t *testing.T) {
		res := setup(t)

		for i := 0; i < 2; i++ {
			err := res.Revert()
			if err != nil {
				t.Fatalf("Revert #%d: %v", i+1, err)
			}
		}

		d, err := res.Check()
		if err != nil {
			t.Fatalf("Check: %v", err)
		}

		if d.Drifted() {
			t.Errorf("drift after second revert: %+v", d)
		}
	})
}

package engine

import (
	"context"
	"log/slog"
	"sync"

	"github.com/tanvir-glide/driftguard/internal/config"
)

// Reconciler checks resources against their desired state. It is
// level-triggered: every run compares current to desired state and never
// asks what changed. All checks happen on one worker goroutine, so a
// resource is never checked concurrently with itself.
//
// For now it only reports drift
//
//	Reverting arrives with modes and the safety layer, so nothing here changes the system
type Reconciler struct {
	logger    *slog.Logger
	resources map[string]Resource
	order     []string

	mu      sync.Mutex
	pending map[string]struct{}
	queue   chan string

	owners map[string][]string // path -> resource IDs that depend on it
}

// NewReconciler returns a Reconciler over the given resources
func NewReconciler(logger *slog.Logger, rb *config.Rulebook, resources []Resource) *Reconciler {
	r := &Reconciler{
		logger:    logger,
		resources: make(map[string]Resource, len(resources)),
		pending:   make(map[string]struct{}, len(resources)),
		// pending de-duplicates IDs, so at most len(resources) are queued
		// and Enqueue never blocks
		queue:  make(chan string, len(resources)),
		owners: make(map[string][]string),
	}

	if rb != nil {
		r.owners = rb.ResourceIDsByPath()
	}

	for _, res := range resources {
		r.resources[res.ID()] = res
		r.order = append(r.order, res.ID())
	}

	return r
}

// CheckPath enqueues every resource that owns the given path
func (r *Reconciler) CheckPath(path string) {
	for _, id := range r.owners[path] {
		r.Enqueue(id)
	}
}

// Enqueue schedules a check of the resource. An ID that is already waiting
// or unknown is ignored. Safe for concurrent use
func (r *Reconciler) Enqueue(id string) {
	if _, ok := r.resources[id]; !ok {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, waiting := r.pending[id]; waiting {
		return
	}

	r.pending[id] = struct{}{}
	r.queue <- id
}

// EnqueueAll schedules a check of every resource
func (r *Reconciler) EnqueueAll() {
	for _, id := range r.order {
		r.Enqueue(id)
	}
}

// Run checks queued resources one at a time until ctx is cancelled
func (r *Reconciler) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-r.queue:
			// Clear before checking so a change during the check
			// schedules another pass
			r.mu.Lock()
			delete(r.pending, id)
			r.mu.Unlock()

			r.reconcile(r.resources[id])
		}
	}
}

func (r *Reconciler) reconcile(res Resource) {
	log := r.logger.With("resource", res.ID(), "type", res.Type())

	drift, err := res.Check()
	if err != nil {
		log.Error("check failed", "error", err)

		return
	}

	if !drift.Drifted() {
		log.Debug("no drift")

		return
	}

	log.Warn("drift detected",
		"severity", drift.Severity,
		"current", drift.Current,
		"desired", drift.Desired,
	)
}

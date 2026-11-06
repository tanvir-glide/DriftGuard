package engine

import (
	"errors"
	"fmt"

	"github.com/tanvir-glide/driftguard/internal/config"
)

// Factory builds a Resource from a validated rulebook entry
type Factory func(config.Resource) (Resource, error)

// Registry maps rulebook type names to the factories that build them
// main fills it in; there is no self-registration
type Registry struct {
	factories map[string]Factory
}

// NewRegistry returns a Registry over the given factories
func NewRegistry(factories map[string]Factory) *Registry {
	return &Registry{factories: factories}
}

// Build creates one Resource per rulebook entry. Entries whose type has no
// factory yet are returned in skipped rather than failing the whole build,
// so a rulebook can list types that arrive in later milestones. All factory
// errors are reported together
func (r *Registry) Build(rb *config.Rulebook) (built []Resource, skipped []config.Resource, err error) {
	var errs []error

	for _, entry := range rb.Resources {
		factory, ok := r.factories[entry.Type]
		if !ok {
			skipped = append(skipped, entry)

			continue
		}

		res, ferr := factory(entry)
		if ferr != nil {
			errs = append(errs, fmt.Errorf("build %s (%s): %w", entry.ID, entry.Type, ferr))

			continue
		}

		built = append(built, res)
	}

	return built, skipped, errors.Join(errs...)
}

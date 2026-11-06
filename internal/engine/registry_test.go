package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/tanvir-glide/driftguard/internal/config"
)

type fakeResource struct {
	id, typ string
	drift   Drift
	err     error
	checks  int
}

func (f *fakeResource) Check() (Drift, error) { f.checks++; return f.drift, f.err }
func (f *fakeResource) Revert() error         { return nil }
func (f *fakeResource) ID() string            { return f.id }
func (f *fakeResource) Type() string          { return f.typ }

func fakeFactory(r config.Resource) (Resource, error) {
	return &fakeResource{id: r.ID, typ: r.Type}, nil
}

func TestRegistry_Build(t *testing.T) {
	failing := func(config.Resource) (Resource, error) { return nil, errors.New("boom") }

	tests := []struct {
		name        string
		factories   map[string]Factory
		types       []string
		wantBuilt   int
		wantSkipped int
		wantErr     string
	}{
		{
			name:      "builds registered types",
			factories: map[string]Factory{"ssh_config": fakeFactory},
			types:     []string{"ssh_config", "ssh_config"},
			wantBuilt: 2,
		},
		{
			name:        "skips types without factory",
			factories:   map[string]Factory{"ssh_config": fakeFactory},
			types:       []string{"ssh_config", "port", "firewall"},
			wantBuilt:   1,
			wantSkipped: 2,
		},
		{
			name:      "reports all factory errors together",
			factories: map[string]Factory{"ssh_config": failing, "port": failing},
			types:     []string{"ssh_config", "port"},
			wantErr:   "boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := &config.Rulebook{}
			for i, typ := range tt.types {
				rb.Resources = append(rb.Resources, config.Resource{Type: typ, ID: string(rune('a' + i))})
			}

			built, skipped, err := NewRegistry(tt.factories).Build(rb)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}

				if n := strings.Count(err.Error(), "boom"); n != 2 {
					t.Errorf("want both errors reported, got %d: %v", n, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(built) != tt.wantBuilt || len(skipped) != tt.wantSkipped {
				t.Errorf("built=%d skipped=%d, want %d/%d", len(built), len(skipped), tt.wantBuilt, tt.wantSkipped)
			}
		})
	}
}

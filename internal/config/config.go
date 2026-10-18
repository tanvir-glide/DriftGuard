// Package config parses and validates DriftGuard rulebooks
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"gopkg.in/yaml.v3"
)

// SupportedVersion is the only rulebook schema version understood so far
const SupportedVersion = "1.0"

// Resource types supported by the rulebook
const (
	TypeSSHConfig = "ssh_config"
	TypePort      = "port"
	TypeFirewall  = "firewall"
)

// Rulebook is the top-level document describing the desired baseline
type Rulebook struct {
	Version   string     `yaml:"version"`
	Resources []Resource `yaml:"resources"`
}

// Resource is a single baseline entry. Which fields apply depends on Type
type Resource struct {
	Type string `yaml:"type"`
	ID   string `yaml:"id"`

	// ssh_config
	Path   string     `yaml:"path,omitempty"`
	Checks []SSHCheck `yaml:"checks,omitempty"`

	// port
	Port         int    `yaml:"port,omitempty"`
	Protocol     string `yaml:"protocol,omitempty"`
	DesiredState string `yaml:"desired_state,omitempty"`

	// firewall
	Backend string         `yaml:"backend,omitempty"`
	Rules   []FirewallRule `yaml:"rules,omitempty"`
}

// SSHCheck pins one sshd_config parameter to a desired value
type SSHCheck struct {
	Parameter string `yaml:"parameter"`
	Desired   string `yaml:"desired"`
}

// FirewallRule describes one rule that must be present
type FirewallRule struct {
	Chain    string `yaml:"chain"`
	Protocol string `yaml:"protocol"`
	Port     int    `yaml:"port"`
	Action   string `yaml:"action"`
}

// Load reads and validates the rulebook at path
func Load(path string) (*Rulebook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rulebook: %w", err)
	}

	rb, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return rb, nil
}

// Parse decodes and validates a rulebook. Unknown fields are rejected so
// that typos in a security baseline never go silently unnoticed
func Parse(data []byte) (*Rulebook, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var rb Rulebook

	err := dec.Decode(&rb)
	if err != nil {
		return nil, fmt.Errorf("parse rulebook: %w", err)
	}

	err = rb.Validate()
	if err != nil {
		return nil, err
	}

	return &rb, nil
}

// Validate checks the rulebook for structural and semantic errors and
// reports all of them at once
func (rb *Rulebook) Validate() error {
	var errs []error

	if rb.Version != SupportedVersion {
		errs = append(errs, fmt.Errorf("unsupported version %q (want %q)", rb.Version, SupportedVersion))
	}

	if len(rb.Resources) == 0 {
		errs = append(errs, errors.New("rulebook defines no resources"))
	}

	seen := make(map[string]struct{}, len(rb.Resources))

	for i, r := range rb.Resources {
		if r.ID != "" {
			if _, dup := seen[r.ID]; dup {
				errs = append(errs, fmt.Errorf("resources[%d]: duplicate id %q", i, r.ID))
			}

			seen[r.ID] = struct{}{}
		}

		for _, err := range r.validate() {
			errs = append(errs, fmt.Errorf("resources[%d] (%s): %w", i, r.label(), err))
		}
	}

	return errors.Join(errs...)
}

// WatchPaths returns the sorted, de-duplicated filesystem paths that must
// be monitored for changes
func (rb *Rulebook) WatchPaths() []string {
	set := make(map[string]struct{})

	for _, r := range rb.Resources {
		if r.Path != "" {
			set[filepath.Clean(r.Path)] = struct{}{}
		}
	}

	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}

	sort.Strings(paths)

	return paths
}

// ResourceIDsByPath maps each watched path to the IDs of the resources
// that depend on it
func (rb *Rulebook) ResourceIDsByPath() map[string][]string {
	index := make(map[string][]string)

	for _, r := range rb.Resources {
		if r.Path == "" {
			continue
		}

		p := filepath.Clean(r.Path)
		index[p] = append(index[p], r.ID)
	}

	return index
}

func (r Resource) label() string {
	if r.ID != "" {
		return r.ID
	}

	return "no id"
}

func (r Resource) validate() []error {
	var errs []error

	if r.ID == "" {
		errs = append(errs, errors.New("id is required"))
	}

	switch r.Type {
	case TypeSSHConfig:
		errs = append(errs, r.validateSSH()...)
	case TypePort:
		errs = append(errs, r.validatePort()...)
	case TypeFirewall:
		errs = append(errs, r.validateFirewall()...)
	case "":
		errs = append(errs, errors.New("type is required"))
	default:
		errs = append(errs, fmt.Errorf("unknown type %q", r.Type))
	}

	return errs
}

func (r Resource) validateSSH() []error {
	var errs []error

	if r.Path == "" {
		errs = append(errs, errors.New("path is required"))
	} else if !filepath.IsAbs(r.Path) {
		errs = append(errs, fmt.Errorf("path %q must be absolute", r.Path))
	}

	if len(r.Checks) == 0 {
		errs = append(errs, errors.New("at least one check is required"))
	}

	for i, c := range r.Checks {
		if c.Parameter == "" || c.Desired == "" {
			errs = append(errs, fmt.Errorf("checks[%d]: parameter and desired are required", i))
		}
	}

	return errs
}

func (r Resource) validatePort() []error {
	var errs []error

	if !validPort(r.Port) {
		errs = append(errs, fmt.Errorf("port %d out of range 1-65535", r.Port))
	}

	if !oneOf(r.Protocol, "tcp", "udp") {
		errs = append(errs, fmt.Errorf("protocol %q must be tcp or udp", r.Protocol))
	}

	if !oneOf(r.DesiredState, "listening", "closed") {
		errs = append(errs, fmt.Errorf("desired_state %q must be listening or closed", r.DesiredState))
	}

	return errs
}

func (r Resource) validateFirewall() []error {
	var errs []error

	if !oneOf(r.Backend, "iptables") {
		errs = append(errs, fmt.Errorf("backend %q is not supported", r.Backend))
	}

	if len(r.Rules) == 0 {
		errs = append(errs, errors.New("at least one rule is required"))
	}

	for i, rule := range r.Rules {
		if rule.Chain == "" {
			errs = append(errs, fmt.Errorf("rules[%d]: chain is required", i))
		}

		if !oneOf(rule.Protocol, "tcp", "udp") {
			errs = append(errs, fmt.Errorf("rules[%d]: protocol %q must be tcp or udp", i, rule.Protocol))
		}

		if !validPort(rule.Port) {
			errs = append(errs, fmt.Errorf("rules[%d]: port %d out of range 1-65535", i, rule.Port))
		}

		if !oneOf(rule.Action, "ACCEPT", "DROP", "REJECT") {
			errs = append(errs, fmt.Errorf("rules[%d]: action %q must be ACCEPT, DROP or REJECT", i, rule.Action))
		}
	}

	return errs
}

func validPort(p int) bool {
	return p >= 1 && p <= 65535
}

func oneOf(v string, allowed ...string) bool {
	return slices.Contains(allowed, v)
}

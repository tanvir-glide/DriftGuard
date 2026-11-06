// Package resources holds the concrete engine.Resource implementations,
// one file per type
package resources

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tanvir-glide/driftguard/internal/config"
	"github.com/tanvir-glide/driftguard/internal/engine"
)

// SSHConfig guards parameters in an sshd_config file.
//
// It follows sshd's own rules: keywords are case-insensitive, the first
// occurrence wins, and everything from the first Match line on is
// conditional and ignored. Include files are not followed, so a value set
// by an included drop-in is invisible here
type SSHConfig struct {
	id     string
	path   string
	checks []config.SSHCheck
}

// NewSSHConfig builds an SSHConfig from a validated rulebook entry
func NewSSHConfig(r config.Resource) (engine.Resource, error) {
	if r.Path == "" || len(r.Checks) == 0 {
		return nil, fmt.Errorf("ssh_config needs a path and at least one check")
	}

	return &SSHConfig{id: r.ID, path: r.Path, checks: r.Checks}, nil
}

// ID returns the rulebook identifier
func (s *SSHConfig) ID() string { return s.id }

// Type returns the rulebook type
func (s *SSHConfig) Type() string { return config.TypeSSHConfig }

// Check reports every guarded parameter whose effective value differs from
// the baseline. An unset parameter counts as drift, because the baseline
// asks for an explicit value
func (s *SSHConfig) Check() (engine.Drift, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return engine.Drift{}, fmt.Errorf("read %s: %w", s.path, err)
	}

	bad := s.drifted(parseSSHConfig(data))
	if len(bad) == 0 {
		return engine.Drift{}, nil
	}

	current := make(map[string]string, len(bad))
	desired := make(map[string]string, len(bad))

	for _, c := range bad {
		current[c.check.Parameter] = c.current
		desired[c.check.Parameter] = c.check.Desired
	}

	return engine.Drift{
		Resource:  s.id,
		Current:   current,
		Desired:   desired,
		Severity:  engine.SeverityHigh,
		Timestamp: time.Now(),
	}, nil
}

// Revert fixes only the drifted parameters and leaves every other line
// untouched. With no drift it writes nothing, so running it twice is harmless
func (s *SSHConfig) Revert() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read %s: %w", s.path, err)
	}

	doc := parseSSHConfig(data)

	bad := s.drifted(doc)
	if len(bad) == 0 {
		return nil
	}

	for _, c := range bad {
		doc.set(c.check.Parameter, c.check.Desired)
	}

	err = WriteFileAtomic(s.path, []byte(doc.String()))
	if err != nil {
		return fmt.Errorf("revert %s: %w", s.id, err)
	}

	return nil
}

type sshDrift struct {
	check   config.SSHCheck
	current string
}

func (s *SSHConfig) drifted(doc *sshDoc) []sshDrift {
	var out []sshDrift

	for _, c := range s.checks {
		cur, _ := doc.value(c.Parameter)
		if !strings.EqualFold(cur, normalizeValue(c.Desired)) {
			out = append(out, sshDrift{check: c, current: cur})
		}
	}

	return out
}

// sshDoc is an sshd_config split into lines, with an index of the lines
// that count: the first global occurrence of each keyword
type sshDoc struct {
	lines   []string
	first   map[string]int // lowercase keyword -> line index
	matchAt int            // index of first Match line, or -1
}

func parseSSHConfig(data []byte) *sshDoc {
	doc := &sshDoc{
		lines:   strings.Split(string(data), "\n"),
		first:   make(map[string]int),
		matchAt: -1,
	}

	for i, line := range doc.lines {
		key, _, ok := splitSSHLine(line)
		if !ok {
			continue
		}

		key = strings.ToLower(key)

		if key == "match" {
			doc.matchAt = i

			break
		}

		if _, seen := doc.first[key]; !seen {
			doc.first[key] = i
		}
	}

	return doc
}

// value returns the normalized effective value of a keyword
func (d *sshDoc) value(keyword string) (string, bool) {
	i, ok := d.first[strings.ToLower(keyword)]
	if !ok {
		return "", false
	}

	_, val, _ := splitSSHLine(d.lines[i])

	return normalizeValue(val), true
}

// set rewrites the winning line in place, or inserts a new line before the
// first Match block (or at the end) so it stays in the global section
func (d *sshDoc) set(keyword, value string) {
	if i, ok := d.first[strings.ToLower(keyword)]; ok {
		line := d.lines[i]
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		orig, _, _ := splitSSHLine(line)
		d.lines[i] = indent + orig + " " + value

		return
	}

	at := d.matchAt
	if at < 0 {
		at = len(d.lines)
		if at > 0 && d.lines[at-1] == "" {
			at-- // keep the trailing newline last
		}
	}

	d.lines = append(d.lines[:at], append([]string{keyword + " " + value}, d.lines[at:]...)...)

	d.first[strings.ToLower(keyword)] = at
	if d.matchAt >= 0 {
		d.matchAt++
	}
}

// String renders the document back to file content
func (d *sshDoc) String() string {
	return strings.Join(d.lines, "\n")
}

// splitSSHLine splits "Keyword value" or "Keyword=value". ok is false for
// blank lines and comments
func splitSSHLine(line string) (key, val string, ok bool) {
	t := strings.TrimSpace(line)
	if t == "" || t[0] == '#' {
		return "", "", false
	}

	i := strings.IndexAny(t, " \t=")
	if i < 0 {
		return t, "", true
	}

	rest := strings.TrimLeft(t[i:], " \t")
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "="))

	return t[:i], rest, true
}

// normalizeValue collapses whitespace and drops quotes around each word
func normalizeValue(v string) string {
	fields := strings.Fields(v)
	for i, f := range fields {
		fields[i] = strings.Trim(f, `"`)
	}

	return strings.Join(fields, " ")
}

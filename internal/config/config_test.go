package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	const ssh = `
  - type: "ssh_config"
    id: "sshd"
    path: "/etc/ssh/sshd_config"
    checks:
      - parameter: "PermitRootLogin"
        desired: "no"
`

	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "valid ssh resource",
			yaml: "version: \"1.0\"\nresources:" + ssh,
		},
		{
			name:    "unsupported version",
			yaml:    "version: \"2.0\"\nresources:" + ssh,
			wantErr: "unsupported version",
		},
		{
			name:    "no resources",
			yaml:    "version: \"1.0\"\nresources: []\n",
			wantErr: "no resources",
		},
		{
			name:    "duplicate id",
			yaml:    "version: \"1.0\"\nresources:" + ssh + ssh,
			wantErr: "duplicate id",
		},
		{
			name:    "unknown type",
			yaml:    "version: \"1.0\"\nresources:\n  - type: \"cron\"\n    id: \"x\"\n",
			wantErr: "unknown type",
		},
		{
			name:    "missing id",
			yaml:    "version: \"1.0\"\nresources:\n  - type: \"port\"\n    port: 22\n    protocol: \"tcp\"\n    desired_state: \"listening\"\n",
			wantErr: "id is required",
		},
		{
			name:    "relative ssh path",
			yaml:    "version: \"1.0\"\nresources:\n  - type: \"ssh_config\"\n    id: \"a\"\n    path: \"sshd_config\"\n    checks:\n      - parameter: \"A\"\n        desired: \"b\"\n",
			wantErr: "must be absolute",
		},
		{
			name:    "ssh without checks",
			yaml:    "version: \"1.0\"\nresources:\n  - type: \"ssh_config\"\n    id: \"a\"\n    path: \"/etc/ssh/sshd_config\"\n",
			wantErr: "at least one check",
		},
		{
			name:    "port out of range",
			yaml:    "version: \"1.0\"\nresources:\n  - type: \"port\"\n    id: \"p\"\n    port: 70000\n    protocol: \"tcp\"\n    desired_state: \"listening\"\n",
			wantErr: "out of range",
		},
		{
			name:    "bad protocol",
			yaml:    "version: \"1.0\"\nresources:\n  - type: \"port\"\n    id: \"p\"\n    port: 22\n    protocol: \"icmp\"\n    desired_state: \"listening\"\n",
			wantErr: "must be tcp or udp",
		},
		{
			name:    "bad firewall action",
			yaml:    "version: \"1.0\"\nresources:\n  - type: \"firewall\"\n    id: \"f\"\n    backend: \"iptables\"\n    rules:\n      - chain: \"INPUT\"\n        protocol: \"tcp\"\n        port: 22\n        action: \"ALLOW\"\n",
			wantErr: "must be ACCEPT, DROP or REJECT",
		},
		{
			name:    "unsupported firewall backend",
			yaml:    "version: \"1.0\"\nresources:\n  - type: \"firewall\"\n    id: \"f\"\n    backend: \"pf\"\n    rules:\n      - chain: \"INPUT\"\n        protocol: \"tcp\"\n        port: 22\n        action: \"ACCEPT\"\n",
			wantErr: "not supported",
		},
		{
			name:    "unknown field is rejected",
			yaml:    "version: \"1.0\"\nresources:\n  - type: \"port\"\n    id: \"p\"\n    prot: 22\n",
			wantErr: "field prot not found",
		},
		{
			name:    "malformed yaml",
			yaml:    "version: [",
			wantErr: "parse rulebook",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb, err := Parse([]byte(tt.yaml))

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if rb == nil {
					t.Fatal("expected rulebook, got nil")
				}

				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		path    func(t *testing.T) string
		wantErr bool
	}{
		{
			name: "valid fixture",
			path: func(t *testing.T) string {
				return filepath.Join("..", "..", "testdata", "valid.yaml")
			},
		},
		{
			name: "example rulebook",
			path: func(t *testing.T) string {
				return filepath.Join("..", "..", "configs", "example.yaml")
			},
		},
		{
			name: "missing file",
			path: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "nope.yaml")
			},
			wantErr: true,
		},
		{
			name: "invalid content",
			path: func(t *testing.T) string {
				p := filepath.Join(t.TempDir(), "bad.yaml")

				err := os.WriteFile(p, []byte("version: \"9\"\n"), 0o600)
				if err != nil {
					t.Fatal(err)
				}

				return p
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(tt.path(t))

			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}

func TestRulebook_WatchPathsAndIndex(t *testing.T) {
	rb, err := Load(filepath.Join("..", "..", "testdata", "valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	wantPaths := []string{"/etc/ssh/sshd_config"}

	if got := rb.WatchPaths(); !reflect.DeepEqual(got, wantPaths) {
		t.Errorf("WatchPaths: want %v, got %v", wantPaths, got)
	}

	wantIndex := map[string][]string{
		"/etc/ssh/sshd_config": {"sshd_config", "sshd_config_extra"},
	}

	if got := rb.ResourceIDsByPath(); !reflect.DeepEqual(got, wantIndex) {
		t.Errorf("ResourceIDsByPath: want %v, got %v", wantIndex, got)
	}
}

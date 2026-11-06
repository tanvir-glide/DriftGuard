package resources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tanvir-glide/driftguard/internal/config"
	"github.com/tanvir-glide/driftguard/internal/engine"
)

func writeSSH(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "sshd_config")

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	return path
}

func newSSH(t *testing.T, path string, checks ...config.SSHCheck) engine.Resource {
	t.Helper()

	res, err := NewSSHConfig(config.Resource{
		Type: config.TypeSSHConfig, ID: "sshd", Path: path, Checks: checks,
	})
	if err != nil {
		t.Fatalf("NewSSHConfig: %v", err)
	}

	return res
}

var rootNo = config.SSHCheck{Parameter: "PermitRootLogin", Desired: "no"}

func TestSSHConfig_Check(t *testing.T) {
	passNo := config.SSHCheck{Parameter: "PasswordAuthentication", Desired: "no"}

	tests := []struct {
		name      string
		file      string
		checks    []config.SSHCheck
		wantDrift bool
		wantCur   map[string]string
	}{
		{
			name:   "matches",
			file:   "PermitRootLogin no\n",
			checks: []config.SSHCheck{rootNo},
		},
		{
			name:      "differs",
			file:      "PermitRootLogin yes\n",
			checks:    []config.SSHCheck{rootNo},
			wantDrift: true,
			wantCur:   map[string]string{"PermitRootLogin": "yes"},
		},
		{
			name:      "unset is drift",
			file:      "Port 22\n",
			checks:    []config.SSHCheck{rootNo},
			wantDrift: true,
			wantCur:   map[string]string{"PermitRootLogin": ""},
		},
		{
			name:   "keyword case-insensitive",
			file:   "permitrootlogin NO\n",
			checks: []config.SSHCheck{rootNo},
		},
		{
			name:   "equals separator",
			file:   "PermitRootLogin = no\n",
			checks: []config.SSHCheck{rootNo},
		},
		{
			name:   "quoted value",
			file:   "PermitRootLogin \"no\"\n",
			checks: []config.SSHCheck{rootNo},
		},
		{
			name:   "commented line ignored",
			file:   "#PermitRootLogin yes\nPermitRootLogin no\n",
			checks: []config.SSHCheck{rootNo},
		},
		{
			name:      "commented line is not a value",
			file:      "#PermitRootLogin no\n",
			checks:    []config.SSHCheck{rootNo},
			wantDrift: true,
			wantCur:   map[string]string{"PermitRootLogin": ""},
		},
		{
			name:   "first occurrence wins",
			file:   "PermitRootLogin no\nPermitRootLogin yes\n",
			checks: []config.SSHCheck{rootNo},
		},
		{
			name:      "first occurrence wins, bad first",
			file:      "PermitRootLogin yes\nPermitRootLogin no\n",
			checks:    []config.SSHCheck{rootNo},
			wantDrift: true,
			wantCur:   map[string]string{"PermitRootLogin": "yes"},
		},
		{
			name:      "Match block value does not count",
			file:      "Match User bob\n  PermitRootLogin no\n",
			checks:    []config.SSHCheck{rootNo},
			wantDrift: true,
			wantCur:   map[string]string{"PermitRootLogin": ""},
		},
		{
			name:   "value before Match counts",
			file:   "PermitRootLogin no\nMatch User bob\n  PermitRootLogin yes\n",
			checks: []config.SSHCheck{rootNo},
		},
		{
			name:      "only drifted params reported",
			file:      "PermitRootLogin no\nPasswordAuthentication yes\n",
			checks:    []config.SSHCheck{rootNo, passNo},
			wantDrift: true,
			wantCur:   map[string]string{"PasswordAuthentication": "yes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := newSSH(t, writeSSH(t, tt.file), tt.checks...)

			d, err := res.Check()
			if err != nil {
				t.Fatalf("Check: %v", err)
			}

			if d.Drifted() != tt.wantDrift {
				t.Fatalf("Drifted = %v, want %v (%+v)", d.Drifted(), tt.wantDrift, d)
			}

			if !tt.wantDrift {
				return
			}

			cur, _ := d.Current.(map[string]string)
			for k, want := range tt.wantCur {
				if cur[k] != want {
					t.Errorf("Current[%s] = %q, want %q", k, cur[k], want)
				}
			}

			if len(cur) != len(tt.wantCur) {
				t.Errorf("Current = %v, want %v", cur, tt.wantCur)
			}
		})
	}
}

func TestSSHConfig_Check_MissingFile(t *testing.T) {
	res := newSSH(t, filepath.Join(t.TempDir(), "nope"), rootNo)

	_, err := res.Check()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSSHConfig_Revert(t *testing.T) {
	passNo := config.SSHCheck{Parameter: "PasswordAuthentication", Desired: "no"}

	tests := []struct {
		name   string
		file   string
		checks []config.SSHCheck
		want   string
	}{
		{
			name:   "rewrites only the drifted line",
			file:   "# header\nPort 22\nPermitRootLogin yes\nUsePAM yes\n",
			checks: []config.SSHCheck{rootNo},
			want:   "# header\nPort 22\nPermitRootLogin no\nUsePAM yes\n",
		},
		{
			name:   "keeps indent and keyword spelling",
			file:   "  permitrootlogin=yes\n",
			checks: []config.SSHCheck{rootNo},
			want:   "  permitrootlogin no\n",
		},
		{
			name:   "fixes the winning occurrence only",
			file:   "PermitRootLogin yes\nPermitRootLogin no\n",
			checks: []config.SSHCheck{rootNo},
			want:   "PermitRootLogin no\nPermitRootLogin no\n",
		},
		{
			name:   "appends when unset",
			file:   "Port 22\n",
			checks: []config.SSHCheck{rootNo},
			want:   "Port 22\nPermitRootLogin no\n",
		},
		{
			name:   "appends and adds missing trailing newline",
			file:   "Port 22",
			checks: []config.SSHCheck{rootNo},
			want:   "Port 22\nPermitRootLogin no",
		},
		{
			name:   "inserts before Match so it stays global",
			file:   "Port 22\nMatch User bob\n  X11Forwarding yes\n",
			checks: []config.SSHCheck{rootNo},
			want:   "Port 22\nPermitRootLogin no\nMatch User bob\n  X11Forwarding yes\n",
		},
		{
			name:   "ignores commented line when inserting",
			file:   "#PermitRootLogin yes\n",
			checks: []config.SSHCheck{rootNo},
			want:   "#PermitRootLogin yes\nPermitRootLogin no\n",
		},
		{
			name:   "two drifted, one fine",
			file:   "PermitRootLogin yes\nPasswordAuthentication yes\nPort 22\n",
			checks: []config.SSHCheck{rootNo, passNo},
			want:   "PermitRootLogin no\nPasswordAuthentication no\nPort 22\n",
		},
		{
			name:   "two unset before Match keep order",
			file:   "Match all\n  X yes\n",
			checks: []config.SSHCheck{rootNo, passNo},
			want:   "PermitRootLogin no\nPasswordAuthentication no\nMatch all\n  X yes\n",
		},
		{
			name:   "no drift leaves file byte-identical",
			file:   "PermitRootLogin no\r\n  weird   spacing  \n",
			checks: []config.SSHCheck{rootNo},
			want:   "PermitRootLogin no\r\n  weird   spacing  \n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeSSH(t, tt.file)
			res := newSSH(t, path, tt.checks...)

			err := res.Revert()
			if err != nil {
				t.Fatalf("Revert: %v", err)
			}

			got, _ := os.ReadFile(path)
			if string(got) != tt.want {
				t.Errorf("file =\n%q\nwant\n%q", got, tt.want)
			}

			d, err := res.Check()
			if err != nil || d.Drifted() {
				t.Errorf("after revert: drift=%+v err=%v", d, err)
			}
		})
	}
}

func TestSSHConfig_Revert_PreservesMode(t *testing.T) {
	path := writeSSH(t, "PermitRootLogin yes\n")

	err := os.Chmod(path, 0o640)
	if err != nil {
		t.Fatal(err)
	}

	err = newSSH(t, path, rootNo).Revert()
	if err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", info.Mode().Perm())
	}
}

func TestSSHConfig_Contract(t *testing.T) {
	testResourceContract(t, func(t *testing.T) engine.Resource {
		path := writeSSH(t, "Port 22\nPermitRootLogin yes\nMatch all\n  X11Forwarding yes\n")

		return newSSH(t, path, rootNo, config.SSHCheck{Parameter: "PasswordAuthentication", Desired: "no"})
	})
}

func TestNewSSHConfig_Invalid(t *testing.T) {
	tests := []config.Resource{
		{Type: config.TypeSSHConfig, ID: "a", Checks: []config.SSHCheck{rootNo}},
		{Type: config.TypeSSHConfig, ID: "a", Path: "/etc/ssh/sshd_config"},
	}

	for _, r := range tests {
		_, err := NewSSHConfig(r)
		if err == nil || !strings.Contains(err.Error(), "ssh_config") {
			t.Errorf("want ssh_config error, got %v", err)
		}
	}
}

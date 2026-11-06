# DriftGuard [WIP]

DriftGuard is a lightweight background service that continuously monitors and enforces server configurations. It compares critical server state (SSH settings, listening ports, firewall rules) against a declarative baseline, and automatically reverts unauthorized changes.

## Status

**Goal 2 of 8 is complete.** DriftGuard parses and validates a rulebook, checks `ssh_config` resources at startup and on every change to the watched file, and logs drift. `ssh_config` revert is implemented and tested, but the daemon does **not** revert anything yet: modes and the safety layer arrive in M4. `port` and `firewall` entries are skipped with a warning until M3.

- [x] Daemon structure, rulebook parsing, file monitoring
- [x] SSH config checking and reversion (check wired in; revert waits for M4 safety layer)
- [ ] Port monitoring and firewall rule management
- [ ] Modes, rate limiting, circuit breaker, periodic reconcile, audit logging
- [ ] systemd integration, production hardening
- [ ] More resource types: file permissions, file integrity, sysctl, systemd services, port allowlist
- [ ] SIGHUP reload, `scan` report, approved-change windows, revert history
- [ ] Alerting, Prometheus metrics, self-protection


## Quick start

Requires the Go version in `go.mod`.

```sh
go mod tidy
go build -o driftguard ./cmd/driftguard

# Validate a rulebook and exit
./driftguard -check -config configs/example.yaml

# Run the daemon (logs a warning whenever a watched file changes)
./driftguard -config configs/example.yaml
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | `configs/example.yaml` | Path to the rulebook YAML |
| `-check` | `false` | Validate the rulebook and exit |
| `-debounce` | `500ms` | Coalesce bursts of file changes within this window |
| `-version` | `false` | Print the version and exit |

## Rulebook

A rulebook declares the desired baseline. Unknown fields are rejected, so a typo never silently weakens it.

```yaml
version: "1.0"
resources:
  - type: "ssh_config"
    id: "sshd_config"
    path: "/etc/ssh/sshd_config"
    checks:
      - parameter: "PermitRootLogin"
        desired: "no"

  - type: "port"
    id: "ssh_port"
    port: 22
    protocol: "tcp"
    desired_state: "listening"

  - type: "firewall"
    id: "iptables_rules"
    backend: "iptables"
    rules:
      - chain: "INPUT"
        protocol: "tcp"
        port: 22
        action: "ACCEPT"
```

See [`configs/example.yaml`](configs/example.yaml) for a fuller example.

## Project layout

```
.
├── cmd/driftguard/main.go
├── configs/example.yaml                    # Example rulebooks
├── internal
│   ├── config                              # Rulebook parsing and validation
│   ├── engine                              # Resource and Drift types, registry, reconciler
│   ├── monitor                             # Debounced file watching (fsnotify)
│   └── resources                           # Resource implementations (one file per type)
│       ├── fsutil.go                       # Atomic file write
│       └── ssh_config.go
├── testdata/valid.yaml                     # Test fixtures
├── go.mod
└── README.md
```

The safety layer (`engine/safety.go`) and the audit log (`internal/audit`) arrive with M4.

## Development

```sh
go test -race ./...
go vet ./...
gofmt -l .
```

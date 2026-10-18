# DriftGuard [WIP]

DriftGuard is a lightweight background service that continuously monitors and enforces server configurations. It compares critical server state (SSH settings, listening ports, firewall rules) against a declarative baseline, and automatically reverts unauthorized changes.

## Status

**Milestone 1 of 8 is complete.** DriftGuard currently parses a rulebook, validates it, and watches the files it references for changes. It does **not** yet check values or revert anything.

- [x] Daemon structure, rulebook parsing, file monitoring
- [ ] SSH config checking and reversion
- [ ] Port monitoring and firewall rule management
- [ ] Modes, rate limiting, circuit breaker, periodic reconcile, audit logging
- [ ] systemd integration, production hardening
- [ ] More resource types: file permissions, file integrity, sysctl, systemd services, port allowlist
- [ ] SIGHUP reload, `scan` report, approved-change windows, revert history
- [ ] Alerting, Prometheus metrics, self-protection


## Quick start

Requires Go 1.27.2 or newer (see `go.mod`).

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
cmd/driftguard/     Main binary
internal/
  config/           Rulebook parsing and validation
  monitor/          Debounced file watching (fsnotify)
  engine/           Resource and Drift types
configs/            Example rulebooks
testdata/           Test fixtures
```

Packages for reverting, audit logging, and rate limiting (`revert`, `logging`, `safety`) arrive with their milestones.

## Development

```sh
go test -race ./...
go vet ./...
gofmt -l .
```

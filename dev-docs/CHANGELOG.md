# Changelog

## 2026-05-04 - Initial setup

### Created
- Project structure: `cmd/asty/main.go`, `internal/platform/asty/`
- Go module with dependencies (NATS, zerolog, yaml)
- Configuration system with A_* env variables
- Service definition parser for .asty files
- Agent and Server skeletons with NATS connectivity
- Example .asty configs: gateway, xauth
- Landing page: `docs/index.html`
- Development docs in `dev-docs/`
- Build system: Makefile, .gitignore

### Updated documentation
- README.md: commercial product description (no Nomad mentions)
- dev-docs/architecture.md: technical context, Nomad mapping, process notes
- dev-docs/autoscaling.md: bot-proof autoscaling with RPS threshold
- dev-docs/configuration.md: all A_* variables, .asty format
- dev-docs/monitoring.md: metrics, UI, testing
- dev-docs/README.md: development plan, implementation phases

### Working
- ✅ Binary builds successfully
- ✅ Config loads from A_* env variables
- ✅ .asty files parse correctly (YAML)
- ✅ Agent connects to NATS and handles commands
- ✅ Process lifecycle: start/stop with graceful shutdown (SIGTERM → SIGKILL)
- ✅ Health checks: periodic HTTP probes with state tracking
- ✅ Metrics collection: CPU/Memory from /proc filesystem
- ✅ Artifact downloads: tar.gz extraction with SHA256 verification
- ✅ Agent can start/stop services, register health checks, collect metrics

### Next steps (see dev-docs/README.md)
1. Phase 1: Process management (start/stop, health checks, metrics)
2. Phase 2: Clustering (DNS discovery, leader election, state sync)
3. Phase 3: Basic scheduler (system + service types)
4. Phase 4: Locality-aware autoscaler
5. Phase 5: Deployments (rolling updates, canary)
6. Phase 6: Observability (API, UI, metrics)

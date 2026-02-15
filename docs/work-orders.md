# Work Orders — kafkaspectre

## WO-01: CLI Entry Point

**Goal:** Create `cmd/kafkaspectre/main.go` with Cobra wiring for `audit` subcommand.

### Details
- Makefile already references `./cmd/kafkaspectre` but directory does not exist
- Wire existing `internal/kafka.Inspector` and `internal/reporter` into Cobra command
- Flags: `--bootstrap-server`, `--auth-mechanism`, `--username`, `--password`, `--tls`, `--tls-cert`, `--tls-key`, `--tls-ca`, `--output` (json|text), `--exclude-internal`
- Version command with LDFLAGS from Makefile

### Acceptance
- `make build` produces working binary
- `./bin/kafkaspectre audit --bootstrap-server localhost:9092` connects and produces report
- `./bin/kafkaspectre version` prints version, commit, date

---

## WO-02: Tests

**Goal:** Add unit tests for inspector, reporter, and audit logic.

### Details
- `internal/kafka/inspector_test.go` — test SASL config builder, TLS config builder (no live Kafka needed)
- `internal/reporter/audit_test.go` — test risk classification, retention formatting, summary calculation
- `internal/reporter/audit_json_test.go` — test JSON output structure
- `internal/reporter/audit_text_test.go` — test text output format
- Target: >85% coverage on reporter package

### Acceptance
- `make test` passes with -race
- No flaky tests

---

## WO-03: CI Workflow

**Goal:** Add GitHub Actions CI matching other Spectre tools.

### Details
- `.github/workflows/ci.yml` — go build, test, vet, lint on push/PR
- Matrix: Go 1.25.x, ubuntu-latest
- golangci-lint step

### Acceptance
- CI runs on push to main and PRs
- Green badge

---

## WO-04: README Trim

**Goal:** Slim README to match Spectre family style. Move cleanup guide to docs/.

### Details
- Move "Topic Cleanup Guide" section (lines ~247-943) to `docs/cleanup-guide.md`
- Keep: description, status, installation, usage (audit + check), architecture, license
- Remove emoji headers, verbose examples
- Add badges (CI, Go version, license)

### Acceptance
- README under 150 lines
- `docs/cleanup-guide.md` contains the full guide
- No information lost, just relocated

---

## WO-05: Code Scanner (Phase 2)

**Goal:** Add `check` command — scan code for topic references, compare with cluster.

### Details
- `internal/scanner/` package with interface
- YAML/JSON scanner — find topic names in config files
- .env scanner — find KAFKA_TOPIC-style variables
- Regex fallback scanner for Go/Python/Java source
- `check` command: `--repo <path>` + `--bootstrap-server` → compare code refs vs cluster topics
- Status: OK, MISSING_IN_CLUSTER, UNREFERENCED_IN_REPO, UNUSED

### Acceptance
- `./bin/kafkaspectre check --repo ./my-app --bootstrap-server kafka:9092` produces report
- JSON and text output formats

---

## WO-06: Release v0.1.1

**Goal:** Tag and release with GoReleaser.

### Details
- `.goreleaser.yml` config — linux/darwin/windows, amd64/arm64
- GitHub release with changelog
- Binary artifacts attached
- Update README install section with release download

### Acceptance
- `gh release list` shows v0.1.1
- Binaries downloadable for all platforms
- `brew install` formula can follow later

---

---

## Phase 2: Hardening

---

## WO-07: Structured logging (slog)

**Goal:** Replace ad-hoc stderr writes with `log/slog`.

### Steps
1. Create `internal/logging/logging.go` — `Init(verbose bool)`
2. Replace fmt.Fprintf(stderr) with slog.Debug/Info/Warn
3. `--verbose` maps to LevelDebug, default LevelWarn
4. Structured fields: topic count, partition count, consumer group count, duration

### Acceptance
- Silent by default
- `make test` passes with -race

---

## WO-08: Connection resilience

**Goal:** Transient Kafka broker failures retry, auth failures fail fast.

### Steps
1. Create `internal/kafka/retry.go` — exponential backoff (max 3 attempts)
2. Classify: SASL auth failure → fail fast, broker unavailable/timeout → retry
3. `--timeout` caps total retry window

### Acceptance
- Broker unavailable retries with backoff
- SASL failures fail immediately
- `make test` passes with -race

---

## WO-09: SARIF output

**Goal:** `--output sarif` for GitHub Security tab.

### Steps
1. Create `internal/reporter/sarif.go` — SARIF 2.1.0 writer
2. Rule IDs: `kafkaspectre/UNUSED_TOPIC`, `kafkaspectre/HIGH_RISK_TOPIC`, etc.
3. Severity mapping

### Acceptance
- Valid SARIF 2.1.0 output
- `make test` passes with -race

---

## WO-10: Config file (.kafkaspectre.yaml)

**Goal:** Persistent defaults for broker address, auth, exclude patterns.

### Steps
1. Create `internal/config/config.go` — YAML loader
2. Fields: bootstrap_servers, auth_mechanism, exclude_topics, exclude_internal, format, timeout
3. CLI flags override config

### Acceptance
- Config file auto-loaded
- `make test` passes with -race

---

## WO-11: Baseline mode

**Goal:** Suppress known findings on repeat runs.

### Steps
1. Create `internal/baseline/baseline.go` — SHA-256 fingerprints
2. `--baseline` and `--update-baseline` flags

### Acceptance
- Second run shows only new findings
- `make test` passes with -race

---

## Phase 3: Distribution & Adoption

---

## WO-12: Docker image

**Goal:** `docker run kafkaspectre audit --bootstrap-server ...` — zero install.

### Steps
1. Create `Dockerfile` — multi-stage: Go builder → distroless
2. Multi-arch (amd64, arm64)
3. Push to `ghcr.io/ppiankov/kafkaspectre`
4. `docker-compose.yml` example: kafkaspectre + Kafka for local testing

### Acceptance
- Image < 20MB, multi-arch
- `docker run ghcr.io/ppiankov/kafkaspectre version` works

---

## WO-13: Homebrew formula

**Goal:** `brew install ppiankov/tap/kafkaspectre`

### Steps
1. GoReleaser `brews` section → ppiankov/homebrew-tap
2. Auto-updates on release

### Acceptance
- `brew install ppiankov/tap/kafkaspectre` works

---

## WO-14: GitHub Action

**Goal:** `uses: ppiankov/kafkaspectre-action@v1`

### Steps
1. Composite action repo
2. Inputs: `bootstrap-server`, `format`, `fail-on`, `args`
3. Download binary, run, upload SARIF

### Acceptance
- Action works in workflow

---

## WO-15: First-run experience

**Goal:** Helpful messages for new users.

### Steps
1. Summary header: broker address, topic count, consumer group count
2. No findings: "No issues detected. N topics scanned."
3. Connection banner on verbose
4. Exit code hints

### Acceptance
- First run shows helpful summary
- `make test` passes with -race

---

## WO-16: Standardized output header

**Goal:** Emit `{"tool": "kafkaspectre", "version": "...", "timestamp": "..."}` per spectrehub contract.

### Steps
1. Add header fields to JSON report struct
2. Backward compatible

### Acceptance
- JSON includes tool, version, timestamp
- spectrehub parses without errors

---

## WO-17: CONTRIBUTING.md

**Goal:** Community contribution guidelines.

### Steps
1. Prerequisites (Go 1.25+, Kafka for integration tests)
2. Build/test/lint commands
3. PR conventions
4. Architecture overview

### Acceptance
- CONTRIBUTING.md in repo root

---

## Non-Goals

- No web UI or dashboard
- No persistent state or database
- No topic auto-deletion
- No consumer lag monitoring (use Burrow for that)
- No Kafka Connect management

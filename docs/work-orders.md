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

## Non-Goals

- No web UI or dashboard
- No persistent state or database
- No topic auto-deletion
- No consumer lag monitoring (use Burrow for that)
- No Kafka Connect management

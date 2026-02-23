# kafkaspectre
[![CI](https://github.com/ppiankov/kafkaspectre/actions/workflows/ci.yml/badge.svg)](https://github.com/ppiankov/kafkaspectre/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ppiankov/kafkaspectre)](https://goreportcard.com/report/github.com/ppiankov/kafkaspectre)
[![Go 1.25+](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![ANCC](https://img.shields.io/badge/ANCC-compliant-brightgreen)](https://ancc.dev)

Kafka cluster auditor — identifies unused, orphaned, and misconfigured topics.

Connects to your Kafka cluster, analyzes topic metadata and consumer groups, produces risk-scored cleanup recommendations. Compares code references against live cluster state to detect drift.

## What kafkaspectre is

- **Cluster auditor** (`audit`) — connects to Kafka, identifies unused topics, scores risk, recommends cleanup
- **Drift detector** (`check`) — scans a code repository for topic references and compares with live cluster state
- **Risk scorer** — classifies unused topics as high/medium/low risk based on partition count and replication factor
- **Multi-format reporter** — JSON, SARIF 2.1.0, and human-readable text output
- **SpectreHub compatible** — JSON output includes tool/version/timestamp for aggregation

## What kafkaspectre is NOT

- Not a monitoring dashboard
- Not a consumer lag alerting system
- Not a topic management UI
- Not a replacement for Kafka's built-in tools
- Not long-term storage or trend analysis

## Philosophy

**Principiis obsta** — resist the beginnings. Address root causes, not symptoms.

- **Read-only** — never mutates cluster state, never deletes topics
- **Explicit consent** — requires bootstrap server address, SASL credentials via flags or config
- **Bounded execution** — configurable query timeout, deterministic completion
- **Mirrors, not oracles** — presents evidence and lets you decide

## Quick start

```bash
# Install with Homebrew (macOS / Linux)
brew install ppiankov/tap/kafkaspectre

# Or download binary (macOS Apple Silicon)
curl -LO https://github.com/ppiankov/kafkaspectre/releases/latest/download/kafkaspectre_darwin_arm64.tar.gz
tar -xzf kafkaspectre_darwin_arm64.tar.gz
sudo mv kafkaspectre /usr/local/bin/

# Or build from source
git clone https://github.com/ppiankov/kafkaspectre.git
cd kafkaspectre && make build
./bin/kafkaspectre version

# Audit a cluster
kafkaspectre audit --bootstrap-server localhost:9092

# Audit with JSON output
kafkaspectre audit --bootstrap-server localhost:9092 --output json

# Check code vs cluster drift
kafkaspectre check --repo ./my-app --bootstrap-server localhost:9092
```

### Agent integration

kafkaspectre is designed for autonomous agent use. Single binary, deterministic output, structured JSON, bounded jobs.

```bash
# Agent install (no brew needed)
curl -LO https://github.com/ppiankov/kafkaspectre/releases/latest/download/kafkaspectre_linux_amd64.tar.gz
tar -xzf kafkaspectre_linux_amd64.tar.gz && sudo mv kafkaspectre /usr/local/bin/
```

Agents: read [`SKILL.md`](SKILL.md) for commands, flags, JSON output structure, and exit codes.

Key patterns:
- `kafkaspectre audit --bootstrap-server <addr> --output json` — cluster audit with unused topics
- `kafkaspectre check --repo <path> --bootstrap-server <addr> --output json` — drift detection
- Exit code 6 means findings detected (unused topics or mismatches)

## Usage

| Command | Description |
|---------|-------------|
| `kafkaspectre audit` | Audit a Kafka cluster for unused topics |
| `kafkaspectre check` | Scan repository for topic references and compare with cluster |
| `kafkaspectre version` | Print version information |

### Key flags

```bash
# Authentication
kafkaspectre audit --bootstrap-server kafka:9092 --auth-mechanism SCRAM-SHA-256 --username reader --password secret

# TLS
kafkaspectre audit --bootstrap-server kafka:9092 --tls --tls-cert cert.pem --tls-key key.pem --tls-ca ca.pem

# Output formats
kafkaspectre audit --bootstrap-server kafka:9092 --output json
kafkaspectre audit --bootstrap-server kafka:9092 --output sarif
kafkaspectre check --repo ./app --bootstrap-server kafka:9092 --output json

# Exclusions
kafkaspectre audit --bootstrap-server kafka:9092 --exclude-internal --exclude-topics "_confluent-*"

# Timeout
kafkaspectre audit --bootstrap-server kafka:9092 --timeout 30s

# Config file (flags override)
# ~/.kafkaspectre.yaml
kafkaspectre audit   # uses config defaults
```

## Architecture

```
cmd/kafkaspectre/main.go         Cobra CLI: audit, check, version
internal/
  kafka/inspector.go             Kafka client (franz-go), metadata fetching
  kafka/retry.go                 Connection retry with exponential backoff
  reporter/                      Output formatters (JSON, SARIF, text)
  scanner/scanner.go             Repository code scanner for topic references
  config/config.go               YAML config loader (~/.kafkaspectre.yaml)
  logging/logging.go             Structured logging (slog)
```

### Audit flow

```
kafkaspectre audit
  │
  ├─ Load config defaults (if config file exists)
  ├─ Validate flags (bootstrap-server required, TLS pair check)
  │
  ├─ Connect to Kafka (with retry + backoff)
  ├─ Fetch metadata (topics, partitions, consumer groups)
  │
  ├─ For each topic:
  │   ├─ Skip if internal and --exclude-internal
  │   ├─ Skip if matches --exclude-topics pattern
  │   ├─ Check consumer group membership
  │   ├─ Classify risk (high/medium/low)
  │   └─ Generate recommendation
  │
  ├─ Compute cluster health score
  └─ Output (json | sarif | text)
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success (no findings) |
| `1` | Internal error |
| `2` | Invalid arguments |
| `3` | Not found (repo path, cluster unreachable) |
| `5` | Network error (Kafka connection failed) |
| `6` | Findings detected (unused topics or check mismatches) |

## Security

**Authentication:**
- SASL mechanisms: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
- TLS support with custom CA, client certificate, and private key
- Credentials via CLI flags or config file (`~/.kafkaspectre.yaml`)

**Safety:**
- Read-only cluster operations — never creates, deletes, or modifies topics
- No auto-deletion of topics — reports and recommends only
- Bounded execution time with `--timeout` (default 10s)
- No persistent state beyond generated reports

**Data:**
- Cluster metadata fetched on-demand, not cached
- SARIF output integrates with GitHub Code Scanning and security dashboards
- Config file permissions are user-controlled (`~/.kafkaspectre.yaml`)

## Known limitations

- **Cluster access required** — cannot audit without a live Kafka connection
- **Consumer group metadata only** — does not analyze actual message throughput or lag
- **No historical trend analysis** — single point-in-time audit (use SpectreHub for trends)
- **Pattern-based code scanning** — may miss dynamic topic name construction
- **Network timeout** — default 10s may be too short for large clusters (use `--timeout`)
- **No CRDs or operators** — imperative CLI workflow

## Project Status

**Status: Stable** · **v0.2.0** · Active development

| Milestone | Status |
|-----------|--------|
| Core audit functionality | Complete |
| Check command (drift detection) | Complete |
| SARIF output format | Complete |
| golangci-lint config | Complete |
| CI pipeline (test/lint/scan) | Complete |
| Homebrew distribution | Complete |
| SpectreHub compatibility | Complete |
| SKILL.md for agents | Complete |
| Baseline mode | Planned |

## License

[MIT](LICENSE)

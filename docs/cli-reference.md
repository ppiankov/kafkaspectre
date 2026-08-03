## Philosophy

**Principiis obsta** — resist the beginnings. Address root causes, not symptoms.

- **Read-only** — never mutates cluster state, never deletes topics
- **Explicit consent** — requires bootstrap server address, SASL credentials via flags or config
- **Bounded execution** — configurable query timeout, deterministic completion
- **Mirrors, not oracles** — presents evidence and lets you decide


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
kafkaspectre audit --bootstrap-server kafka:9092 --output spectrehub
kafkaspectre check --repo ./app --bootstrap-server kafka:9092 --output json

# Exclusions
kafkaspectre audit --bootstrap-server kafka:9092 --exclude-internal --exclude-topics "_confluent-*"

# Service-managed topics (Schema Registry, Connect, MirrorMaker 2, Streams) are
# held out of the analysis by default. Surface them with an explicit
# do-not-delete recommendation:
kafkaspectre audit --bootstrap-server kafka:9092 --include-managed

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
  reporter/                      Output formatters (JSON, SARIF, SpectreHub, text)
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
  └─ Output (json | sarif | spectrehub | text)
```


## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success (no findings) |
| `1` | Internal error |
| `2` | Invalid arguments |
| `3` | Not found (repo path, cluster unreachable) |
| `4` | Scan incomplete — consumer-group data could not be fully read; findings are unverified |
| `5` | Network error (Kafka connection failed) |
| `6` | Findings detected (unused topics or check mismatches) |


## Security

**Authentication:**
- SASL mechanisms: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
- TLS support with custom CA, client certificate, and private key
- Credentials via CLI flags or the `KAFKASPECTRE_USERNAME` and
  `KAFKASPECTRE_PASSWORD` environment variables. They are **not** readable from
  the config file — a `username` or `password` key there is a parse error, so a
  credential never sits in plaintext on disk by accident.
- Connection settings (`bootstrap_servers`, `auth_mechanism`, `tls`, `tls_cert`,
  `tls_key`, `tls_ca`) may live in `~/.kafkaspectre.yaml`

**Safety:**
- Read-only cluster operations — never creates, deletes, or modifies topics
- No auto-deletion of topics — reports and recommends only
- Bounded execution time with `--timeout` (default 60s)
- No persistent state beyond generated reports

**Data:**
- Cluster metadata fetched on-demand, not cached
- SARIF output integrates with GitHub Code Scanning and security dashboards
- Config file permissions are user-controlled (`~/.kafkaspectre.yaml`)


## Known limitations

- **Cluster access required** — cannot audit without a live Kafka connection
- **Consumer lag reported** — stale topics (high lag, active consumers) are classified separately from unused
- **Baseline mode** — snapshot once, report only deltas on subsequent runs
- **Scan reliability is reported, not assumed** — if consumer group data cannot
  be read, findings are marked UNVERIFIED, deletion advice is suppressed, and
  `reliability.consumer_groups_complete` is `false` in JSON output
- **Managed-topic detection is name-based and best-effort** — renamed Connect or
  Streams backing topics cannot be recognised automatically
- **No historical trend analysis** — single point-in-time audit (use SpectreHub for trends)
- **Pattern-based code scanning** — may miss dynamic topic name construction
- **Network timeout** — the connection Ping fails within 10s; the default 60s metadata timeout covers clusters with 200+ consumer groups
- **No CRDs or operators** — imperative CLI workflow


## Project Status

**Status: Stable** · **v0.2.4** · Active development

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
| Baseline mode | Complete |


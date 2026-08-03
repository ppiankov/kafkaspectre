# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed

- FetchOffsets calls now run concurrently (16 workers) instead of sequentially,
  making the tool usable on clusters with 200+ consumer groups without a manual
  timeout override. Default timeout raised from 10s to 60s.

### Fixed

- A degraded scan (incomplete consumer-group read) now exits code 4 instead of
  6 (findings) or 0 (success), so CI can distinguish unverified from actionable.

## [0.2.2] - 2026-08-02

### Fixed

- Removed dead Go Report Card badge from README.
- SpectreHub links in README now point to spectrehub.dev.

### Fixed

- Service-managed topics are no longer recommended for deletion. Schema Registry
  (`_schemas`), Kafka Connect, Confluent Platform, MirrorMaker 2, and Kafka
  Streams changelog/repartition topics are recognised and held out of the unused
  set. Deleting `_schemas` destroys every registered schema in a cluster.
- A failed consumer-group read no longer reports the whole cluster as unused.
  Scans track read completeness, mark affected findings `UNVERIFIED`, suppress
  deletion advice, and publish no cleanup list. Applies to `audit` and `check`.
- `summary.recommended_cleanup_topics` can no longer name a topic whose own
  recommendation forbids deleting it.
- Consumer-group topic attribution now includes live member assignments, not
  only committed offsets, so consumers that store offsets externally or have not
  committed yet no longer make their topics look unused.
- `Empty` and `Dead` consumer groups no longer count as active consumers.
- Partial `DescribeGroups` results are used instead of discarded, so one
  unreachable coordinator no longer blanks out consumer data for the cluster.
- Unused topics are ordered by risk descending in JSON, SARIF, and SpectreHub
  output, not only in the text report.
- The config parser accepts YAML block sequences at the parent key's
  indentation, which is valid YAML that was previously rejected outright.
- `--timeout 0` is rejected instead of being silently replaced by the default.
- `docs/SKILL.md` and the README quick-start describe commands and flags that
  actually exist; tests now fail the build if they drift again.
- Service backing topics are no longer counted as unused findings. A healthy
  cluster used to exit 6 on default flags purely because of `__consumer_offsets`,
  and advertise its partitions as reclaimable while labelling it DO NOT DELETE.
  They are reported under `managed_topics` and counted by
  `summary.managed_topics_held_out`.
- `ListGroups` partial results are used instead of aborting the whole command
  when one broker is unreachable.
- `check` no longer reports a repo-referenced internal topic as
  UNREFERENCED_IN_REPO, and still reports a managed topic that is referenced but
  genuinely absent from the cluster.

### Added

- `--include-managed` surfaces service-managed topics with an explicit
  do-not-delete recommendation.
- `reliability.consumer_groups_complete` and `reliability.read_errors` in JSON
  output, so consumers can distinguish a degraded scan from a clean one.
- `managed_by` and `abandoned_consumer_groups` on unused-topic findings.
- `tls`, `tls_cert`, `tls_key`, and `tls_ca` config keys. Credentials are read
  from `KAFKASPECTRE_USERNAME` / `KAFKASPECTRE_PASSWORD` and are rejected if
  placed in the config file.
- Repository scanning of `.properties` (Kafka's native config format) plus
  `.kt`, `.scala`, `.ts`, and `.js`; scan results report skipped files.
- CI test matrix now includes `windows-latest`, matching the published archives.

## [0.2.1] - 2026-02-23

### Added

- SpectreHub `spectre/v1` envelope output format (`--output spectrehub`)
- `HashBootstrap()` function for Kafka bootstrap server hashing
- Audit and check commands both support spectrehub output

## [0.2.0] - 2026-02-22

### Added

- SpectreHub compatibility: top-level `tool`, `version`, `timestamp` fields in JSON output
- Structured exit codes (0=success, 1=internal, 2=invalid args, 3=not found, 5=network, 6=findings)
- SKILL.md for agent integration
- CHANGELOG.md
- Homebrew tap integration (`brew install ppiankov/tap/kafkaspectre`)

### Changed

- README rewritten with security, exit codes, key flags, known limitations, and project status sections

## [0.1.1] - 2026-02-18

### Added

- GoReleaser multi-platform release configuration
- GitHub Actions CI workflow (test, lint, security scan)
- GitHub Actions release workflow with GoReleaser
- KafkaSpectre GitHub Action for CI integration
- Audit and check summary headers in text output
- Connection retry with exponential backoff
- SARIF 2.1.0 output format
- Structured logging with slog
- Code scanner for repository topic reference detection (`check` command)
- Configuration file support (`~/.kafkaspectre.yaml`)
- Dockerfile for containerized execution
- CONTRIBUTING.md

### Fixed

- GoReleaser action compatibility (v6 for v2 config)

## [0.1.0] - 2026-02-14

### Added

- Kafka cluster metadata inspection via franz-go
- Topic audit with unused topic detection and risk classification
- Consumer group analysis with topic-to-group mapping
- JSON and human-readable text output formats
- SASL authentication support (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)
- TLS support with custom CA, client cert, and key
- Topic exclusion by name or glob pattern
- Configurable query timeout
- Cluster health scoring (excellent/good/fair/poor/critical)
- Cleanup priority recommendations (top 10 candidates)

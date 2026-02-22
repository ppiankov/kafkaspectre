# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

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

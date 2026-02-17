# KafkaSpectre

[![CI](https://github.com/ppiankov/kafkaspectre/actions/workflows/ci.yml/badge.svg)](https://github.com/ppiankov/kafkaspectre/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/ppiankov/kafkaspectre)](https://github.com/ppiankov/kafkaspectre/blob/main/go.mod)
[![License](https://img.shields.io/github/license/ppiankov/kafkaspectre)](LICENSE)
[![Status: Phase 1 Ready](https://img.shields.io/badge/status-phase%201%20ready-brightgreen)](#status)
[![Status: Phase 2 Planned](https://img.shields.io/badge/status-phase%202%20planned-lightgrey)](#status)

KafkaSpectre is a **Go-based Kafka cluster auditor** that identifies unused, orphaned, and misconfigured topics.
It connects to your Kafka cluster to analyze topic metadata, consumer groups, and partition usage, providing risk-based cleanup recommendations.

## Status

- Phase 1 (Audit): ready
- Phase 2 (Check): planned

## Installation

### From source

```bash
git clone https://github.com/ppiankov/kafkaspectre.git
cd kafkaspectre
make build
```

Binary will be at `bin/kafkaspectre`.

### Binary releases

Coming soon.

## Usage

### Audit

```bash
./bin/kafkaspectre audit --bootstrap-server localhost:9092
```

### Check (coming soon)

```bash
./bin/kafkaspectre check --repo ./my-app --bootstrap-server localhost:9092
```

See `docs/cleanup-guide.md` for the full cleanup workflow.

## Architecture

```
┌─────────────┐    ┌──────────────┐    ┌─────────────┐    ┌──────────────┐
│   Scanner   │───▶│   Analyzer   │───▶│   Scorer    │───▶│   Reporter   │
│ (Static)    │    │  (Matching)  │    │ (Status)    │    │ (JSON/Text)  │
└─────────────┘    └──────────────┘    └─────────────┘    └──────────────┘
       │                   │
       ▼                   ▼
┌─────────────┐    ┌──────────────┐
│ Repo Files  │    │ Kafka Client │
│ (Topic Refs)│    │ (Metadata)   │
└─────────────┘    └──────────────┘
```

## License

MIT License — see LICENSE.

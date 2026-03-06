# kafkaspectre

[![CI](https://github.com/ppiankov/kafkaspectre/actions/workflows/ci.yml/badge.svg)](https://github.com/ppiankov/kafkaspectre/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ppiankov/kafkaspectre)](https://goreportcard.com/report/github.com/ppiankov/kafkaspectre)
[![ANCC](https://img.shields.io/badge/ANCC-compliant-brightgreen)](https://ancc.dev)

**kafkaspectre** — Kafka cluster auditor for unused topics and drift. Part of [SpectreHub](https://github.com/ppiankov/spectrehub).

## What it is

- Connects to Kafka and identifies unused, orphaned, and misconfigured topics
- Scans code repositories for topic references and compares against live cluster state
- Risk-scores cleanup recommendations by partition count and replication factor
- Detects consumer group lag and abandoned consumers
- Outputs text, JSON, SARIF, and SpectreHub formats

## What it is NOT

- Not a monitoring dashboard — point-in-time auditor
- Not a consumer lag alerting system
- Not a topic management UI
- Not a replacement for Kafka's built-in admin tools

## Quick start

### Homebrew

```sh
brew tap ppiankov/tap
brew install kafkaspectre
```

### From source

```sh
git clone https://github.com/ppiankov/kafkaspectre.git
cd kafkaspectre
make build
```

### Usage

```sh
kafkaspectre audit --brokers localhost:9092 --format json
```

## CLI commands

| Command | Description |
|---------|-------------|
| `kafkaspectre audit` | Audit cluster for unused and misconfigured topics |
| `kafkaspectre check` | Compare code topic references against live cluster |
| `kafkaspectre version` | Print version |

## SpectreHub integration

kafkaspectre feeds Kafka cluster findings into [SpectreHub](https://github.com/ppiankov/spectrehub) for unified visibility across your infrastructure.

```sh
spectrehub collect --tool kafkaspectre
```

## Safety

kafkaspectre operates in **read-only mode**. It inspects and reports — never modifies, deletes, or alters your topics.

## Documentation

| Document | Contents |
|----------|----------|
| [CLI Reference](docs/cli-reference.md) | Full command reference, flags, and configuration |

## License

MIT — see [LICENSE](LICENSE).

---

Built by [Obsta Labs](https://obstalabs.dev)

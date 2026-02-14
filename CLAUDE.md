# kafkaspectre

Kafka cluster auditor — identifies unused, orphaned, and misconfigured topics.

## What This Is

CLI tool that connects to Kafka, fetches topic metadata and consumer groups, and produces risk-scored audit reports. Part of the Spectre family (code-vs-reality drift detection).

## What This Is NOT

- Not a Kafka monitoring dashboard
- Not a consumer lag alerting system
- Not a topic management UI
- Not a replacement for Kafka's built-in tools

## Structure

```
cmd/kafkaspectre/main.go  — CLI entry point (Cobra)
internal/kafka/           — cluster inspector, types, franz-go client
internal/reporter/        — audit logic, JSON/text report generation
```

## Subcommands

- `audit` — cluster-only analysis: unused topics, risk scoring, cleanup recommendations
- `check` — (planned) scan code + compare with cluster state

## Code Style

- Go: minimal main.go delegating to internal/
- Cobra for CLI, franz-go for Kafka client (pure Go, zero CGO)
- Named returns only when needed for clarity
- Error wrapping with fmt.Errorf and %w

## Naming

- Go files: snake_case.go
- Go packages: short single-word (kafka, reporter)
- Conventional commits: feat:, fix:, docs:, test:, refactor:, chore:

## Testing

- `make test` (includes -race)
- `make lint` (golangci-lint)
- `go vet ./...`
- Tests are mandatory for all new code

## Anti-Patterns

- NEVER add CRDs or controllers — this is a CLI tool
- NEVER add a web UI or dashboard
- NEVER add persistent state — read cluster, produce report, exit
- NEVER auto-delete topics — report and recommend only
- NEVER suppress authentication errors — fail loud on bad creds

# kafkaspectre

Kafka cluster auditor for unused topics and code/cluster drift. Read-only.

## Install

```
brew install ppiankov/tap/kafkaspectre
```

Or via Go:

```
go install github.com/ppiankov/kafkaspectre/cmd/kafkaspectre@latest
```

## Commands

### kafkaspectre audit

Audits a Kafka cluster for topics with no active consumer groups.

**Flags:**
- `--bootstrap-server host:port` — Kafka bootstrap server(s), comma-separated (required)
- `--output json` — output as JSON
- `--output sarif` — SARIF format for CI integration
- `--output spectrehub` — SpectreHub aggregator format
- `--output text` — human-readable report (default)
- `--auth-mechanism PLAIN|SCRAM-SHA-256|SCRAM-SHA-512` — SASL mechanism
- `--username`, `--password` — SASL credentials
- `--tls`, `--tls-cert`, `--tls-key`, `--tls-ca` — TLS configuration
- `--exclude-internal` — exclude broker-internal (`__`-prefixed) topics
- `--exclude-topics pattern` — exclude topics by name or glob (repeatable)
- `--include-managed` — include service-managed topics (Schema Registry, Connect)
- `--timeout 10s` — Kafka query timeout
- `--verbose` — verbose logging

### kafkaspectre check

Scans a repository for topic references and compares them against the cluster.

**Flags:** `--repo path` (required), plus every flag listed for `audit`.

Per-topic status values: `OK`, `MISSING_IN_CLUSTER`, `UNREFERENCED_IN_REPO`, `UNUSED`.

### kafkaspectre version

Prints version, commit, and build date.

**JSON output (`audit --output json`):**
```json
{
  "tool": "kafkaspectre",
  "version": "0.1.0",
  "timestamp": "2026-08-02T00:00:00Z",
  "summary": {
    "cluster_name": "broker-1",
    "total_brokers": 1,
    "total_topics_analyzed": 1,
    "unused_topics": 1,
    "active_topics": 0,
    "unused_percentage": 0,
    "total_partitions": 0,
    "unused_partitions": 0,
    "total_consumer_groups": 0,
    "high_risk_count": 0,
    "medium_risk_count": 0,
    "low_risk_count": 0,
    "recommended_cleanup_topics": ["orders"],
    "cluster_health_score": "critical"
  },
  "unused_topics": [
    {
      "name": "orders",
      "partitions": 1,
      "replication_factor": 1,
      "reason": "No consumer groups found",
      "recommendation": "Safe to delete after confirmation",
      "risk": "low",
      "cleanup_priority": 1
    }
  ],
  "cluster_metadata": {
    "brokers": [{ "id": 1, "host": "broker-1", "port": 9092 }],
    "consumer_groups_count": 0,
    "fetched_at": "1970-01-01 00:00:00 UTC"
  },
  "reliability": { "consumer_groups_complete": true }
}
```

`unused_topics` is ordered by risk descending. Entries carry `managed_by` when the
topic is a service backing store, and `abandoned_consumer_groups` when the only
groups referencing it hold no live members.

**Exit codes:**
- 0: scan complete, no findings
- 1: internal error
- 2: invalid arguments
- 3: not found (repo path missing, cluster unreachable)
- 5: network error (Kafka connection failure)
- 6: findings detected

## Configuration

Optional `.kafkaspectre.yaml` in the working directory or `$HOME`. Keys:
`bootstrap_servers`, `auth_mechanism`, `exclude_topics`, `exclude_internal`,
`format`, `timeout`, `tls`, `tls_cert`, `tls_key`, `tls_ca`,
`managed_topics`.

`managed_topics` takes glob patterns for service backing topics this tool
cannot recognise by name — renamed Kafka Connect topics, custom Streams
application IDs. Declared topics are never recommended for deletion.

Credentials are never read from the config file. Supply them via the
`KAFKASPECTRE_USERNAME` and `KAFKASPECTRE_PASSWORD` environment variables.
Explicit flags override config values.

## Handoffs

- Output: `--output spectrehub`. Next: spectrehub for aggregation across scanners.
- Output: SARIF. Next: CI security gates.
- Refused questions: whether to delete a topic, how to remediate, risk acceptance decisions.

## What this does NOT do

- Does not remediate or modify Kafka clusters — every operation is read-only
- Does not store findings or manage a findings database
- Does not compute consumer lag or message throughput — consumer group metadata only
- Does not replace Kafka monitoring — point-in-time audit only

## Failure Modes

- Authentication failure: returns exit code 5 or 2 depending on cause. Distrust: all findings. Safe fallback: report scan failure, do not cache.
- Network timeout: returns exit code 5. Distrust: completeness of findings. Safe fallback: report scan failure.
- Partial consumer-group read: scan completes but `reliability.consumer_groups_complete` is `false` and delete recommendations are suppressed. Distrust: every unused-topic finding. Safe fallback: re-run once the cluster is fully readable; do not act on the findings.

## Parsing examples

```bash
kafkaspectre audit --bootstrap-server kafka:9092 --output json | jq '.summary'
kafkaspectre audit --bootstrap-server kafka:9092 --output json | jq '.unused_topics[] | select(.risk == "high")'
kafkaspectre audit --bootstrap-server kafka:9092 --output json | jq -e '.reliability.consumer_groups_complete'
kafkaspectre check --repo ./app --bootstrap-server kafka:9092 --output json | jq '.findings[] | select(.status == "MISSING_IN_CLUSTER")'
```

---

This tool follows the [Agent-Native CLI Convention](https://ancc.dev). Validate with: `ancc validate .`

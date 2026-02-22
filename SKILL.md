---
name: kafkaspectre
description: Kafka cluster auditor — identifies unused, orphaned, and misconfigured topics
user-invocable: false
metadata: {"requires":{"bins":["kafkaspectre"]}}
---

# kafkaspectre — Kafka Cluster Auditor

You have access to `kafkaspectre`, a Kafka cluster auditor that identifies unused topics and code-vs-cluster drift.

## Install

```bash
brew install ppiankov/tap/kafkaspectre
```

Or download binary:

```bash
curl -LO https://github.com/ppiankov/kafkaspectre/releases/latest/download/kafkaspectre_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m).tar.gz
tar -xzf kafkaspectre_*.tar.gz
sudo mv kafkaspectre /usr/local/bin/
```

## Commands

| Command | What it does |
|---------|-------------|
| `kafkaspectre audit --bootstrap-server <addr>` | Audit cluster for unused topics |
| `kafkaspectre audit --output json` | JSON output for parsing |
| `kafkaspectre check --repo <path> --bootstrap-server <addr>` | Compare code references vs cluster |
| `kafkaspectre check --output json` | JSON drift report |
| `kafkaspectre version` | Print version info |

## Key Flags

| Flag | Applies to | Description |
|------|-----------|-------------|
| `--bootstrap-server` | audit, check | Kafka broker address (required) |
| `--auth-mechanism` | audit, check | SASL mechanism (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512) |
| `--username`, `--password` | audit, check | SASL credentials |
| `--tls` | audit, check | Enable TLS |
| `--tls-cert`, `--tls-key`, `--tls-ca` | audit, check | TLS certificate files |
| `--output` | audit, check | Format: json, text, sarif (default: text) |
| `--exclude-internal` | audit, check | Skip internal Kafka topics |
| `--exclude-topics` | audit, check | Exclude by glob pattern (repeatable) |
| `--timeout` | audit, check | Query timeout (default: 10s) |
| `--repo` | check | Repository path (required for check) |

## Exit Codes

| Code | Meaning | Agent action |
|------|---------|--------------|
| `0` | Success (no findings) | Continue |
| `1` | Internal error | Fail job |
| `2` | Invalid arguments | Fix command and retry |
| `3` | Not found (cluster/repo) | Check connectivity |
| `5` | Network error | Retry with backoff |
| `6` | Findings detected | Parse JSON and report |

## JSON Output Structure

### audit --output json

```json
{
  "tool": "kafkaspectre",
  "version": "0.2.0",
  "timestamp": "2026-02-22T10:30:00Z",
  "summary": {
    "cluster_name": "kafka.example.com",
    "total_brokers": 3,
    "total_topics_including_internal": 55,
    "total_topics_analyzed": 50,
    "unused_topics": 8,
    "active_topics": 42,
    "internal_topics_excluded": 5,
    "unused_percentage": 16.0,
    "total_partitions": 150,
    "unused_partitions": 24,
    "active_partitions": 126,
    "unused_partitions_percentage": 16.0,
    "total_consumer_groups": 15,
    "high_risk_count": 2,
    "medium_risk_count": 3,
    "low_risk_count": 3,
    "recommended_cleanup_topics": ["topic-a", "topic-b"],
    "cluster_health_score": "good",
    "potential_savings_info": "8 unused topics representing 24 partitions (16.0% of total)"
  },
  "unused_topics": [
    {
      "name": "deprecated-events",
      "partitions": 8,
      "replication_factor": 3,
      "retention_ms": "604800000",
      "retention_human": "7 days",
      "cleanup_policy": "delete",
      "min_insync_replicas": "1",
      "interesting_config": {},
      "reason": "No consumer groups found",
      "recommendation": "Investigate before deletion",
      "risk": "high",
      "cleanup_priority": 3
    }
  ],
  "active_topics": [
    {
      "name": "user-events",
      "partitions": 12,
      "replication_factor": 3,
      "consumer_groups": ["analytics-service", "email-service"],
      "consumer_count": 2
    }
  ],
  "cluster_metadata": {
    "brokers": [{"id": 1, "host": "kafka-1", "port": 9092}],
    "consumer_groups_count": 15,
    "fetched_at": "2026-02-22 10:30:00 UTC"
  }
}
```

### check --output json

```json
{
  "tool": "kafkaspectre",
  "version": "0.2.0",
  "timestamp": "2026-02-22T10:30:00Z",
  "summary": {
    "repo_path": "/path/to/repo",
    "files_scanned": 234,
    "repo_topics": 15,
    "cluster_topics": 47,
    "total_findings": 62,
    "ok_count": 10,
    "missing_in_cluster_count": 5,
    "unreferenced_in_repo_count": 32,
    "unused_count": 15
  },
  "findings": [
    {
      "topic": "user-events",
      "status": "OK",
      "referenced_in_repo": true,
      "in_cluster": true,
      "consumer_groups": ["user-service"],
      "references": [
        {"file": "src/events.go", "line": 45, "source": "TopicName: \"user-events\""}
      ],
      "reason": "topic exists in cluster and has active consumers"
    }
  ]
}
```

## Agent Usage Patterns

### Audit workflow

```bash
kafkaspectre audit --bootstrap-server kafka:9092 --output json > audit.json
if [ $? -eq 6 ]; then
  # Findings detected — parse JSON and report
  jq '.unused_topics[] | select(.risk == "high")' audit.json
fi
```

### Drift detection

```bash
kafkaspectre check --repo ./my-app --bootstrap-server kafka:9092 --output json > check.json
if [ $? -eq 6 ]; then
  # Mismatches detected
  jq '.findings[] | select(.status == "MISSING_IN_CLUSTER")' check.json
fi
```

### Config file

Create `~/.kafkaspectre.yaml` to avoid repeating flags:

```yaml
bootstrap_servers: "kafka:9092"
auth_mechanism: "SCRAM-SHA-256"
username: "reader"
password: "secret"
exclude_internal: true
exclude_topics:
  - "_confluent-*"
  - "__consumer_offsets"
timeout: 30s
format: json
```

Then run without flags:

```bash
kafkaspectre audit  # uses config defaults
```

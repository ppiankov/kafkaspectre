## Topic Cleanup Guide

After running KafkaSpectre audit, use this guide to safely clean up unused topics.

### ⚠️ Safety First

**Before deleting ANY topic:**

1. **Verify with stakeholders**: Confirm with application owners that topics are truly unused
2. **Check external consumers**: Some consumers may not be visible in your cluster (cross-cluster replication, external systems)
3. **Review retention settings**: Topics with long retention may be intentionally idle
4. **Test in non-production first**: Always validate cleanup procedures in dev/staging environments
5. **Backup topic data**: Consider exporting topic data before deletion (if recovery might be needed)

### Cleanup Workflow

#### Step 1: Generate Audit Report

```bash
# Generate JSON report
./bin/kafkaspectre audit \
  --bootstrap-server kafka:9092 \
  --auth-mechanism SCRAM-SHA-256 \
  --username admin \
  --password "$KAFKA_PASSWORD" \
  --output json > audit-report.json

# Review summary statistics
jq '.summary' audit-report.json
```

#### Step 2: Filter by Risk Level

Extract low-risk topics recommended for cleanup:

```bash
# Get low-risk topics only
jq -r '.unused_topics[] | select(.risk == "low") | .name' audit-report.json > low-risk-topics.txt

# Review the list
cat low-risk-topics.txt
```

#### Step 3: Create Deletion Script

Generate a safe deletion script with confirmation prompts:

```bash
#!/bin/bash
# cleanup-kafka-topics.sh

KAFKA_BOOTSTRAP="kafka:9092"
TOPICS_FILE="low-risk-topics.txt"

echo "=== Kafka Topic Cleanup Script ==="
echo "Cluster: $KAFKA_BOOTSTRAP"
echo "Topics to delete: $(wc -l < $TOPICS_FILE)"
echo ""
echo "WARNING: This will permanently delete topics!"
read -p "Are you sure you want to proceed? (type 'yes' to continue): " CONFIRM

if [ "$CONFIRM" != "yes" ]; then
    echo "Aborted."
    exit 1
fi

while IFS= read -r topic; do
    echo "Deleting topic: $topic"
    kafka-topics.sh --bootstrap-server $KAFKA_BOOTSTRAP \
        --command-config /path/to/admin.properties \
        --delete --topic "$topic"

    # Add delay to avoid overwhelming the cluster
    sleep 1
done < "$TOPICS_FILE"

echo "Cleanup complete!"
```

#### Step 4: Using Kafka Admin Tools

**Option 1: kafka-topics.sh (Kafka CLI)**

```bash
# Delete a single topic
kafka-topics.sh --bootstrap-server kafka:9092 \
  --command-config admin.properties \
  --delete --topic deprecated-topic-name

# Verify deletion
kafka-topics.sh --bootstrap-server kafka:9092 \
  --command-config admin.properties \
  --list | grep deprecated-topic-name
```

**Option 2: kafkacat/kcat**

```bash
# List topics (verify before deletion)
kcat -b kafka:9092 -L | grep "topic"

# Note: kcat cannot delete topics, use kafka-topics.sh
```

**Option 3: Terraform (Infrastructure as Code)**

```hcl
# Remove topic resources from your Terraform config
# Then apply changes
terraform plan
terraform apply
```

**Option 4: AWS MSK (AWS Console/CLI)**

```bash
# Using AWS CLI for MSK
aws kafka delete-topic \
  --cluster-arn arn:aws:kafka:region:account:cluster/name/uuid \
  --topic-name deprecated-topic-name
```

#### Step 5: Verify Cleanup

After deletion, run KafkaSpectre again to verify:

```bash
./bin/kafkaspectre audit \
  --bootstrap-server kafka:9092 \
  --output json > audit-after-cleanup.json

# Compare before and after
echo "Before: $(jq '.summary.unused_topics' audit-report.json)"
echo "After:  $(jq '.summary.unused_topics' audit-after-cleanup.json)"
```

### Understanding Risk Levels

**IMPORTANT**: All unused topics have **ZERO consumer groups**. Risk level indicates the **resource footprint** and **potential impact** if deleted, NOT whether they have consumers.

#### ⚠️ Warning: Invisible Consumers

Topics may appear "unused" but are actually consumed manually without consumer groups:

**Common invisible consumers:**
- Analytics teams using `kafkacat`, `kafka-console-consumer`, or `kcat` for manual exports
- Ad-hoc data exports to Excel/CSV for analysis
- One-off debugging or data inspection
- Scripts that read data without committing offsets

**Why this is a problem:**
- ❌ No offset tracking - can't resume from where they left off
- ❌ Invisible to monitoring tools like KafkaSpectre
- ❌ Topics appear unused when they're actively consumed
- ❌ Risk of deleting data that's actually needed

**High-risk topics (10+ partitions, 3+ replicas) are especially suspicious** - someone invested significant resources for a reason!

**Best practices to fix invisible consumption:**

1. **Kafka Connect to Data Lake** (Recommended for Analytics):
   ```bash
   # Stream to S3/Parquet for Athena/Snowflake queries
   kafka-connect → S3 → Athena/BigQuery
   ```

2. **Dedicated Consumer Service**:
   ```bash
   # Create a proper consumer with a group ID
   kafka-consumer --group-id analytics-team-export --topic user-events
   ```

3. **Use Consumer Groups Even for Manual Tools**:
   ```bash
   # kafkacat with consumer group (now visible!)
   kafkacat -b kafka:9092 -t user-events \
     -G analytics-manual-export-group
   ```

**Before deleting high-risk topics:**
1. Check with analytics/data teams
2. Search internal docs for topic references
3. Review topic names for "analytics", "export", "report" keywords
4. Consider retention period - long retention suggests intentional archival

#### How Risk is Calculated

KafkaSpectre classifies unused topics based on partition count and replication factor:

```go
// From cmd/kafkaspectre/main.go
if topic.Partitions <= 1 && topic.ReplicationFactor <= 1 {
    return "low"      // 1 partition AND 1 replica
}
if topic.Partitions >= 10 || topic.ReplicationFactor >= 3 {
    return "high"     // 10+ partitions OR 3+ replicas
}
return "medium"       // Everything else
```

#### 🟢 Low Risk (Safe to Delete)

**Criteria**: `1 partition` AND `1 replication factor` AND `0 consumers`

**Why safe**: Minimal resource footprint, likely test/dev topics

**Extract and delete:**
```bash
# Extract low-risk topics
jq -r '.unused_topics[] | select(.risk == "low") | .name' \
  audit-report.json > low-risk-topics.txt

# Count them
wc -l low-risk-topics.txt

# Delete with kafkactl
while IFS= read -r topic; do
    echo "Deleting: $topic"
    kafkactl --context your-context delete topic "$topic"
    sleep 0.5
done < low-risk-topics.txt

# Or with kafka-topics.sh
while IFS= read -r topic; do
    kafka-topics.sh --bootstrap-server kafka:9092 \
        --command-config admin.properties \
        --delete --topic "$topic"
done < low-risk-topics.txt
```

**Examples**: `test-topic-123`, `dev-experiment-old`, `staging-temp-data`

#### 🟡 Medium Risk (Review Before Deleting)

**Criteria**: `2-9 partitions` OR `2 replicas` AND `0 consumers`

**Why cautious**: More resources invested, might be used for batch processing or periodic jobs

**Extract and review:**
```bash
# Extract medium-risk topics with details
jq -r '.unused_topics[] | select(.risk == "medium") |
  "\(.name) - \(.partitions)p/\(.replication_factor)r - \(.retention_human)"' \
  audit-report.json > medium-risk-topics.txt

# Review the list
less medium-risk-topics.txt

# Extract just names for deletion
jq -r '.unused_topics[] | select(.risk == "medium") | .name' \
  audit-report.json > medium-risk-names.txt

# Delete after review
while IFS= read -r topic; do
    kafkactl --context your-context delete topic "$topic"
done < medium-risk-names.txt
```

**Examples**: `staging-user-events` (3p/2r), `batch-analytics-v2` (5p/1r)

#### 🔴 High Risk (Investigate First!)

**Criteria**: `10+ partitions` OR `3+ replicas` AND `0 consumers`

**Why dangerous**: Significant resource investment, may be:
- Over-provisioned production topics
- Seasonal/periodic usage (Black Friday, year-end reports)
- **Invisible consumers** (analytics teams using kafkacat/console-consumer without consumer groups)
- External consumers not visible in cluster metadata
- Disaster recovery or future capacity planning

**⚠️ CRITICAL: Check for invisible analytics usage!**

High-risk topics with many partitions are often consumed manually by data/analytics teams without proper consumer groups. **Always verify before deletion:**

```bash
# Look for analytics-related topic names
jq -r '.unused_topics[] | select(.risk == "high") | .name' \
  audit-report.json | grep -iE '(analytics|export|report|data|warehouse|bi|dashboard)'

# Check topics with long retention (suggests archival/analysis use)
jq -r '.unused_topics[] | select(.risk == "high" and .retention_human != "") |
  "\(.name) - Retention: \(.retention_human)"' \
  audit-report.json
```

**Investigate before deleting:**
```bash
# Get high-risk topics with full details
jq -r '.unused_topics[] | select(.risk == "high") |
  "\(.name)|\(.partitions)|\(.replication_factor)|\(.retention_human)|\(.recommendation)"' \
  audit-report.json | column -t -s'|' > high-risk-analysis.txt

# Sample 10 random high-risk topics to investigate
jq -r '.unused_topics[] | select(.risk == "high") | .name' \
  audit-report.json | shuf | head -10 > investigate-these.txt

# Check with stakeholders before deleting
cat investigate-these.txt
```

**Action**: Post to team chat: *"These topics have 10+ partitions but zero consumers. Does anyone know why they exist?"*

**Examples**: `production-orders-archive` (50p/3r), `ml-training-data-2023` (20p/3r)

### Identifying Invisible Consumers (Real-World Example)

If analytics teams tell you *"we use some of these topics manually"*, here's how to identify which ones:

```bash
# 1. Find high-risk topics with analytics-related names
jq -r '.unused_topics[] | select(.risk == "high") | .name' \
  audit-report.json | \
  grep -iE '(analytics|export|report|data|warehouse|bi|dashboard|etl|dwh)' \
  > potentially-analytics.txt

# 2. Check for topics with very long retention (archival use case)
jq -r '.unused_topics[] |
  select(.risk == "high" and .retention_ms != "" and .retention_ms != "-1") |
  (.retention_ms | tonumber) as $ms |
  select($ms > 604800000) |  # More than 7 days
  "\(.name) - \(.retention_human)"' \
  audit-report.json

# 3. Share with analytics team for confirmation
cat potentially-analytics.txt | \
  while read topic; do
    echo "- $topic"
  done > send-to-analytics-team.txt

# 4. For confirmed analytics topics, set up proper consumer groups
# Use Kafka Connect or create a service with consumer group ID
```

**Fix invisible consumption:**
```bash
# Quick fix: Use kafkacat with consumer group
kafkacat -b kafka:9092 -t user-events \
  -G analytics-manual-export-group \
  -o end | tee export.json

# Better: Set up Kafka Connect S3 Sink
curl -X POST http://kafka-connect:8083/connectors -H "Content-Type: application/json" -d '{
  "name": "s3-analytics-sink",
  "config": {
    "connector.class": "io.confluent.connect.s3.S3SinkConnector",
    "topics": "user-events,orders,analytics-data",
    "s3.bucket.name": "analytics-data-lake",
    "format.class": "io.confluent.connect.s3.format.parquet.ParquetFormat"
  }
}'

# Now these topics will show as "active" in future audits!
```

### Complete Deletion Workflow by Risk Level

```bash
# 1. Generate audit report
./bin/kafkaspectre audit \
  --bootstrap-server kafka:9092 \
  --tls \
  --output json > audit-report.json

# 2. Check risk distribution
jq '.summary | {
  low: .low_risk_count,
  medium: .medium_risk_count,
  high: .high_risk_count,
  total_unused: .unused_topics
}' audit-report.json

# 3. Start with low-risk (safest)
jq -r '.unused_topics[] | select(.risk == "low") | .name' \
  audit-report.json | \
  while read topic; do
    kafkactl delete topic "$topic"
  done

# 4. Then medium-risk (after review)
jq -r '.unused_topics[] | select(.risk == "medium") | .name' \
  audit-report.json > medium-to-review.txt
# Review file, then delete

# 5. High-risk (CHECK FOR INVISIBLE CONSUMERS FIRST!)
# Search for analytics-related names
jq -r '.unused_topics[] | select(.risk == "high") | .name' \
  audit-report.json | \
  grep -iE '(analytics|export|report|data)' > check-with-analytics.txt

# Investigate remaining high-risk topics
jq -r '.unused_topics[] | select(.risk == "high") |
  [.name, .partitions, .replication_factor] | @tsv' \
  audit-report.json > high-risk-investigate.tsv
# Manual review required - contact topic owners
```

### kafkactl Configuration

**For AWS MSK with TLS:**

Create `~/.config/kafkactl/config.yml`:

```yaml
contexts:
    production:
        brokers:
            - b-1.kafka-prod.example.com:9094
            - b-2.kafka-prod.example.com:9094
            - b-3.kafka-prod.example.com:9094
        tls:
            enabled: true
        sasl:
            enabled: true
            mechanism: scram-sha512
            username: admin
            password: ${KAFKA_PASSWORD}
```

**Delete topics:**

```bash
# Single topic
kafkactl --context production delete topic deprecated-topic-name

# Batch delete using xargs (fast)
cat topics-to-delete.txt | \
  xargs -n1 -I{} kafkactl --context production delete topic {}

# Or directly from file:
xargs -n1 -I{} kafkactl --context production delete topic {} \
  < topics-to-delete.txt

# Parallel deletion (5 at a time) - fastest
cat topics-to-delete.txt | \
  xargs -n1 -P5 -I{} kafkactl --context production delete topic {}

# Traditional while loop (slower but readable)
cat topics-to-delete.txt | while read topic; do
    kafkactl --context production delete topic "$topic"
    sleep 0.5  # Avoid overwhelming the cluster
done

# With confirmation prompts (safest)
while IFS= read -r topic; do
    echo -n "Delete $topic? [y/N] "
    read -r confirm
    if [[ "$confirm" == "y" ]]; then
        kafkactl --context production delete topic "$topic"
    fi
done < topics-to-delete.txt
```

### Alternative: Reduce Replication Factor Instead of Deletion

**When you can't delete a topic but need to reduce resource usage**, reduce the replication factor instead:

#### Why Reduce Replication?

- ✅ Keep data available (don't delete)
- ✅ Reduce storage costs (fewer replicas)
- ✅ Reduce replication overhead on brokers
- ✅ Safer than deletion (data remains accessible)
- ⚠️ Lower fault tolerance (trade-off)

#### Use Cases

1. **High-risk topics with 3 replicas** → Reduce to 1 or 2
2. **Dev/staging topics** → Don't need production-level redundancy
3. **Archive topics** → Rarely accessed, don't need 3 replicas
4. **Compliance topics** → Must keep data but don't need high availability

#### How to Reduce Replication Factor

```bash
# 1. Preview changes (dry-run)
kafkactl --context production alter topic my-topic \
  --replication-factor 1 \
  --validate-only

# Output shows what will change:
# PARTITION  OLDEST_OFFSET  NEWEST_OFFSET  LEADER  REPLICAS  IN_SYNC_REPLICAS
# 0          1              1000           1       1         1,2,3

# 2. Apply changes
kafkactl --context production alter topic my-topic \
  --replication-factor 1

# Output:
# partition replicas have been reassigned

# 3. Verify the change
kafkactl --context production describe topic my-topic
```

#### Batch Reduce Replication for High-Risk Topics

**Method 1: Using xargs (Fast and Clean)**

```bash
# 1. Extract topics with RF=3
jq -r '.unused_topics[] |
  select(.replication_factor == 3 and .risk == "high") |
  .name' audit-report.json > rf3-topics.txt

# 2. Preview changes for all topics (dry-run)
cat rf3-topics.txt | \
  xargs -n1 -I{} kafkactl --context production alter topic {} \
    --replication-factor 1 --validate-only

# 3. Apply changes to all topics
cat rf3-topics.txt | \
  xargs -n1 -I{} kafkactl --context production alter topic {} \
    --replication-factor 1

# Or directly from file:
xargs -n1 -I{} kafkactl --context production alter topic {} \
  --replication-factor 1 < rf3-topics.txt
```

**Method 2: Using xargs with parallel processing**

```bash
# Process 5 topics at a time (faster for large batches)
cat rf3-topics.txt | \
  xargs -n1 -P5 -I{} kafkactl --context production alter topic {} \
    --replication-factor 1

# -P5 = run 5 operations in parallel
# Adjust based on cluster capacity
```

**Method 3: Interactive with confirmation (Safer)**

```bash
# Ask for confirmation for each topic
cat rf3-topics.txt | while read topic; do
    echo "Reducing replication for: $topic"

    # Preview first
    kafkactl alter topic "$topic" --replication-factor 1 --validate-only

    # Ask for confirmation
    echo -n "Apply changes? [y/N] "
    read confirm
    if [[ "$confirm" == "y" ]]; then
        kafkactl alter topic "$topic" --replication-factor 1
    fi

    sleep 1
done
```

**Performance comparison:**
- **xargs method**: ~1-2 seconds per topic
- **while loop**: ~2-3 seconds per topic (slower due to shell overhead)
- **parallel xargs (-P5)**: Process 5 topics simultaneously (fastest)
```

#### Calculate Savings

```bash
# Find topics with RF=3 that could be reduced to RF=1
jq -r '.unused_topics[] |
  select(.replication_factor == 3) |
  "\(.name)|\(.partitions)|\(.replication_factor)"' \
  audit-report.json | \
  awk -F'|' '{
    partitions=$2
    replicas=$3
    current_replicas_total = partitions * replicas
    new_replicas_total = partitions * 1
    savings = current_replicas_total - new_replicas_total
    print $1 " - Savings: " savings " partition-replicas"
  }' > replication-savings.txt

# Total savings
awk '{sum+=$NF} END {print "Total partition-replica savings: " sum}' replication-savings.txt
```

#### Real-World Example

Your cluster has 870 high-risk topics with likely high replication:

```bash
# Identify high-RF unused topics
jq -r '.unused_topics[] |
  select(.risk == "high" and .replication_factor >= 3) |
  "\(.name) - \(.partitions)p/\(.replication_factor)r"' \
  kafka-dev-report.json | head -20

# Example reduction (3→1 for a 10-partition topic):
# Before: 10 partitions × 3 replicas = 30 partition-replicas
# After:  10 partitions × 1 replica  = 10 partition-replicas
# Savings: 66% storage reduction per topic!
```

#### Recommended Strategy

1. **Low-risk topics**: Delete (safest)
2. **Medium-risk topics**: Review → Delete if confirmed unused
3. **High-risk topics**:
   - **Can't delete?** → Reduce RF from 3→1 (66% savings)
   - **Analytics topics?** → Set up Kafka Connect, then reduce RF
   - **Seasonal topics?** → Keep at RF=1 until next season

#### Caveats

⚠️ **Reduced fault tolerance**: RF=1 means:
- Topic unavailable if broker goes down
- No redundancy (single point of failure)
- Acceptable for dev/staging, archives, or reconstructible data

✅ **Good for**: Dev/test, archives, analytics exports, reconstructible data
❌ **Not for**: Production critical topics, compliance data (until backed up elsewhere)

### Rollback Plan

If you accidentally delete an important topic:

1. **Recreate the topic** with the same configuration:
   ```bash
   kafka-topics.sh --bootstrap-server kafka:9092 \
     --create --topic recovered-topic \
     --partitions 3 \
     --replication-factor 2 \
     --config retention.ms=604800000
   ```

2. **Restore data from backup** (if available):
   - From S3/cloud storage backups
   - From tiered storage (if enabled)
   - From disaster recovery cluster

3. **Check consumer group offsets**:
   ```bash
   kafka-consumer-groups.sh --bootstrap-server kafka:9092 \
     --describe --group consumer-group-name
   ```

### Automation Example (CI/CD Integration)

Add KafkaSpectre to your cleanup pipeline:

```yaml
# .github/workflows/kafka-cleanup.yml
name: Kafka Cluster Cleanup

on:
  schedule:
    - cron: '0 0 * * 0'  # Weekly on Sunday

jobs:
  audit:
    runs-on: ubuntu-latest
    steps:
      - name: Download KafkaSpectre
        run: |
          curl -L https://github.com/ppiankov/kafkaspectre/releases/latest/download/kafkaspectre-linux-amd64 -o kafkaspectre
          chmod +x kafkaspectre

      - name: Run Audit
        run: |
          ./kafkaspectre audit \
            --bootstrap-server ${{ vars.KAFKA_BOOTSTRAP }} \
            --auth-mechanism SCRAM-SHA-256 \
            --username ${{ vars.KAFKA_USERNAME }} \
            --password "${{ secrets.KAFKA_PASSWORD }}" \
            --output json > audit-report.json

      - name: Create Cleanup PR
        if: ${{ success() }}
        run: |
          # Extract low-risk topics
          jq -r '.unused_topics[] | select(.risk == "low") | .name' audit-report.json > cleanup-list.txt

          # Create PR with cleanup recommendations
          gh pr create \
            --title "Kafka Cleanup: $(wc -l < cleanup-list.txt) unused topics" \
            --body-file audit-report.json
```

### Best Practices

1. **Schedule regular audits**: Run KafkaSpectre weekly or monthly
2. **Set topic naming conventions**: Use prefixes to identify ownership (e.g., `team-service-topic`)
3. **Implement topic lifecycle policies**: Define TTL for test/development topics
4. **Track topic metadata**: Use tags or comments to document topic purpose
5. **Monitor after cleanup**: Watch for errors or alerts after deletion
6. **Document decisions**: Keep a log of deleted topics and reasons

### Cleanup Metrics to Track

After cleanup, measure the impact:

- **Partition reduction**: Track `unused_partitions` before and after
- **Storage savings**: Monitor disk usage on brokers
- **Performance improvement**: Check broker CPU/memory after reducing partition count
- **Cluster health score**: Track improvement from "critical" to "good"


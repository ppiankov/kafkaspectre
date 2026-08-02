package reporter

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

// TestEmitDocEnvelope is a documentation aid: with KS_EMIT_ENVELOPE=1 it prints
// the real audit JSON envelope so docs can be derived from the code, not memory.
func TestEmitDocEnvelope(t *testing.T) {
	if os.Getenv("KS_EMIT_ENVELOPE") != "1" {
		t.Skip("set KS_EMIT_ENVELOPE=1 to print the envelope")
	}

	res := &AuditResult{
		Tool: "kafkaspectre", Version: "0.1.0", Timestamp: "2026-08-02T00:00:00Z",
		Summary: &AuditSummary{ClusterName: "broker-1", TotalBrokers: 1, TotalTopics: 1, UnusedTopics: 1, RecommendedCleanup: []string{"orders"}, ClusterHealthScore: "critical"},
		UnusedTopics: []*UnusedTopic{{
			Name: "orders", Partitions: 1, ReplicationFactor: 1,
			Reason: "No consumer groups found", Recommendation: "Safe to delete after confirmation",
			Risk: "low", CleanupPriority: 1, InterestingConfig: map[string]string{},
		}},
		Metadata:    &kafka.ClusterMetadata{Brokers: []kafka.BrokerInfo{{ID: 1, Host: "broker-1", Port: 9092}}, FetchedAt: time.Unix(0, 0).UTC()},
		Reliability: ScanReliability{ConsumerGroupsComplete: true},
	}

	var buf bytes.Buffer
	if err := NewAuditJSONReporter(&buf, true).GenerateAudit(context.Background(), res); err != nil {
		t.Fatalf("GenerateAudit: %v", err)
	}
	t.Log("\n" + buf.String())
}

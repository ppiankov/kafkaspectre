package reporter

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

// WO-27: degraded result fixture
func degradedResult() *AuditResult {
	return &AuditResult{
		Tool: "kafkaspectre", Version: "test", Timestamp: "2026-08-02T00:00:00Z",
		Summary: &AuditSummary{ClusterName: "b1", TotalBrokers: 1, TotalTopics: 1, UnusedTopics: 1},
		UnusedTopics: []*UnusedTopic{{
			Name: "orders", Partitions: 1, ReplicationFactor: 1,
			Reason: "Consumer group data could not be read; unused status is UNVERIFIED",
			Risk:   "low", CleanupPriority: 1, InterestingConfig: map[string]string{},
		}},
		Metadata: &kafka.ClusterMetadata{
			Brokers:   []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
			FetchedAt: time.Unix(0, 0).UTC(),
		},
		Reliability: ScanReliability{
			ConsumerGroupsComplete: false,
			ReadErrors:             []string{"describe consumer groups: broker unreachable"},
		},
	}
}

// WO-27: render JSON helper
func renderAuditJSON(t *testing.T, result *AuditResult) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := NewAuditJSONReporter(&buf, false).GenerateAudit(context.Background(), result); err != nil {
		t.Fatalf("GenerateAudit: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal audit JSON: %v", err)
	}
	return decoded
}

// TestAuditJSONEnvelopeContract pins the top-level key names of the audit JSON
// envelope.
//
// These names are a published contract: docs/SKILL.md documents them and its
// parsing examples pipe them through jq. Renaming `unused_topics` used to pass
// the entire suite while breaking every documented consumer.
// WO-25: JSON envelope key names
func TestAuditJSONEnvelopeContract(t *testing.T) {
	decoded := renderAuditJSON(t, degradedResult())

	for _, key := range []string{
		"tool", "version", "timestamp",
		"summary", "unused_topics", "cluster_metadata", "reliability",
	} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("audit JSON envelope is missing documented key %q", key)
		}
	}

	summary, ok := decoded["summary"].(map[string]any)
	if !ok {
		t.Fatal("summary is not an object")
	}
	for _, key := range []string{
		"unused_topics", "active_topics", "total_partitions",
		"high_risk_count", "medium_risk_count", "low_risk_count",
		"recommended_cleanup_topics", "cluster_health_score",
	} {
		if _, ok := summary[key]; !ok {
			t.Errorf("summary is missing documented key %q", key)
		}
	}

	topics, ok := decoded["unused_topics"].([]any)
	if !ok || len(topics) == 0 {
		t.Fatal("unused_topics is not a non-empty array")
	}
	first, ok := topics[0].(map[string]any)
	if !ok {
		t.Fatal("unused_topics[0] is not an object")
	}
	for _, key := range []string{"name", "partitions", "replication_factor", "reason", "risk", "cleanup_priority"} {
		if _, ok := first[key]; !ok {
			t.Errorf("unused_topics[] is missing documented key %q", key)
		}
	}
}

// WO-27: the reliability marker is what downstream consumers gate on —
// docs/SKILL.md tells agents to run `jq -e '.reliability.consumer_groups_complete'`.
// Dropping it from the envelope used to pass every test.
// WO-27: reliability in JSON
func TestAuditJSONCarriesReliability(t *testing.T) {
	decoded := renderAuditJSON(t, degradedResult())

	reliability, ok := decoded["reliability"].(map[string]any)
	if !ok {
		t.Fatal("reliability key missing or not an object")
	}
	complete, ok := reliability["consumer_groups_complete"].(bool)
	if !ok {
		t.Fatal("consumer_groups_complete missing or not a bool")
	}
	if complete {
		t.Fatal("degraded scan serialized consumer_groups_complete=true")
	}
	errs, ok := reliability["read_errors"].([]any)
	if !ok || len(errs) != 1 {
		t.Fatalf("read_errors = %v, want the recorded failure", reliability["read_errors"])
	}
}

// WO-27: the operator-facing warning must actually render.
// WO-27: degraded text banner
func TestAuditTextWarnsOnDegradedScan(t *testing.T) {
	var buf bytes.Buffer
	if err := NewAuditTextReporter(&buf, false).GenerateAudit(context.Background(), degradedResult()); err != nil {
		t.Fatalf("GenerateAudit: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "INCOMPLETE SCAN") {
		t.Error("degraded scan produced no INCOMPLETE SCAN banner")
	}
	if !strings.Contains(out, "broker unreachable") {
		t.Error("degraded scan did not name the read failure")
	}
}

// WO-27: a clean scan must NOT cry wolf.
// WO-27: clean scan silent
func TestAuditTextSilentOnCleanScan(t *testing.T) {
	result := degradedResult()
	result.Reliability = ScanReliability{ConsumerGroupsComplete: true}

	var buf bytes.Buffer
	if err := NewAuditTextReporter(&buf, false).GenerateAudit(context.Background(), result); err != nil {
		t.Fatalf("GenerateAudit: %v", err)
	}
	if strings.Contains(buf.String(), "INCOMPLETE SCAN") {
		t.Error("clean scan emitted an incomplete-scan warning")
	}
}

// TestEveryAuditReporterCarriesReliability is the fail-closed guard for the
// propagation gap: reliability reached the JSON and text reporters but was
// silently dropped by SARIF and SpectreHub — and SpectreHub is the documented
// aggregation target, so an aggregator had to regex English prose to tell a
// blind scan from an authoritative one.
// WO-27: all reporters carry reliability
func TestEveryAuditReporterCarriesReliability(t *testing.T) {
	result := degradedResult()

	renders := map[string]func() string{
		"json": func() string {
			var buf bytes.Buffer
			_ = NewAuditJSONReporter(&buf, false).GenerateAudit(context.Background(), result)
			return buf.String()
		},
		"text": func() string {
			var buf bytes.Buffer
			_ = NewAuditTextReporter(&buf, false).GenerateAudit(context.Background(), result)
			return buf.String()
		},
		"sarif": func() string {
			var buf bytes.Buffer
			_ = NewSARIFReporter(&buf, false).GenerateAudit(context.Background(), result)
			return buf.String()
		},
		"spectrehub": func() string {
			var buf bytes.Buffer
			_ = NewSpectreHubReporter(&buf, "kafka:9092").GenerateAudit(context.Background(), result)
			return buf.String()
		},
	}

	// Each format signals degradation in its own idiom; all four must signal it
	// STRUCTURALLY, not only inside a human-readable message string.
	wantMarker := map[string]string{
		"json":       `"consumer_groups_complete": false`,
		"text":       "INCOMPLETE SCAN",
		"sarif":      `"executionSuccessful": false`,
		"spectrehub": `"consumer_groups_complete": false`,
	}

	for format, render := range renders {
		out := render()
		marker := wantMarker[format]
		// Tolerate compact JSON encoding.
		compact := strings.ReplaceAll(marker, `": `, `":`)
		if !strings.Contains(out, marker) && !strings.Contains(out, compact) {
			t.Errorf("%s output does not signal a degraded scan (looking for %q)", format, marker)
		}
	}
}

// A clean scan must report success in every format.
// WO-27: clean scan all reporters
func TestEveryAuditReporterReportsCleanScan(t *testing.T) {
	result := degradedResult()
	result.Reliability = ScanReliability{ConsumerGroupsComplete: true}

	var sarifBuf, hubBuf bytes.Buffer
	_ = NewSARIFReporter(&sarifBuf, false).GenerateAudit(context.Background(), result)
	_ = NewSpectreHubReporter(&hubBuf, "kafka:9092").GenerateAudit(context.Background(), result)

	if strings.Contains(sarifBuf.String(), `"executionSuccessful":false`) ||
		strings.Contains(sarifBuf.String(), `"executionSuccessful": false`) {
		t.Error("clean scan reported executionSuccessful=false in SARIF")
	}
	if !strings.Contains(hubBuf.String(), `"consumer_groups_complete":true`) &&
		!strings.Contains(hubBuf.String(), `"consumer_groups_complete": true`) {
		t.Error("clean scan did not report completeness in the SpectreHub envelope")
	}
}

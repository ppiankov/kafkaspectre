package reporter

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestSpectreHubReporter_GenerateAudit(t *testing.T) {
	result := &AuditResult{
		Tool:      "kafkaspectre",
		Version:   "0.2.0",
		Timestamp: "2026-02-22T10:00:00Z",
		Summary: &AuditSummary{
			ClusterName:     "broker1",
			TotalTopics:     10,
			UnusedTopics:    3,
			HighRiskCount:   1,
			MediumRiskCount: 1,
			LowRiskCount:    1,
		},
		UnusedTopics: []*UnusedTopic{
			{Name: "old-events", Partitions: 12, ReplicationFactor: 3, RetentionHuman: "7 days", Risk: "high", Reason: "No consumer groups found", Recommendation: "Investigate before deletion"},
			{Name: "tmp-data", Partitions: 3, ReplicationFactor: 2, RetentionHuman: "1 days", Risk: "medium", Reason: "No consumer groups found", Recommendation: "Review before deletion"},
			{Name: "test-topic", Partitions: 1, ReplicationFactor: 1, RetentionHuman: "infinite", Risk: "low", Reason: "No consumer groups found", Recommendation: "Safe to delete after confirmation"},
		},
	}

	var buf bytes.Buffer
	r := NewSpectreHubReporter(&buf, "broker1:9092")
	if err := r.GenerateAudit(context.Background(), result); err != nil {
		t.Fatalf("GenerateAudit: %v", err)
	}

	var envelope SpectreHubEnvelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if envelope.Schema != "spectre/v1" {
		t.Errorf("schema = %q, want spectre/v1", envelope.Schema)
	}
	if envelope.Tool != "kafkaspectre" {
		t.Errorf("tool = %q, want kafkaspectre", envelope.Tool)
	}
	if envelope.Version != "0.2.0" {
		t.Errorf("version = %q, want 0.2.0", envelope.Version)
	}
	if envelope.Target.Type != "kafka" {
		t.Errorf("target.type = %q, want kafka", envelope.Target.Type)
	}
	if envelope.Target.Cluster != "broker1" {
		t.Errorf("target.cluster = %q, want broker1", envelope.Target.Cluster)
	}
	if len(envelope.Findings) != 3 {
		t.Fatalf("findings count = %d, want 3", len(envelope.Findings))
	}
	if envelope.Findings[0].ID != "UNUSED_TOPIC" {
		t.Errorf("findings[0].id = %q, want UNUSED_TOPIC", envelope.Findings[0].ID)
	}
	if envelope.Findings[0].Severity != "high" {
		t.Errorf("findings[0].severity = %q, want high", envelope.Findings[0].Severity)
	}
	if envelope.Findings[0].Location != "old-events" {
		t.Errorf("findings[0].location = %q, want old-events", envelope.Findings[0].Location)
	}
	if envelope.Summary.Total != 3 {
		t.Errorf("summary.total = %d, want 3", envelope.Summary.Total)
	}
	if envelope.Summary.High != 1 || envelope.Summary.Medium != 1 || envelope.Summary.Low != 1 {
		t.Errorf("summary severity = high=%d medium=%d low=%d, want 1/1/1",
			envelope.Summary.High, envelope.Summary.Medium, envelope.Summary.Low)
	}
}

func TestSpectreHubReporter_GenerateCheck(t *testing.T) {
	result := &CheckResult{
		Tool:      "kafkaspectre",
		Version:   "0.2.0",
		Timestamp: "2026-02-22T10:00:00Z",
		Summary: &CheckSummary{
			TotalFindings:           4,
			OKCount:                 1,
			MissingInClusterCount:   1,
			UnreferencedInRepoCount: 1,
			UnusedCount:             1,
		},
		Findings: []*CheckFinding{
			{Topic: "active-topic", Status: CheckStatusOK, Reason: "topic exists and has consumers"},
			{Topic: "missing-topic", Status: CheckStatusMissingInCluster, Reason: "referenced in code but not in cluster"},
			{Topic: "orphan-topic", Status: CheckStatusUnreferencedInRepo, Reason: "exists in cluster but not in code"},
			{Topic: "idle-topic", Status: CheckStatusUnused, Reason: "no active consumer groups"},
		},
	}

	var buf bytes.Buffer
	r := NewSpectreHubReporter(&buf, "broker1:9092")
	if err := r.GenerateCheck(context.Background(), result); err != nil {
		t.Fatalf("GenerateCheck: %v", err)
	}

	var envelope SpectreHubEnvelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if envelope.Schema != "spectre/v1" {
		t.Errorf("schema = %q, want spectre/v1", envelope.Schema)
	}
	// OK findings are excluded
	if len(envelope.Findings) != 3 {
		t.Fatalf("findings count = %d, want 3 (OK excluded)", len(envelope.Findings))
	}
	if envelope.Findings[0].ID != "MISSING_IN_CLUSTER" {
		t.Errorf("findings[0].id = %q, want MISSING_IN_CLUSTER", envelope.Findings[0].ID)
	}
	if envelope.Findings[0].Severity != "high" {
		t.Errorf("findings[0].severity = %q, want high", envelope.Findings[0].Severity)
	}
	if envelope.Summary.Total != 3 || envelope.Summary.High != 1 || envelope.Summary.Medium != 1 || envelope.Summary.Low != 1 {
		t.Errorf("summary = total=%d high=%d medium=%d low=%d, want 3/1/1/1",
			envelope.Summary.Total, envelope.Summary.High, envelope.Summary.Medium, envelope.Summary.Low)
	}
}

func TestSpectreHubReporter_EmptyFindings(t *testing.T) {
	result := &AuditResult{
		Tool:      "kafkaspectre",
		Version:   "0.2.0",
		Timestamp: "2026-02-22T10:00:00Z",
		Summary:   &AuditSummary{ClusterName: "test"},
	}

	var buf bytes.Buffer
	r := NewSpectreHubReporter(&buf, "broker1:9092")
	if err := r.GenerateAudit(context.Background(), result); err != nil {
		t.Fatalf("GenerateAudit: %v", err)
	}

	var envelope SpectreHubEnvelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if envelope.Findings == nil {
		t.Fatal("findings should be empty array, not null")
	}
	if len(envelope.Findings) != 0 {
		t.Errorf("findings count = %d, want 0", len(envelope.Findings))
	}
}

func TestHashBootstrap(t *testing.T) {
	h1 := HashBootstrap("broker1:9092")
	h2 := HashBootstrap("broker1:9092")
	if h1 != h2 {
		t.Errorf("same bootstrap should produce same hash: %s != %s", h1, h2)
	}

	h3 := HashBootstrap("broker2:9092")
	if h1 == h3 {
		t.Errorf("different bootstraps should produce different hashes")
	}

	if len(h1) < 10 || h1[:7] != "sha256:" {
		t.Errorf("hash should start with sha256:, got %q", h1)
	}
}

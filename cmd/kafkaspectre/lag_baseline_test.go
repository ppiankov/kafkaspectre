package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
	"github.com/ppiankov/kafkaspectre/internal/reporter"
)

// WO-47: a topic with consumers but high lag must be classified stale,
// not active. This is the core behavior the WO adds.
func TestStaleClassification(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"healthy-topic": topic("healthy-topic", 3, 1),
			"laggy-topic":   topic("laggy-topic", 1, 1),
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
			"healthy-cg": {
				GroupID: "healthy-cg", State: "Stable",
				Topics: []string{"healthy-topic"},
				Lag:    map[string]int64{"healthy-topic": 100},
			},
			"laggy-cg": {
				GroupID: "laggy-cg", State: "Stable",
				Topics: []string{"laggy-topic"},
				Lag:    map[string]int64{"laggy-topic": 50000},
			},
		},
	}

	result := buildAuditResultWithOptions(metadata, false, nil, false, defaultLagThreshold)

	// laggy-topic: 50000 lag >= 10000 threshold → stale
	if len(result.StaleTopics) != 1 {
		t.Fatalf("expected 1 stale topic, got %d", len(result.StaleTopics))
	}
	stale := result.StaleTopics[0]
	if stale.Name != "laggy-topic" {
		t.Fatalf("stale topic = %q, want laggy-topic", stale.Name)
	}
	if stale.TotalLag != 50000 {
		t.Fatalf("TotalLag = %d, want 50000", stale.TotalLag)
	}

	// healthy-topic: 100 lag < threshold → active
	if len(result.ActiveTopics) != 1 {
		t.Fatalf("expected 1 active topic, got %d", len(result.ActiveTopics))
	}
	if result.ActiveTopics[0].Name != "healthy-topic" {
		t.Fatalf("active topic = %q, want healthy-topic", result.ActiveTopics[0].Name)
	}

	// Summary should reflect the stale count
	if result.Summary.StaleTopics != 1 {
		t.Fatalf("summary.stale_topics = %d, want 1", result.Summary.StaleTopics)
	}
}

// WO-47: lag threshold 0 disables stale classification entirely.
func TestLagThresholdZeroDisablesStale(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"laggy-topic": topic("laggy-topic", 1, 1),
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
			"cg": {
				GroupID: "cg", State: "Stable",
				Topics: []string{"laggy-topic"},
				Lag:    map[string]int64{"laggy-topic": 999999},
			},
		},
	}

	result := buildAuditResultWithOptions(metadata, false, nil, false, 0)

	if len(result.StaleTopics) != 0 {
		t.Fatalf("expected 0 stale topics with threshold=0, got %d", len(result.StaleTopics))
	}
	if len(result.ActiveTopics) != 1 {
		t.Fatalf("expected 1 active topic (threshold disabled), got %d", len(result.ActiveTopics))
	}
}

// WO-48: snapshotFromResult produces one entry per topic.
func TestSnapshotFromResult(t *testing.T) {
	result := &reporter.AuditResult{
		ActiveTopics: []*reporter.ActiveTopic{{Name: "active-topic"}},
		UnusedTopics: []*reporter.UnusedTopic{{Name: "unused-topic"}},
		StaleTopics:  []*reporter.StaleTopic{{Name: "stale-topic", TotalLag: 42000}},
	}

	snap := snapshotFromResult(result)

	if len(snap.Topics) != 3 {
		t.Fatalf("snapshot has %d topics, want 3", len(snap.Topics))
	}

	statuses := map[string]string{}
	for _, ts := range snap.Topics {
		statuses[ts.Name] = ts.Status
	}
	if statuses["active-topic"] != "active" {
		t.Errorf("active-topic status = %q", statuses["active-topic"])
	}
	if statuses["unused-topic"] != "unused" {
		t.Errorf("unused-topic status = %q", statuses["unused-topic"])
	}
	if statuses["stale-topic"] != "stale" {
		t.Errorf("stale-topic status = %q", statuses["stale-topic"])
	}
}

// WO-48: computeDeltas identifies all four delta types.
func TestComputeDeltas(t *testing.T) {
	baseline := BaselineSnapshot{
		Version:   "1",
		Timestamp: "2026-01-01T00:00:00Z",
		Topics: []TopicSnapshot{
			{Name: "was-active-now-unused", Status: "active"},
			{Name: "was-unused-now-active", Status: "unused"},
			{Name: "was-active-now-stale", Status: "active"},
			{Name: "lag-increased", Status: "stale", Lag: 1000},
			{Name: "unchanged-active", Status: "active"},
		},
	}

	result := &reporter.AuditResult{
		ActiveTopics: []*reporter.ActiveTopic{
			{Name: "was-unused-now-active"},
			{Name: "unchanged-active"},
		},
		UnusedTopics: []*reporter.UnusedTopic{
			{Name: "was-active-now-unused"},
		},
		StaleTopics: []*reporter.StaleTopic{
			{Name: "was-active-now-stale", TotalLag: 50000},
			{Name: "lag-increased", TotalLag: 50000},
		},
	}

	deltas := computeDeltas(baseline, result)

	if len(deltas) != 4 {
		t.Fatalf("expected 4 deltas, got %d: %+v", len(deltas), deltas)
	}

	deltaTypes := map[string]string{}
	for _, d := range deltas {
		deltaTypes[d.Topic] = d.DeltaType
	}

	if deltaTypes["was-active-now-unused"] != "NEWLY_UNUSED" {
		t.Errorf("was-active-now-unused delta = %q, want NEWLY_UNUSED", deltaTypes["was-active-now-unused"])
	}
	if deltaTypes["was-unused-now-active"] != "NEWLY_ACTIVE" {
		t.Errorf("was-unused-now-active delta = %q, want NEWLY_ACTIVE", deltaTypes["was-unused-now-active"])
	}
	if deltaTypes["was-active-now-stale"] != "NEWLY_STALE" {
		t.Errorf("was-active-now-stale delta = %q, want NEWLY_STALE", deltaTypes["was-active-now-stale"])
	}
	if deltaTypes["lag-increased"] != "LAG_INCREASED" {
		t.Errorf("lag-increased delta = %q, want LAG_INCREASED", deltaTypes["lag-increased"])
	}

	// unchanged-active should NOT appear
	if _, found := deltaTypes["unchanged-active"]; found {
		t.Error("unchanged-active should not appear in deltas")
	}
}

// WO-48: baseline save/load round-trips correctly.
func TestBaselineRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")

	snap := BaselineSnapshot{
		Version:   "1",
		Timestamp: "2026-08-03T00:00:00Z",
		Topics: []TopicSnapshot{
			{Name: "topic-a", Status: "active"},
			{Name: "topic-b", Status: "unused"},
		},
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	loaded, err := loadBaseline(path)
	if err != nil {
		t.Fatalf("loadBaseline: %v", err)
	}
	if len(loaded.Topics) != 2 {
		t.Fatalf("loaded %d topics, want 2", len(loaded.Topics))
	}
	if loaded.Topics[0].Name != "topic-a" || loaded.Topics[0].Status != "active" {
		t.Errorf("first topic = %+v", loaded.Topics[0])
	}
}

// WO-48: loadBaseline errors on missing file.
func TestLoadBaselineMissingFile(t *testing.T) {
	_, err := loadBaseline("/nonexistent/baseline.json")
	if err == nil {
		t.Fatal("expected error loading missing file")
	}
}

// WO-48: no deltas when nothing changed.
func TestComputeDeltasNoChanges(t *testing.T) {
	baseline := BaselineSnapshot{
		Topics: []TopicSnapshot{
			{Name: "topic-a", Status: "active"},
		},
	}
	result := &reporter.AuditResult{
		ActiveTopics: []*reporter.ActiveTopic{{Name: "topic-a"}},
	}

	deltas := computeDeltas(baseline, result)
	if len(deltas) != 0 {
		t.Fatalf("expected 0 deltas, got %d", len(deltas))
	}
}

// WO-48/round-1: newly created topics (absent from baseline) must produce
// NEWLY_ACTIVE deltas. Previously they were silently skipped.
func TestComputeDeltasNewTopic(t *testing.T) {
	baseline := BaselineSnapshot{
		Topics: []TopicSnapshot{
			{Name: "old-topic", Status: "active"},
		},
	}
	result := &reporter.AuditResult{
		ActiveTopics: []*reporter.ActiveTopic{
			{Name: "old-topic"},
			{Name: "brand-new-topic"},
		},
	}

	deltas := computeDeltas(baseline, result)

	found := false
	for _, d := range deltas {
		if d.Topic == "brand-new-topic" && d.DeltaType == "NEWLY_ACTIVE" {
			found = true
			if d.From != "absent" {
				t.Errorf("From = %q, want absent", d.From)
			}
		}
	}
	if !found {
		t.Fatal("brand-new-topic not reported as NEWLY_ACTIVE")
	}
}

// WO-48/round-1: deleted topics (in baseline but not current) must produce
// DELETED deltas.
func TestComputeDeltasDeletedTopic(t *testing.T) {
	baseline := BaselineSnapshot{
		Topics: []TopicSnapshot{
			{Name: "deleted-topic", Status: "unused"},
			{Name: "still-here", Status: "active"},
		},
	}
	result := &reporter.AuditResult{
		ActiveTopics: []*reporter.ActiveTopic{{Name: "still-here"}},
	}

	deltas := computeDeltas(baseline, result)

	found := false
	for _, d := range deltas {
		if d.Topic == "deleted-topic" && d.DeltaType == "DELETED" {
			found = true
			if d.To != "absent" {
				t.Errorf("To = %q, want absent", d.To)
			}
		}
	}
	if !found {
		t.Fatal("deleted-topic not reported as DELETED")
	}
}

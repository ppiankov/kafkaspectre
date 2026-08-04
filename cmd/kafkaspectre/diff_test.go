package main

import (
	"testing"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

// WO-60: topics in A but not in B are listed factually.
func TestCompareTopicsOnlyInA(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{
		"shared": {Name: "shared", Partitions: 3, ReplicationFactor: 2},
		"only-a": {Name: "only-a", Partitions: 1, ReplicationFactor: 1},
	}
	topicsB := map[string]*kafka.TopicInfo{
		"shared": {Name: "shared", Partitions: 3, ReplicationFactor: 2},
	}

	result := compareTopics("a:9092", "b:9092", topicsA, topicsB, false)

	if len(result.TopicsPresentInANotB) != 1 {
		t.Fatalf("expected 1 topic only in A, got %d", len(result.TopicsPresentInANotB))
	}
	if result.TopicsPresentInANotB[0].Name != "only-a" {
		t.Errorf("only-in-A topic = %q, want only-a", result.TopicsPresentInANotB[0].Name)
	}
}

// WO-60: topics in B but not in A.
func TestCompareTopicsOnlyInB(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{
		"shared": {Name: "shared", Partitions: 3, ReplicationFactor: 2},
	}
	topicsB := map[string]*kafka.TopicInfo{
		"shared": {Name: "shared", Partitions: 3, ReplicationFactor: 2},
		"only-b": {Name: "only-b", Partitions: 6, ReplicationFactor: 3},
	}

	result := compareTopics("a:9092", "b:9092", topicsA, topicsB, false)

	if len(result.TopicsPresentInBNotA) != 1 {
		t.Fatalf("expected 1 topic only in B, got %d", len(result.TopicsPresentInBNotA))
	}
	if result.TopicsPresentInBNotA[0].Name != "only-b" {
		t.Errorf("only-in-B topic = %q, want only-b", result.TopicsPresentInBNotA[0].Name)
	}
}

// WO-60: config mismatch detected for topics in both.
func TestCompareTopicsConfigMismatch(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{
		"drift": {Name: "drift", Partitions: 3, ReplicationFactor: 2},
	}
	topicsB := map[string]*kafka.TopicInfo{
		"drift": {Name: "drift", Partitions: 6, ReplicationFactor: 3},
	}

	result := compareTopics("a:9092", "b:9092", topicsA, topicsB, false)

	if len(result.ConfigMismatches) != 1 {
		t.Fatalf("expected 1 config mismatch, got %d", len(result.ConfigMismatches))
	}
	m := result.ConfigMismatches[0]
	if m.PartitionsA != 3 || m.PartitionsB != 6 {
		t.Errorf("partitions A=%d B=%d, want 3 and 6", m.PartitionsA, m.PartitionsB)
	}
	if m.ReplicationFactorA != 2 || m.ReplicationFactorB != 3 {
		t.Errorf("RF A=%d B=%d, want 2 and 3", m.ReplicationFactorA, m.ReplicationFactorB)
	}
}

// WO-60: identical topics counted as in-both.
func TestCompareTopicsIdentical(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{
		"same": {Name: "same", Partitions: 3, ReplicationFactor: 2},
	}
	topicsB := map[string]*kafka.TopicInfo{
		"same": {Name: "same", Partitions: 3, ReplicationFactor: 2},
	}

	result := compareTopics("a:9092", "b:9092", topicsA, topicsB, false)

	if result.TopicsInBoth != 1 {
		t.Fatalf("topics_in_both = %d, want 1", result.TopicsInBoth)
	}
	if len(result.ConfigMismatches) != 0 {
		t.Errorf("expected 0 mismatches, got %d", len(result.ConfigMismatches))
	}
}

// WO-60: internal topics excluded by default.
func TestCompareTopicsExcludeInternal(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{
		"__consumer_offsets": {Name: "__consumer_offsets", Partitions: 50, ReplicationFactor: 3, Internal: true},
		"app-topic":          {Name: "app-topic", Partitions: 3, ReplicationFactor: 2},
	}
	topicsB := map[string]*kafka.TopicInfo{
		"app-topic": {Name: "app-topic", Partitions: 3, ReplicationFactor: 2},
	}

	result := compareTopics("a:9092", "b:9092", topicsA, topicsB, true)

	if len(result.TopicsPresentInANotB) != 0 {
		t.Fatalf("internal topics should be excluded, got %d in A-not-B", len(result.TopicsPresentInANotB))
	}
}

// WO-60: internal topics included when excludeInternal=false.
func TestCompareTopicsIncludeInternal(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{
		"__consumer_offsets": {Name: "__consumer_offsets", Partitions: 50, ReplicationFactor: 3, Internal: true},
	}
	topicsB := map[string]*kafka.TopicInfo{}

	result := compareTopics("a:9092", "b:9092", topicsA, topicsB, false)

	if len(result.TopicsPresentInANotB) != 1 {
		t.Fatalf("expected 1 internal topic in A-not-B, got %d", len(result.TopicsPresentInANotB))
	}
}

// WO-60: evidence note is always present.
func TestCompareTopicsHasEvidenceNote(t *testing.T) {
	result := compareTopics("a:9092", "b:9092", map[string]*kafka.TopicInfo{}, map[string]*kafka.TopicInfo{}, true)

	if result.Note == "" {
		t.Fatal("evidence note should not be empty")
	}
	if !contains(result.Note, "factual") {
		t.Error("note should mention 'factual observations'")
	}
}

// WO-60: results are sorted by name.
func TestCompareTopicsSorted(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{
		"zebra": {Name: "zebra", Partitions: 1, ReplicationFactor: 1},
		"apple": {Name: "apple", Partitions: 1, ReplicationFactor: 1},
		"mango": {Name: "mango", Partitions: 1, ReplicationFactor: 1},
	}
	topicsB := map[string]*kafka.TopicInfo{}

	result := compareTopics("a:9092", "b:9092", topicsA, topicsB, false)

	if len(result.TopicsPresentInANotB) != 3 {
		t.Fatalf("expected 3 topics, got %d", len(result.TopicsPresentInANotB))
	}
	if result.TopicsPresentInANotB[0].Name != "apple" {
		t.Errorf("first topic = %q, want apple (sorted)", result.TopicsPresentInANotB[0].Name)
	}
	if result.TopicsPresentInANotB[2].Name != "zebra" {
		t.Errorf("last topic = %q, want zebra (sorted)", result.TopicsPresentInANotB[2].Name)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

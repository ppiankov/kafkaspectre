package main

import (
	"context"
	"testing"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

// WO-60/killgate: topic only in A is observed with ObservedInA=true, ObservedInB=false.
// No directional bucket — the consumer derives absence.
func TestBuildTopicObservationsOnlyInA(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{
		"shared": {Name: "shared", Partitions: 3, ReplicationFactor: 2},
		"only-a": {Name: "only-a", Partitions: 1, ReplicationFactor: 1},
	}
	topicsB := map[string]*kafka.TopicInfo{
		"shared": {Name: "shared", Partitions: 3, ReplicationFactor: 2},
	}

	obs := buildTopicObservations(topicsA, topicsB, false)

	if len(obs) != 2 {
		t.Fatalf("expected 2 observations, got %d", len(obs))
	}

	byName := map[string]TopicObservation{}
	for _, o := range obs {
		byName[o.Name] = o
	}

	if !byName["only-a"].ObservedInA || byName["only-a"].ObservedInB {
		t.Errorf("only-a: ObservedInA=%v ObservedInB=%v, want true/false", byName["only-a"].ObservedInA, byName["only-a"].ObservedInB)
	}
	if !byName["shared"].ObservedInA || !byName["shared"].ObservedInB {
		t.Errorf("shared: ObservedInA=%v ObservedInB=%v, want true/true", byName["shared"].ObservedInA, byName["shared"].ObservedInB)
	}
}

// WO-60/killgate: topic only in B.
func TestBuildTopicObservationsOnlyInB(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{}
	topicsB := map[string]*kafka.TopicInfo{
		"only-b": {Name: "only-b", Partitions: 6, ReplicationFactor: 3},
	}

	obs := buildTopicObservations(topicsA, topicsB, false)

	if len(obs) != 1 {
		t.Fatalf("expected 1 observation, got %d", len(obs))
	}
	if obs[0].ObservedInA || !obs[0].ObservedInB {
		t.Errorf("only-b: ObservedInA=%v ObservedInB=%v, want false/true", obs[0].ObservedInA, obs[0].ObservedInB)
	}
	if obs[0].PartitionsB != 6 {
		t.Errorf("PartitionsB = %d, want 6", obs[0].PartitionsB)
	}
}

// WO-60/killgate: topic in both with different topology — per-cluster fields exposed, no "mismatch" label.
func TestBuildTopicObservationsDifferentTopology(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{
		"drift": {Name: "drift", Partitions: 3, ReplicationFactor: 2},
	}
	topicsB := map[string]*kafka.TopicInfo{
		"drift": {Name: "drift", Partitions: 6, ReplicationFactor: 3},
	}

	obs := buildTopicObservations(topicsA, topicsB, false)

	if len(obs) != 1 {
		t.Fatalf("expected 1 observation, got %d", len(obs))
	}
	o := obs[0]
	if o.PartitionsA != 3 || o.PartitionsB != 6 {
		t.Errorf("PartitionsA=%d PartitionsB=%d, want 3/6", o.PartitionsA, o.PartitionsB)
	}
	if o.ReplicationFactorA != 2 || o.ReplicationFactorB != 3 {
		t.Errorf("RFA=%d RFB=%d, want 2/3", o.ReplicationFactorA, o.ReplicationFactorB)
	}
}

// WO-60/killgate: topic in both with identical topology.
func TestBuildTopicObservationsIdentical(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{
		"same": {Name: "same", Partitions: 3, ReplicationFactor: 2},
	}
	topicsB := map[string]*kafka.TopicInfo{
		"same": {Name: "same", Partitions: 3, ReplicationFactor: 2},
	}

	obs := buildTopicObservations(topicsA, topicsB, false)

	if len(obs) != 1 {
		t.Fatalf("expected 1 observation, got %d", len(obs))
	}
	if obs[0].PartitionsA != obs[0].PartitionsB {
		t.Errorf("partitions differ: A=%d B=%d", obs[0].PartitionsA, obs[0].PartitionsB)
	}
}

// WO-60/killgate: internal topics excluded by default.
func TestBuildTopicObservationsExcludeInternal(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{
		"__consumer_offsets": {Name: "__consumer_offsets", Partitions: 50, ReplicationFactor: 3, Internal: true},
		"app-topic":          {Name: "app-topic", Partitions: 3, ReplicationFactor: 2},
	}
	topicsB := map[string]*kafka.TopicInfo{
		"app-topic": {Name: "app-topic", Partitions: 3, ReplicationFactor: 2},
	}

	obs := buildTopicObservations(topicsA, topicsB, true)

	for _, o := range obs {
		if o.Internal {
			t.Errorf("internal topic %q should be excluded", o.Name)
		}
	}
	if len(obs) != 1 {
		t.Fatalf("expected 1 non-internal observation, got %d", len(obs))
	}
}

// WO-60/killgate: internal topics included when excludeInternal=false.
func TestBuildTopicObservationsIncludeInternal(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{
		"__consumer_offsets": {Name: "__consumer_offsets", Partitions: 50, ReplicationFactor: 3, Internal: true},
	}
	topicsB := map[string]*kafka.TopicInfo{}

	obs := buildTopicObservations(topicsA, topicsB, false)

	if len(obs) != 1 {
		t.Fatalf("expected 1 observation including internal, got %d", len(obs))
	}
	if !obs[0].Internal {
		t.Error("internal flag should be true for __consumer_offsets")
	}
}

// WO-60/killgate: observations sorted by name.
func TestBuildTopicObservationsSorted(t *testing.T) {
	topicsA := map[string]*kafka.TopicInfo{
		"zebra": {Name: "zebra", Partitions: 1, ReplicationFactor: 1},
		"apple": {Name: "apple", Partitions: 1, ReplicationFactor: 1},
		"mango": {Name: "mango", Partitions: 1, ReplicationFactor: 1},
	}

	obs := buildTopicObservations(topicsA, map[string]*kafka.TopicInfo{}, false)

	if len(obs) != 3 {
		t.Fatalf("expected 3 observations, got %d", len(obs))
	}
	if obs[0].Name != "apple" {
		t.Errorf("first = %q, want apple", obs[0].Name)
	}
	if obs[2].Name != "zebra" {
		t.Errorf("last = %q, want zebra", obs[2].Name)
	}
}

// WO-60/killgate: Reliability records per-cluster read failures.
func TestRunDiffClusterFailureRecorded(t *testing.T) {
	result := runDiff(
		context.Background(),
		"unreachable:9999",
		"also-unreachable:9999",
		"", "",
		false, false, true,
	)

	if result.Reliability.ClusterAComplete {
		t.Error("cluster A should be marked incomplete on failure")
	}
	if result.Reliability.ClusterBComplete {
		t.Error("cluster B should be marked incomplete on failure")
	}
	if len(result.Reliability.ReadErrors) != 2 {
		t.Fatalf("expected 2 read errors, got %d", len(result.Reliability.ReadErrors))
	}
}

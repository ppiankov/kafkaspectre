package main

import (
	"strings"
	"testing"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
	"github.com/ppiankov/kafkaspectre/internal/reporter"
	"github.com/ppiankov/kafkaspectre/internal/scanner"
)

// assertCleanupListConsistent is the core invariant of this tool: nothing may be
// named for cleanup that the report itself says not to delete.
//
// WO-39: the summary list and the per-topic recommendation were decided
// independently and disagreed, so a single JSON document told automation to
// delete __consumer_offsets while telling a human not to.
func assertCleanupListConsistent(t *testing.T, result *reporter.AuditResult) {
	t.Helper()

	byName := make(map[string]*reporter.UnusedTopic, len(result.UnusedTopics))
	for _, unused := range result.UnusedTopics {
		byName[unused.Name] = unused
	}

	for _, name := range result.Summary.RecommendedCleanup {
		unused, ok := byName[name]
		if !ok {
			t.Errorf("recommended_cleanup_topics names %q which is not in unused_topics", name)
			continue
		}
		if unused.ManagedBy != "" {
			t.Errorf("recommended_cleanup_topics names %q, managed by %s", name, unused.ManagedBy)
		}
		if strings.HasPrefix(unused.Recommendation, doNotDeletePrefix) {
			t.Errorf("recommended_cleanup_topics names %q whose recommendation is %q", name, unused.Recommendation)
		}
		if unused.Recommendation == doNotActAdvice {
			t.Errorf("recommended_cleanup_topics names %q from an unverified scan", name)
		}
	}

	if !result.Reliability.ConsumerGroupsComplete && len(result.Summary.RecommendedCleanup) > 0 {
		t.Errorf("degraded scan published a cleanup list: %v", result.Summary.RecommendedCleanup)
	}
}

// WO-39: a plain `audit --output json` against a cluster with the offsets topic
// listed __consumer_offsets for deletion. Internal topics are deliberately still
// analysed when --exclude-internal is off, so the cleanup list is the guard.
func TestConsumerOffsetsNeverRecommendedForCleanup(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"__consumer_offsets": topic("__consumer_offsets", 50, 3),
			"orders":             topic("orders", 1, 1),
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
	}

	result := buildAuditResult(metadata, false, nil)
	assertCleanupListConsistent(t, result)

	for _, name := range result.Summary.RecommendedCleanup {
		if name == "__consumer_offsets" {
			t.Fatal("__consumer_offsets was named for cleanup")
		}
	}
	if got := unusedByName(result, "__consumer_offsets"); got == nil {
		t.Fatal("__consumer_offsets should still be analysed when --exclude-internal is off")
	} else if !strings.HasPrefix(got.Recommendation, doNotDeletePrefix) {
		t.Fatalf("__consumer_offsets recommendation = %q", got.Recommendation)
	}
}

// WO-39: a degraded scan must not publish a named delete list.
func TestDegradedScanPublishesNoCleanupList(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"orders":   topic("orders", 1, 1),
			"payments": topic("payments", 12, 3),
		},
		ConsumerGroups:          map[string]*kafka.ConsumerGroupInfo{},
		ConsumerGroupReadErrors: []string{"describe consumer groups: broker unreachable"},
	}

	result := buildAuditResult(metadata, false, nil)
	assertCleanupListConsistent(t, result)

	if len(result.Summary.RecommendedCleanup) != 0 {
		t.Fatalf("degraded scan cleanup list = %v, want empty", result.Summary.RecommendedCleanup)
	}
}

// WO-39: --include-managed surfaces managed topics but must not promote them
// into the cleanup list.
func TestIncludeManagedKeepsManagedTopicsOutOfCleanupList(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"_schemas": topic("_schemas", 1, 1),
			"orders":   topic("orders", 1, 1),
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
	}

	result := buildAuditResultWithOptions(metadata, false, nil, true)
	assertCleanupListConsistent(t, result)

	if unusedByName(result, "_schemas") == nil {
		t.Fatal("--include-managed should surface _schemas")
	}
	for _, name := range result.Summary.RecommendedCleanup {
		if name == "_schemas" {
			t.Fatal("_schemas was named for cleanup under --include-managed")
		}
	}
}

// WO-38: the check path had no reliability signal, so a failed consumer-group
// read produced confident UNUSED findings for every cluster topic.
func TestCheckSurfacesDegradedScan(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"orders":   topic("orders", 1, 1),
			"payments": topic("payments", 12, 3),
		},
		ConsumerGroups:          map[string]*kafka.ConsumerGroupInfo{},
		ConsumerGroupReadErrors: []string{"describe consumer groups: broker unreachable"},
	}
	scanResult := &scanner.Result{
		RepoPath: "/repo",
		Topics:   map[string]*scanner.TopicReference{"orders": {Topic: "orders"}},
	}

	result := buildCheckResult(scanResult, metadata, false, nil)

	if result.Reliability.ConsumerGroupsComplete {
		t.Fatal("check result should report an incomplete consumer-group read")
	}
	if len(result.Reliability.ReadErrors) == 0 {
		t.Fatal("check result should carry the read errors")
	}
	for _, finding := range result.Findings {
		if strings.Contains(finding.Reason, "has no active consumer groups") {
			t.Errorf("finding %q asserts absence as fact on a degraded scan: %q", finding.Topic, finding.Reason)
		}
	}
}

// WO-38: a clean check scan keeps its precise reasons.
func TestCheckCleanScanKeepsPreciseReasons(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers:        []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics:         map[string]*kafka.TopicInfo{"orders": topic("orders", 1, 1)},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
	}
	scanResult := &scanner.Result{
		RepoPath: "/repo",
		Topics:   map[string]*scanner.TopicReference{"orders": {Topic: "orders"}},
	}

	result := buildCheckResult(scanResult, metadata, false, nil)

	if !result.Reliability.ConsumerGroupsComplete {
		t.Fatal("clean check scan should report complete data")
	}
	if len(result.Findings) != 1 || !strings.Contains(result.Findings[0].Reason, "no active consumer groups") {
		t.Fatalf("clean scan reason = %+v", result.Findings)
	}
}

// WO-42: the managed hold-out is applied to cluster topics AND repo topics. It
// used to filter only the cluster side, so a Connect worker's
// offset.storage.topic reference made connect-offsets look MISSING_IN_CLUSTER
// even though the cluster had it.
func TestManagedTopicReferencedInRepoIsNotMissingInCluster(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"connect-offsets": topic("connect-offsets", 25, 3),
			"orders":          topic("orders", 1, 1),
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
			"cg": {GroupID: "cg", State: "Stable", Topics: []string{"orders"}},
		},
	}
	scanResult := &scanner.Result{
		RepoPath: "/repo",
		Topics: map[string]*scanner.TopicReference{
			"connect-offsets": {Topic: "connect-offsets"},
			"orders":          {Topic: "orders"},
		},
	}

	result := buildCheckResult(scanResult, metadata, false, nil)

	for _, finding := range result.Findings {
		if finding.Topic == "connect-offsets" {
			t.Fatalf("managed topic surfaced as %s: %q", finding.Status, finding.Reason)
		}
	}
	if result.Summary.MissingInClusterCount != 0 {
		t.Fatalf("missing_in_cluster_count = %d, want 0", result.Summary.MissingInClusterCount)
	}
}

// WO-42: a genuinely absent topic must still be reported missing.
func TestGenuinelyMissingTopicStillReported(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers:        []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics:         map[string]*kafka.TopicInfo{},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
	}
	scanResult := &scanner.Result{
		RepoPath: "/repo",
		Topics:   map[string]*scanner.TopicReference{"never-created": {Topic: "never-created"}},
	}

	result := buildCheckResult(scanResult, metadata, false, nil)

	if result.Summary.MissingInClusterCount != 1 {
		t.Fatalf("missing_in_cluster_count = %d, want 1", result.Summary.MissingInClusterCount)
	}
}

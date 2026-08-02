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

// WO-39: a plain `audit --output json` listed __consumer_offsets for deletion.
// It is now held out of the analysis entirely.
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

	if unusedByName(result, "__consumer_offsets") != nil {
		t.Fatal("__consumer_offsets leaked into unused_topics")
	}
	if result.Summary.ManagedTopicsHeldOut < 1 {
		t.Fatal("__consumer_offsets should be counted as held out")
	}
	for _, name := range result.Summary.RecommendedCleanup {
		if name == "__consumer_offsets" {
			t.Fatal("__consumer_offsets was named for cleanup")
		}
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

	if managedByName(result, "_schemas") == nil {
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

// WO-42: the managed hold-out applies to cluster topics AND repo topics.
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

// Round 2: the round-1 hold-out used a different predicate on each side of the
// union, so a repo reference to __consumer_offsets produced UNREFERENCED_IN_REPO.
func TestInternalTopicReferencedInRepoIsNotUnreferenced(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics:  map[string]*kafka.TopicInfo{"__consumer_offsets": topic("__consumer_offsets", 50, 3)},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
			"cg": {GroupID: "cg", State: "Stable", Topics: []string{"__consumer_offsets"}},
		},
	}
	scanResult := &scanner.Result{
		RepoPath: "/repo",
		Topics:   map[string]*scanner.TopicReference{"__consumer_offsets": {Topic: "__consumer_offsets"}},
	}

	result := buildCheckResult(scanResult, metadata, false, nil)

	for _, finding := range result.Findings {
		if finding.Topic == "__consumer_offsets" && !finding.ReferencedInRepo {
			t.Fatalf("repo-referenced topic reported as unreferenced: %q", finding.Reason)
		}
	}
	if result.Summary.UnreferencedInRepoCount != 0 {
		t.Fatalf("unreferenced_in_repo_count = %d, want 0", result.Summary.UnreferencedInRepoCount)
	}
}

// Round 2: a managed topic referenced but ABSENT is a genuine missing-topic
// finding (typo, wrong cluster, Connect never started).
func TestManagedTopicReferencedButAbsentIsStillMissing(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers:        []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics:         map[string]*kafka.TopicInfo{},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
	}
	scanResult := &scanner.Result{
		RepoPath: "/repo",
		Topics:   map[string]*scanner.TopicReference{"connect-offsets": {Topic: "connect-offsets"}},
	}

	result := buildCheckResult(scanResult, metadata, false, nil)

	if result.Summary.MissingInClusterCount != 1 {
		t.Fatalf("missing_in_cluster_count = %d, want 1", result.Summary.MissingInClusterCount)
	}
}

// Round 2: a managed topic can never appear in unused_topics in any mode. This
// is the structural guarantee that makes per-site jq filters unnecessary.
func TestUnusedTopicsNeverContainsAManagedTopic(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"_schemas":               topic("_schemas", 1, 1),
			"__consumer_offsets":     topic("__consumer_offsets", 50, 3),
			"connect-configs":        topic("connect-configs", 1, 3),
			"app-changelog":          topic("app-changelog", 3, 1),
			"mm2-offsets.a.internal": topic("mm2-offsets.a.internal", 2, 1),
			"orders":                 topic("orders", 1, 1),
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
	}

	for _, includeManaged := range []bool{false, true} {
		for _, excludeInternal := range []bool{false, true} {
			for _, degraded := range []bool{false, true} {
				md := *metadata
				if degraded {
					md.ConsumerGroupReadErrors = []string{"broker unreachable"}
				}
				result := buildAuditResultWithOptions(&md, excludeInternal, nil, includeManaged)

				for _, unused := range result.UnusedTopics {
					if unused.ManagedBy != "" {
						t.Errorf("includeManaged=%v excludeInternal=%v degraded=%v: unused_topics contains managed topic %q (%s)",
							includeManaged, excludeInternal, degraded, unused.Name, unused.ManagedBy)
					}
				}
				assertCleanupListConsistent(t, result)
			}
		}
	}
}

// Round 2: the hold-out must be discoverable. Topics and their partitions used
// to vanish from every total with nothing naming them.
func TestManagedHoldOutIsCounted(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"_schemas":      topic("_schemas", 1, 1),
			"app-changelog": topic("app-changelog", 3, 1),
			"orders":        topic("orders", 1, 1),
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
	}

	result := buildAuditResult(metadata, false, nil)

	if result.Summary.ManagedTopicsHeldOut != 2 {
		t.Fatalf("managed_topics_held_out = %d, want 2", result.Summary.ManagedTopicsHeldOut)
	}
	if result.UnusedCount != 1 {
		t.Fatalf("unused count = %d, want 1", result.UnusedCount)
	}
}

// Round 3 (self-review): analyzed == active + unused must hold across all flag
// combinations. Managed topics are neither; they must not inflate the analyzed
// count.
func TestAuditCountingConsistency(t *testing.T) {
	mk := func() *kafka.ClusterMetadata {
		return &kafka.ClusterMetadata{
			Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
			Topics: map[string]*kafka.TopicInfo{
				"_schemas":           topic("_schemas", 1, 1),
				"connect-configs":    topic("connect-configs", 1, 3),
				"app-changelog":      topic("app-changelog", 3, 1),
				"__consumer_offsets": topic("__consumer_offsets", 50, 3),
				"orders-unused":      topic("orders-unused", 1, 1),
				"payments-unused":    topic("payments-unused", 12, 3),
				"active-topic":       topic("active-topic", 4, 2),
			},
			ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
				"cg": {GroupID: "cg", State: "Stable", Topics: []string{"active-topic"}},
			},
		}
	}

	for _, excludeInternal := range []bool{false, true} {
		for _, includeManaged := range []bool{false, true} {
			result := buildAuditResultWithOptions(mk(), excludeInternal, nil, includeManaged)

			if got, want := result.TotalTopics, result.ActiveCount+result.UnusedCount; got != want {
				t.Errorf("excludeInternal=%v includeManaged=%v: analyzed=%d != active+unused=%d",
					excludeInternal, includeManaged, got, want)
			}
			if result.Summary.TotalTopics != result.TotalTopics {
				t.Errorf("summary.total_topics_analyzed %d != result.TotalTopics %d", result.Summary.TotalTopics, result.TotalTopics)
			}
			// Managed topics must not inflate the exit-code findings count.
			if result.UnusedCount > 2 {
				t.Errorf("UnusedCount=%d, want at most 2 (managed must not count)", result.UnusedCount)
			}
		}
	}
}

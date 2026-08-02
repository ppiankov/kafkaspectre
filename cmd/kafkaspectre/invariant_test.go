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
	for _, managed := range result.ManagedTopics {
		byName[managed.Name] = managed
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
	if got := managedByName(result, "__consumer_offsets"); got == nil {
		t.Fatal("__consumer_offsets should still be reported under managed topics")
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

// Round 2, FINDING 3: the round-1 hold-out used a different predicate on each
// side of the union, so a repo reference to __consumer_offsets produced
// UNREFERENCED_IN_REPO with the reason "was not found in repository" — a claim
// contradicted by the reference that triggered it.
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

// Round 2, FINDING 4: holding managed topics out of BOTH sides also suppressed
// the genuine case — a Connect worker pointing at a backing topic that was
// never created (typo, wrong cluster, Connect never started). A managed topic
// is only uninteresting once confirmed to exist.
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
		t.Fatalf("missing_in_cluster_count = %d, want 1 — a referenced-but-absent backing topic is a real finding", result.Summary.MissingInClusterCount)
	}
}

// Round 2: the reviewer found the round-1 fix pattern repeating — every jq
// pipeline in docs/cleanup-guide.md that selects on `risk` is a separate write
// site, and gating them one at a time means the next one gets missed. The fix
// is structural: a managed topic can never appear in `unused_topics` at all, in
// either mode, so no per-site filter is load-bearing.
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
// to vanish from every total with nothing naming them, silently changing the
// health score.
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
		t.Fatalf("unused count = %d, want 1 — managed topics are not findings", result.UnusedCount)
	}
}

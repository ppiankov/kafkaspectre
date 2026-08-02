package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
	"github.com/ppiankov/kafkaspectre/internal/reporter"
)

func topic(name string, partitions, replication int) *kafka.TopicInfo {
	return &kafka.TopicInfo{
		Name:              name,
		Partitions:        partitions,
		ReplicationFactor: replication,
		Internal:          strings.HasPrefix(name, "__"),
		Config:            map[string]string{},
	}
}

func unusedByName(result *reporter.AuditResult, name string) *reporter.UnusedTopic {
	for _, unused := range result.UnusedTopics {
		if unused.Name == name {
			return unused
		}
	}
	return nil
}

// WO-26: the motivating case. `_schemas` is the Confluent Schema Registry
// backing store; deleting it destroys every registered schema in the cluster
// and is not recoverable from Kafka. It has no consumer groups, so before this
// fix it was reported unused with "Safe to delete after confirmation".
func TestManagedTopicsNeverRecommendedForDeletion(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "broker-1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"_schemas":        topic("_schemas", 1, 3),
			"connect-configs": topic("connect-configs", 1, 3),
			"connect-offsets": topic("connect-offsets", 25, 3),
			"connect-status":  topic("connect-status", 5, 3),
			"orders":          topic("orders", 1, 1),
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
	}

	result := buildAuditResult(metadata, false, nil)

	for _, managed := range []string{"_schemas", "connect-configs", "connect-offsets", "connect-status"} {
		if got := unusedByName(result, managed); got != nil {
			t.Fatalf("managed topic %q reported as unused with recommendation %q", managed, got.Recommendation)
		}
	}

	orders := unusedByName(result, "orders")
	if orders == nil {
		t.Fatal("ordinary topic 'orders' should still be reported unused")
	}
	if orders.Recommendation != "Safe to delete after confirmation" {
		t.Fatalf("ordinary topic recommendation = %q", orders.Recommendation)
	}
	if result.TotalTopics != 1 {
		t.Fatalf("analyzed topic count = %d, want 1 (managed topics held out)", result.TotalTopics)
	}
}

// WO-26: the escape hatch must surface managed topics WITHOUT delete advice.
func TestIncludeManagedSurfacesTopicsWithDoNotDeleteAdvice(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers:        []kafka.BrokerInfo{{ID: 1, Host: "broker-1", Port: 9092}},
		Topics:         map[string]*kafka.TopicInfo{"_schemas": topic("_schemas", 1, 3)},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
	}

	result := buildAuditResultWithOptions(metadata, false, nil, true)

	schemas := unusedByName(result, "_schemas")
	if schemas == nil {
		t.Fatal("--include-managed should surface _schemas")
	}
	if !strings.Contains(schemas.Recommendation, "DO NOT DELETE") {
		t.Fatalf("recommendation = %q, want a do-not-delete advisory", schemas.Recommendation)
	}
	if schemas.ManagedBy != string(kafka.OwnerSchemaRegistry) {
		t.Fatalf("managed_by = %q, want %q", schemas.ManagedBy, kafka.OwnerSchemaRegistry)
	}
}

// WO-26: --exclude-internal must keep governing broker-internal topics. The
// managed classification must not silently take over an existing flag.
func TestManagedClassificationDoesNotOverrideExcludeInternal(t *testing.T) {
	newMetadata := func() *kafka.ClusterMetadata {
		return &kafka.ClusterMetadata{
			Brokers: []kafka.BrokerInfo{{ID: 1, Host: "broker-1", Port: 9092}},
			Topics: map[string]*kafka.TopicInfo{
				"__consumer_offsets": topic("__consumer_offsets", 50, 3),
				"orders":             topic("orders", 1, 1),
			},
			ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
		}
	}

	included := buildAuditResult(newMetadata(), false, nil)
	if included.TotalTopics != 2 {
		t.Fatalf("exclude-internal=false analyzed %d topics, want 2", included.TotalTopics)
	}

	excluded := buildAuditResult(newMetadata(), true, nil)
	if excluded.TotalTopics != 1 {
		t.Fatalf("exclude-internal=true analyzed %d topics, want 1", excluded.TotalTopics)
	}
}

// WO-27: a DescribeGroups failure previously left ConsumerGroups empty, so
// every topic in the cluster was reported unused with a delete recommendation.
// A transient broker hiccup must not turn into "delete all your topics".
func TestDegradedConsumerGroupReadSuppressesDeleteAdvice(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "broker-1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"orders":   topic("orders", 1, 1),
			"payments": topic("payments", 12, 3),
		},
		ConsumerGroups:          map[string]*kafka.ConsumerGroupInfo{},
		ConsumerGroupReadErrors: []string{"describe consumer groups (4 groups): broker unavailable"},
	}

	result := buildAuditResult(metadata, false, nil)

	if result.Reliability.ConsumerGroupsComplete {
		t.Fatal("reliability should report an incomplete consumer-group read")
	}
	if len(result.Reliability.ReadErrors) != 1 {
		t.Fatalf("read errors = %v, want the describe failure recorded", result.Reliability.ReadErrors)
	}

	for _, unused := range result.UnusedTopics {
		if strings.Contains(strings.ToLower(unused.Recommendation), "safe to delete") {
			t.Fatalf("topic %q carries delete advice from an unverified scan: %q", unused.Name, unused.Recommendation)
		}
		if !strings.Contains(unused.Reason, "UNVERIFIED") {
			t.Fatalf("topic %q reason does not mark the scan unverified: %q", unused.Name, unused.Reason)
		}
	}
}

// WO-27: a clean scan must still produce ordinary actionable advice.
func TestCompleteScanKeepsActionableAdvice(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers:        []kafka.BrokerInfo{{ID: 1, Host: "broker-1", Port: 9092}},
		Topics:         map[string]*kafka.TopicInfo{"orders": topic("orders", 1, 1)},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
	}

	result := buildAuditResult(metadata, false, nil)

	if !result.Reliability.ConsumerGroupsComplete {
		t.Fatal("clean scan should report complete consumer-group data")
	}
	if got := unusedByName(result, "orders"); got == nil || got.Recommendation != "Safe to delete after confirmation" {
		t.Fatalf("clean scan advice = %+v", got)
	}
}

// WO-29: an Empty or Dead group holding stale offsets is evidence a topic is no
// longer consumed. Counting it as an active consumer inverted that signal and
// hid the topic from the unused list entirely.
func TestAbandonedConsumerGroupsDoNotMarkTopicsActive(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "broker-1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"abandoned-topic": topic("abandoned-topic", 1, 1),
			"live-topic":      topic("live-topic", 1, 1),
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
			"dead-cg":  {GroupID: "dead-cg", State: "Empty", Topics: []string{"abandoned-topic"}},
			"alive-cg": {GroupID: "alive-cg", State: "Stable", Topics: []string{"live-topic"}},
		},
	}

	result := buildAuditResult(metadata, false, nil)

	abandoned := unusedByName(result, "abandoned-topic")
	if abandoned == nil {
		t.Fatal("topic whose only consumer group is Empty should be reported unused")
	}
	if got, want := abandoned.AbandonedConsumerGroups, []string{"dead-cg"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("abandoned groups = %v, want %v", got, want)
	}
	if !strings.Contains(abandoned.Reason, "abandoned") {
		t.Fatalf("reason should name the abandoned group: %q", abandoned.Reason)
	}

	if unusedByName(result, "live-topic") != nil {
		t.Fatal("topic with a Stable consumer group must not be reported unused")
	}
}

// WO-31: structured reporters serialize UnusedTopics in the order they receive
// it, so severity ordering has to hold at the source, not only in the text
// reporter. Names are chosen so alphabetical and severity order disagree.
func TestUnusedTopicsAreSeverityOrderedAtSource(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "broker-1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"aaa-low":     topic("aaa-low", 1, 1),
			"mmm-high":    topic("mmm-high", 12, 3),
			"zzz-medium":  topic("zzz-medium", 2, 1),
			"bbb-high":    topic("bbb-high", 1, 3),
			"nnn-lowtoo":  topic("nnn-lowtoo", 1, 1),
			"ccc-medium2": topic("ccc-medium2", 2, 1),
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
	}

	result := buildAuditResult(metadata, false, nil)

	want := []string{"bbb-high", "mmm-high", "ccc-medium2", "zzz-medium", "aaa-low", "nnn-lowtoo"}
	if got := unusedNames(result.UnusedTopics); !reflect.DeepEqual(got, want) {
		t.Fatalf("unused order = %v, want %v", got, want)
	}
}

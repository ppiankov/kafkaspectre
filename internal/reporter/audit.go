package reporter

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

// AuditResult contains the results of a cluster audit
type AuditResult struct {
	Tool      string // tool identifier for SpectreHub compatibility
	Version   string // tool version for SpectreHub compatibility
	Timestamp string // RFC3339 generation timestamp for SpectreHub compatibility

	Summary       *AuditSummary
	UnusedTopics  []*UnusedTopic
	ActiveTopics  []*ActiveTopic
	Metadata      *kafka.ClusterMetadata
	TotalTopics   int
	UnusedCount   int
	ActiveCount   int
	InternalCount int

	// ManagedTopics lists service backing topics that have no consumer groups.
	// They are reported for visibility but are NOT findings: they are excluded
	// from UnusedCount, the partition statistics, the health score, and the
	// exit code.
	//
	// Round 2: counting them as unused meant a healthy cluster exited 6 on
	// default flags purely because of __consumer_offsets, and advertised 96%
	// "reclaimable" partitions from a topic the same report labelled
	// DO NOT DELETE.
	ManagedTopics []*UnusedTopic

	// Reliability records whether the underlying cluster reads were complete.
	// WO-27: unused-topic findings are only actionable when they were.
	Reliability ScanReliability
}

// AuditSummary provides high-level audit insights
type AuditSummary struct {
	// Cluster Overview
	ClusterName  string `json:"cluster_name"`
	TotalBrokers int    `json:"total_brokers"`

	// Topic Statistics
	TotalTopicsIncludingInternal int     `json:"total_topics_including_internal"`
	TotalTopics                  int     `json:"total_topics_analyzed"`
	UnusedTopics                 int     `json:"unused_topics"`
	ActiveTopics                 int     `json:"active_topics"`
	InternalTopics               int     `json:"internal_topics_excluded"`
	UnusedPercentage             float64 `json:"unused_percentage"`

	// Partition Statistics
	TotalPartitions         int     `json:"total_partitions"`
	UnusedPartitions        int     `json:"unused_partitions"`
	ActivePartitions        int     `json:"active_partitions"`
	UnusedPartitionsPercent float64 `json:"unused_partitions_percentage"`

	// Consumer Group Statistics
	TotalConsumerGroups int `json:"total_consumer_groups"`

	// Risk Breakdown
	HighRiskCount   int `json:"high_risk_count"`
	MediumRiskCount int `json:"medium_risk_count"`
	LowRiskCount    int `json:"low_risk_count"`

	// Recommendations
	RecommendedCleanup []string `json:"recommended_cleanup_topics"`
	ClusterHealthScore string   `json:"cluster_health_score"`

	// ManagedTopicsHeldOut counts service backing topics excluded from the
	// analysis. Round 2: the hold-out was previously invisible — topics and
	// their partitions vanished from every total with nothing naming them.
	ManagedTopicsHeldOut int `json:"managed_topics_held_out"`

	// Stakeholder Metrics
	PotentialSavingsInfo string `json:"potential_savings_info"`
}

// UnusedTopic represents a topic that has no active consumers
type UnusedTopic struct {
	Name              string            `json:"name"`
	Partitions        int               `json:"partitions"`
	ReplicationFactor int               `json:"replication_factor"`
	RetentionMs       string            `json:"retention_ms"`
	RetentionHuman    string            `json:"retention_human"`
	CleanupPolicy     string            `json:"cleanup_policy"`
	MinInsyncReplicas string            `json:"min_insync_replicas"`
	InterestingConfig map[string]string `json:"interesting_config"`
	Reason            string            `json:"reason"`
	Recommendation    string            `json:"recommendation"`
	Risk              string            `json:"risk"`
	CleanupPriority   int               `json:"cleanup_priority"`

	// ManagedBy names the service that owns this topic as backing store.
	// WO-26: a non-empty value means the topic must never be deleted.
	ManagedBy string `json:"managed_by,omitempty"`

	// AbandonedConsumerGroups lists groups that reference this topic but hold
	// no live members. WO-29: these are why the topic reads as unused.
	AbandonedConsumerGroups []string `json:"abandoned_consumer_groups,omitempty"`
}

// ScanReliability describes whether the scan saw a complete cluster picture.
//
// WO-27: without this a degraded read is indistinguishable from a clean scan,
// and downstream consumers treat "could not read consumers" as "no consumers".
type ScanReliability struct {
	ConsumerGroupsComplete bool     `json:"consumer_groups_complete"`
	ReadErrors             []string `json:"read_errors,omitempty"`
}

// ActiveTopic represents a topic with active consumers
type ActiveTopic struct {
	Name              string   `json:"name"`
	Partitions        int      `json:"partitions"`
	ReplicationFactor int      `json:"replication_factor"`
	ConsumerGroups    []string `json:"consumer_groups"`
	ConsumerCount     int      `json:"consumer_count"`
}

// Reporter interface extended with audit capabilities
type AuditReporter interface {
	GenerateAudit(ctx context.Context, result *AuditResult) error
}

// SortUnusedTopicsBySeverity orders unused topics by risk descending, then by
// name. It is the single definition of severity ordering for this tool.
//
// WO-31: ordering used to live only inside the text reporter, so the JSON,
// SARIF and SpectreHub outputs emitted name-ordered findings and downstream
// consumers reading the first N findings got an alphabetical sample rather than
// the high-risk ones. Callers apply this at the source; reporters may reapply
// it because it is idempotent.
func SortUnusedTopicsBySeverity(topics []*UnusedTopic) {
	sort.SliceStable(topics, func(i, j int) bool {
		if topics[i].Risk != topics[j].Risk {
			return RiskLevel(topics[i].Risk) > RiskLevel(topics[j].Risk)
		}
		return topics[i].Name < topics[j].Name
	})
}

// Helper functions

// FilterInterestingConfig extracts only non-default and important config values
func FilterInterestingConfig(config map[string]string) map[string]string {
	interesting := make(map[string]string)

	importantKeys := map[string]bool{
		"retention.ms":        true,
		"retention.bytes":     true,
		"cleanup.policy":      true,
		"min.insync.replicas": true,
		"compression.type":    true,
		"max.message.bytes":   true,
		"segment.ms":          true,
		"segment.bytes":       true,
		"delete.retention.ms": true,
	}

	for key, value := range config {
		if importantKeys[key] {
			interesting[key] = value
		}
	}

	return interesting
}

// FormatRetentionMs converts retention milliseconds to human-readable format
func FormatRetentionMs(retentionMs string) string {
	if retentionMs == "" || retentionMs == "-1" {
		return "infinite"
	}

	ms, err := strconv.ParseInt(retentionMs, 10, 64)
	if err != nil {
		return retentionMs
	}

	// Convert to duration
	duration := time.Duration(ms) * time.Millisecond

	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24

	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("%d days %d hours", days, hours)
		}
		return fmt.Sprintf("%d days", days)
	}

	if hours > 0 {
		return fmt.Sprintf("%d hours", hours)
	}

	minutes := int(duration.Minutes())
	if minutes > 0 {
		return fmt.Sprintf("%d minutes", minutes)
	}

	return fmt.Sprintf("%d ms", ms)
}

// BuildUnusedTopic creates an UnusedTopic from TopicInfo with enhanced fields
func BuildUnusedTopic(topic *kafka.TopicInfo, reason, recommendation, risk string, priority int) *UnusedTopic {
	retentionMs := topic.Config["retention.ms"]

	return &UnusedTopic{
		ManagedBy:         string(topic.ManagedOwner()),
		Name:              topic.Name,
		Partitions:        topic.Partitions,
		ReplicationFactor: topic.ReplicationFactor,
		RetentionMs:       retentionMs,
		RetentionHuman:    FormatRetentionMs(retentionMs),
		CleanupPolicy:     topic.Config["cleanup.policy"],
		MinInsyncReplicas: topic.Config["min.insync.replicas"],
		InterestingConfig: FilterInterestingConfig(topic.Config),
		Reason:            reason,
		Recommendation:    recommendation,
		Risk:              risk,
		CleanupPriority:   priority,
	}
}

// BuildActiveTopic creates an ActiveTopic from TopicInfo with enhanced fields
func BuildActiveTopic(topic *kafka.TopicInfo, consumers []string) *ActiveTopic {
	return &ActiveTopic{
		Name:              topic.Name,
		Partitions:        topic.Partitions,
		ReplicationFactor: topic.ReplicationFactor,
		ConsumerGroups:    consumers,
		ConsumerCount:     len(consumers),
	}
}

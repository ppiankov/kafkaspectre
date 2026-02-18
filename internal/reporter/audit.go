package reporter

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

// AuditResult contains the results of a cluster audit
type AuditResult struct {
	Summary       *AuditSummary
	UnusedTopics  []*UnusedTopic
	ActiveTopics  []*ActiveTopic
	Metadata      *kafka.ClusterMetadata
	TotalTopics   int
	UnusedCount   int
	ActiveCount   int
	InternalCount int
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

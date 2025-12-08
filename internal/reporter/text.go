package reporter

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

// TextReporter generates human-readable text reports
type TextReporter struct {
	writer io.Writer
	color  bool
}

// NewTextReporter creates a new text reporter
func NewTextReporter(w io.Writer, color bool) *TextReporter {
	return &TextReporter{
		writer: w,
		color:  color,
	}
}

// Generate produces a human-readable text report
func (r *TextReporter) Generate(ctx context.Context, metadata *kafka.ClusterMetadata) error {
	fmt.Fprintf(r.writer, "Kafka Cluster Overview\n")
	fmt.Fprintf(r.writer, "======================\n\n")

	// Broker information
	fmt.Fprintf(r.writer, "Brokers: %d\n", len(metadata.Brokers))
	for _, broker := range metadata.Brokers {
		fmt.Fprintf(r.writer, "  - Broker %d: %s:%d", broker.ID, broker.Host, broker.Port)
		if broker.Rack != "" {
			fmt.Fprintf(r.writer, " (rack: %s)", broker.Rack)
		}
		fmt.Fprintf(r.writer, "\n")
	}
	fmt.Fprintf(r.writer, "\n")

	// Topic summary
	totalTopics := len(metadata.Topics)
	internalTopics := 0
	userTopics := 0

	for _, topic := range metadata.Topics {
		if topic.Internal {
			internalTopics++
		} else {
			userTopics++
		}
	}

	fmt.Fprintf(r.writer, "Topics: %d total (%d user, %d internal)\n\n", totalTopics, userTopics, internalTopics)

	// List topics (sorted)
	topicNames := make([]string, 0, len(metadata.Topics))
	for name := range metadata.Topics {
		topicNames = append(topicNames, name)
	}
	sort.Strings(topicNames)

	fmt.Fprintf(r.writer, "Topic Details:\n")
	fmt.Fprintf(r.writer, "==============\n\n")

	for _, name := range topicNames {
		topic := metadata.Topics[name]
		if topic.Internal {
			continue // Skip internal topics in detailed view
		}

		fmt.Fprintf(r.writer, "[Topic] %s\n", topic.Name)
		fmt.Fprintf(r.writer, "  Partitions: %d\n", topic.Partitions)
		fmt.Fprintf(r.writer, "  Replication Factor: %d\n", topic.ReplicationFactor)

		// Display key configurations
		if retention, ok := topic.Config["retention.ms"]; ok {
			fmt.Fprintf(r.writer, "  Retention: %s ms\n", retention)
		}
		if cleanup, ok := topic.Config["cleanup.policy"]; ok {
			fmt.Fprintf(r.writer, "  Cleanup Policy: %s\n", cleanup)
		}

		// Find consumer groups for this topic
		consumerGroups := r.findConsumerGroupsForTopic(metadata, name)
		if len(consumerGroups) > 0 {
			fmt.Fprintf(r.writer, "  Consumer Groups: %s\n", strings.Join(consumerGroups, ", "))
		} else {
			fmt.Fprintf(r.writer, "  Consumer Groups: none\n")
		}

		fmt.Fprintf(r.writer, "\n")
	}

	// Consumer group summary
	fmt.Fprintf(r.writer, "Consumer Groups: %d\n", len(metadata.ConsumerGroups))
	fmt.Fprintf(r.writer, "================\n\n")

	groupNames := make([]string, 0, len(metadata.ConsumerGroups))
	for name := range metadata.ConsumerGroups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)

	for _, name := range groupNames {
		group := metadata.ConsumerGroups[name]
		fmt.Fprintf(r.writer, "[Group] %s\n", group.GroupID)
		fmt.Fprintf(r.writer, "  State: %s\n", group.State)
		fmt.Fprintf(r.writer, "  Members: %d\n", group.Members)
		fmt.Fprintf(r.writer, "  Topics: %s\n", strings.Join(group.Topics, ", "))

		// Display lag information
		if len(group.Lag) > 0 {
			totalLag := int64(0)
			for _, lag := range group.Lag {
				totalLag += lag
			}
			fmt.Fprintf(r.writer, "  Total Lag: %d messages\n", totalLag)

			// Show per-topic lag if multiple topics
			if len(group.Lag) > 1 {
				for topic, lag := range group.Lag {
					if lag > 0 {
						fmt.Fprintf(r.writer, "    - %s: %d\n", topic, lag)
					}
				}
			}
		}

		if !group.LastCommit.IsZero() {
			fmt.Fprintf(r.writer, "  Last Commit: %s\n", group.LastCommit.Format("2006-01-02 15:04:05"))
		}

		fmt.Fprintf(r.writer, "\n")
	}

	return nil
}

// findConsumerGroupsForTopic returns the list of consumer groups consuming from a topic
func (r *TextReporter) findConsumerGroupsForTopic(metadata *kafka.ClusterMetadata, topicName string) []string {
	groups := []string{}
	for _, group := range metadata.ConsumerGroups {
		for _, topic := range group.Topics {
			if topic == topicName {
				groups = append(groups, group.GroupID)
				break
			}
		}
	}
	sort.Strings(groups)
	return groups
}

// GenerateAudit is a stub to satisfy the Reporter interface
func (r *TextReporter) GenerateAudit(ctx context.Context, result *AuditResult) error {
	// TextReporter doesn't support audit mode directly
	// Use AuditTextReporter instead
	return fmt.Errorf("audit mode not supported by TextReporter, use AuditTextReporter")
}

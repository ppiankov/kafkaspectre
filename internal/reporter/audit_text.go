package reporter

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

// AuditTextReporter generates human-readable audit reports
type AuditTextReporter struct {
	writer io.Writer
	color  bool
}

// NewAuditTextReporter creates a new audit text reporter
func NewAuditTextReporter(w io.Writer, color bool) *AuditTextReporter {
	return &AuditTextReporter{
		writer: w,
		color:  color,
	}
}

// GenerateAudit produces a human-readable audit report
func (r *AuditTextReporter) GenerateAudit(ctx context.Context, result *AuditResult) error {
	fmt.Fprintf(r.writer, "Kafka Cluster Audit Report\n")
	fmt.Fprintf(r.writer, "===========================\n\n")

	// Summary
	fmt.Fprintf(r.writer, "Summary:\n")
	fmt.Fprintf(r.writer, "========\n\n")

	// Cluster info
	if result.Summary != nil {
		fmt.Fprintf(r.writer, "Cluster: %s (%d brokers, %d consumer groups)\n\n",
			result.Summary.ClusterName,
			result.Summary.TotalBrokers,
			result.Summary.TotalConsumerGroups)

		// Topic statistics
		fmt.Fprintf(r.writer, "Topics:\n")
		fmt.Fprintf(r.writer, "  Total (including internal): %d\n", result.Summary.TotalTopicsIncludingInternal)
		fmt.Fprintf(r.writer, "  Analyzed:                   %d\n", result.Summary.TotalTopics)
		fmt.Fprintf(r.writer, "  Active (with consumers):    %d\n", result.Summary.ActiveTopics)
		fmt.Fprintf(r.writer, "  Unused (no consumers):      %d (%.1f%%)\n",
			result.Summary.UnusedTopics,
			result.Summary.UnusedPercentage)
		fmt.Fprintf(r.writer, "  Internal (excluded):        %d\n\n", result.Summary.InternalTopics)

		// Partition statistics
		fmt.Fprintf(r.writer, "Partitions:\n")
		fmt.Fprintf(r.writer, "  Total:    %d\n", result.Summary.TotalPartitions)
		fmt.Fprintf(r.writer, "  Active:   %d\n", result.Summary.ActivePartitions)
		fmt.Fprintf(r.writer, "  Unused:   %d (%.1f%%)\n\n",
			result.Summary.UnusedPartitions,
			result.Summary.UnusedPartitionsPercent)

		// Risk breakdown
		if result.Summary.UnusedTopics > 0 {
			fmt.Fprintf(r.writer, "Risk Breakdown:\n")
			fmt.Fprintf(r.writer, "  High Risk:   %d topics\n", result.Summary.HighRiskCount)
			fmt.Fprintf(r.writer, "  Medium Risk: %d topics\n", result.Summary.MediumRiskCount)
			fmt.Fprintf(r.writer, "  Low Risk:    %d topics\n\n", result.Summary.LowRiskCount)
		}

		// Health score
		fmt.Fprintf(r.writer, "Cluster Health: %s\n\n", result.Summary.ClusterHealthScore)

		// Potential savings
		fmt.Fprintf(r.writer, "Potential Savings: %s\n", result.Summary.PotentialSavingsInfo)
	} else {
		fmt.Fprintf(r.writer, "  Total Topics:    %d\n", result.TotalTopics)
		fmt.Fprintf(r.writer, "  Active Topics:   %d (with consumers)\n", result.ActiveCount)
		fmt.Fprintf(r.writer, "  Unused Topics:   %d (no consumers)\n", result.UnusedCount)
		fmt.Fprintf(r.writer, "  Internal Topics: %d (excluded from analysis)\n", result.InternalCount)
	}
	fmt.Fprintf(r.writer, "\n")

	// Unused Topics Section
	if len(result.UnusedTopics) > 0 {
		fmt.Fprintf(r.writer, "Unused Topics (No Consumer Groups)\n")
		fmt.Fprintf(r.writer, "===================================\n\n")

		// Sort by risk level then by name
		sortedUnused := make([]*UnusedTopic, len(result.UnusedTopics))
		copy(sortedUnused, result.UnusedTopics)
		sort.Slice(sortedUnused, func(i, j int) bool {
			if sortedUnused[i].Risk != sortedUnused[j].Risk {
				return riskLevel(sortedUnused[i].Risk) > riskLevel(sortedUnused[j].Risk)
			}
			return sortedUnused[i].Name < sortedUnused[j].Name
		})

		for _, unused := range sortedUnused {
			fmt.Fprintf(r.writer, "[UNUSED] %s\n", unused.Name)
			fmt.Fprintf(r.writer, "  Partitions: %d, Replication: %d\n", unused.Partitions, unused.ReplicationFactor)

			// Display key configurations
			if unused.RetentionHuman != "" {
				fmt.Fprintf(r.writer, "  Retention: %s\n", unused.RetentionHuman)
			}
			if unused.CleanupPolicy != "" {
				fmt.Fprintf(r.writer, "  Cleanup Policy: %s\n", unused.CleanupPolicy)
			}

			fmt.Fprintf(r.writer, "  Reason: %s\n", unused.Reason)
			fmt.Fprintf(r.writer, "  Risk: %s\n", unused.Risk)
			fmt.Fprintf(r.writer, "  Recommendation: %s\n", unused.Recommendation)
			fmt.Fprintf(r.writer, "\n")
		}
	}

	// Active Topics Section (Summary)
	if len(result.ActiveTopics) > 0 {
		fmt.Fprintf(r.writer, "Active Topics (With Consumer Groups)\n")
		fmt.Fprintf(r.writer, "=====================================\n\n")

		// Sort by name
		sortedActive := make([]*ActiveTopic, len(result.ActiveTopics))
		copy(sortedActive, result.ActiveTopics)
		sort.Slice(sortedActive, func(i, j int) bool {
			return sortedActive[i].Name < sortedActive[j].Name
		})

		for _, active := range sortedActive {
			fmt.Fprintf(r.writer, "[ACTIVE] %s\n", active.Name)
			fmt.Fprintf(r.writer, "  Partitions: %d, Replication: %d\n", active.Partitions, active.ReplicationFactor)
			fmt.Fprintf(r.writer, "  Consumer Groups (%d): ", len(active.ConsumerGroups))

			// Show first 3 consumer groups, then indicate if there are more
			if len(active.ConsumerGroups) <= 3 {
				for i, cg := range active.ConsumerGroups {
					if i > 0 {
						fmt.Fprintf(r.writer, ", ")
					}
					fmt.Fprintf(r.writer, "%s", cg)
				}
			} else {
				for i := 0; i < 3; i++ {
					if i > 0 {
						fmt.Fprintf(r.writer, ", ")
					}
					fmt.Fprintf(r.writer, "%s", active.ConsumerGroups[i])
				}
				fmt.Fprintf(r.writer, ", ... and %d more", len(active.ConsumerGroups)-3)
			}
			fmt.Fprintf(r.writer, "\n\n")
		}
	}

	// Recommendations
	if result.UnusedCount > 0 {
		fmt.Fprintf(r.writer, "Cleanup Recommendations\n")
		fmt.Fprintf(r.writer, "=======================\n\n")
		fmt.Fprintf(r.writer, "Found %d unused topics that may be candidates for deletion.\n\n", result.UnusedCount)
		fmt.Fprintf(r.writer, "Before deleting any topics:\n")
		fmt.Fprintf(r.writer, "  1. Verify with application owners that topics are truly unused\n")
		fmt.Fprintf(r.writer, "  2. Check if topics are consumed by external systems not visible here\n")
		fmt.Fprintf(r.writer, "  3. Consider archiving topic data before deletion\n")
		fmt.Fprintf(r.writer, "  4. Test in a non-production environment first\n")
		fmt.Fprintf(r.writer, "\n")
		fmt.Fprintf(r.writer, "Risk Levels:\n")
		fmt.Fprintf(r.writer, "  - low:    Safe to delete (small topic, no consumers)\n")
		fmt.Fprintf(r.writer, "  - medium: Review carefully (larger topic, no consumers)\n")
		fmt.Fprintf(r.writer, "  - high:   Do not delete without confirmation\n")
	} else {
		fmt.Fprintf(r.writer, "No unused topics detected. All topics have active consumer groups.\n")
	}

	return nil
}

// riskLevel converts risk string to numeric value for sorting
func riskLevel(risk string) int {
	switch risk {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

// Generate is a stub to satisfy the Reporter interface
func (r *AuditTextReporter) Generate(ctx context.Context, metadata *kafka.ClusterMetadata) error {
	// AuditTextReporter doesn't support standard generate mode
	return fmt.Errorf("standard mode not supported by AuditTextReporter, use TextReporter")
}

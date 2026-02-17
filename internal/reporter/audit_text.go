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
	var writeErr error
	writef := func(format string, args ...any) {
		if writeErr != nil {
			return
		}
		_, writeErr = fmt.Fprintf(r.writer, format, args...)
	}

	writef("Kafka Cluster Audit Report\n")
	writef("===========================\n\n")

	// Summary
	writef("Summary:\n")
	writef("========\n\n")

	// Cluster info
	if result.Summary != nil {
		writef("Cluster: %s (%d brokers, %d consumer groups)\n\n",
			result.Summary.ClusterName,
			result.Summary.TotalBrokers,
			result.Summary.TotalConsumerGroups)

		// Topic statistics
		writef("Topics:\n")
		writef("  Total (including internal): %d\n", result.Summary.TotalTopicsIncludingInternal)
		writef("  Analyzed:                   %d\n", result.Summary.TotalTopics)
		writef("  Active (with consumers):    %d\n", result.Summary.ActiveTopics)
		writef("  Unused (no consumers):      %d (%.1f%%)\n",
			result.Summary.UnusedTopics,
			result.Summary.UnusedPercentage)
		writef("  Internal (excluded):        %d\n\n", result.Summary.InternalTopics)

		// Partition statistics
		writef("Partitions:\n")
		writef("  Total:    %d\n", result.Summary.TotalPartitions)
		writef("  Active:   %d\n", result.Summary.ActivePartitions)
		writef("  Unused:   %d (%.1f%%)\n\n",
			result.Summary.UnusedPartitions,
			result.Summary.UnusedPartitionsPercent)

		// Risk breakdown
		if result.Summary.UnusedTopics > 0 {
			writef("Risk Breakdown:\n")
			writef("  High Risk:   %d topics\n", result.Summary.HighRiskCount)
			writef("  Medium Risk: %d topics\n", result.Summary.MediumRiskCount)
			writef("  Low Risk:    %d topics\n\n", result.Summary.LowRiskCount)
		}

		// Health score
		writef("Cluster Health: %s\n\n", result.Summary.ClusterHealthScore)

		// Potential savings
		writef("Potential Savings: %s\n", result.Summary.PotentialSavingsInfo)
	} else {
		writef("  Total Topics:    %d\n", result.TotalTopics)
		writef("  Active Topics:   %d (with consumers)\n", result.ActiveCount)
		writef("  Unused Topics:   %d (no consumers)\n", result.UnusedCount)
		writef("  Internal Topics: %d (excluded from analysis)\n", result.InternalCount)
	}
	writef("\n")

	// Unused Topics Section
	if len(result.UnusedTopics) > 0 {
		writef("Unused Topics (No Consumer Groups)\n")
		writef("===================================\n\n")

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
			writef("[UNUSED] %s\n", unused.Name)
			writef("  Partitions: %d, Replication: %d\n", unused.Partitions, unused.ReplicationFactor)

			// Display key configurations
			if unused.RetentionHuman != "" {
				writef("  Retention: %s\n", unused.RetentionHuman)
			}
			if unused.CleanupPolicy != "" {
				writef("  Cleanup Policy: %s\n", unused.CleanupPolicy)
			}

			writef("  Reason: %s\n", unused.Reason)
			writef("  Risk: %s\n", unused.Risk)
			writef("  Recommendation: %s\n", unused.Recommendation)
			writef("\n")
		}
	}

	// Active Topics Section (Summary)
	if len(result.ActiveTopics) > 0 {
		writef("Active Topics (With Consumer Groups)\n")
		writef("=====================================\n\n")

		// Sort by name
		sortedActive := make([]*ActiveTopic, len(result.ActiveTopics))
		copy(sortedActive, result.ActiveTopics)
		sort.Slice(sortedActive, func(i, j int) bool {
			return sortedActive[i].Name < sortedActive[j].Name
		})

		for _, active := range sortedActive {
			writef("[ACTIVE] %s\n", active.Name)
			writef("  Partitions: %d, Replication: %d\n", active.Partitions, active.ReplicationFactor)
			writef("  Consumer Groups (%d): ", len(active.ConsumerGroups))

			// Show first 3 consumer groups, then indicate if there are more
			if len(active.ConsumerGroups) <= 3 {
				for i, cg := range active.ConsumerGroups {
					if i > 0 {
						writef(", ")
					}
					writef("%s", cg)
				}
			} else {
				for i := 0; i < 3; i++ {
					if i > 0 {
						writef(", ")
					}
					writef("%s", active.ConsumerGroups[i])
				}
				writef(", ... and %d more", len(active.ConsumerGroups)-3)
			}
			writef("\n\n")
		}
	}

	// Recommendations
	if result.UnusedCount > 0 {
		writef("Cleanup Recommendations\n")
		writef("=======================\n\n")
		writef("Found %d unused topics that may be candidates for deletion.\n\n", result.UnusedCount)
		writef("Before deleting any topics:\n")
		writef("  1. Verify with application owners that topics are truly unused\n")
		writef("  2. Check if topics are consumed by external systems not visible here\n")
		writef("  3. Consider archiving topic data before deletion\n")
		writef("  4. Test in a non-production environment first\n")
		writef("\n")
		writef("Risk Levels:\n")
		writef("  - low:    Safe to delete (small topic, no consumers)\n")
		writef("  - medium: Review carefully (larger topic, no consumers)\n")
		writef("  - high:   Do not delete without confirmation\n")
	} else {
		writef("No unused topics detected. All topics have active consumer groups.\n")
	}

	if writeErr != nil {
		return writeErr
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

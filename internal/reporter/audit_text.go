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

	// WO-27: a degraded read makes every unused-topic finding unreliable. Say
	// so before the operator reads a single finding.
	if !result.Reliability.ConsumerGroupsComplete {
		writef("!! INCOMPLETE SCAN — consumer group data could not be fully read.\n")
		writef("!! Unused-topic findings below are UNVERIFIED and must not be acted on.\n")
		for _, readErr := range result.Reliability.ReadErrors {
			writef("!!   %s\n", readErr)
		}
		writef("\n")
	}

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
		writef("  Internal (excluded):        %d\n", result.Summary.InternalTopics)
		// Round 2: the hold-out used to be invisible — topics and their
		// partitions vanished from every total with nothing naming them.
		writef("  Service-managed (held out): %d\n\n", result.Summary.ManagedTopicsHeldOut)

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

		// WO-31: severity ordering is applied at the source so every reporter
		// inherits it. Reapplying the shared comparator here keeps the renderer
		// correct for callers that construct an AuditResult directly; it is the
		// same function, not a second definition of the ordering.
		sortedUnused := make([]*UnusedTopic, len(result.UnusedTopics))
		copy(sortedUnused, result.UnusedTopics)
		SortUnusedTopicsBySeverity(sortedUnused)

		for _, unused := range sortedUnused {
			writef("[UNUSED] %s\n", unused.Name)
			writef("  Partitions: %d, Replication: %d\n", unused.Partitions, unused.ReplicationFactor)
			if unused.ManagedBy != "" {
				writef("  Managed By: %s\n", unused.ManagedBy)
			}

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

	// Service-Managed Topics Section
	//
	// Round 2: these were moved out of the unused list because a backing topic
	// having no consumer group is its steady state, not a finding. They must
	// still be VISIBLE — silently omitting them would hide, for example, a
	// Schema Registry topic the operator may want to know about.
	if len(result.ManagedTopics) > 0 {
		writef("Service-Managed Topics (not cleanup candidates)\n")
		writef("================================================\n\n")

		for _, managed := range result.ManagedTopics {
			writef("[MANAGED] %s\n", managed.Name)
			writef("  Owner: %s\n", managed.ManagedBy)
			writef("  Partitions: %d, Replication: %d\n", managed.Partitions, managed.ReplicationFactor)
			writef("  %s\n\n", managed.Recommendation)
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

// RiskLevel converts risk string to numeric value for sorting.
//
// WO-31: exported so severity ordering has a single owner — the audit result
// builder — instead of being re-implemented per reporter.
func RiskLevel(risk string) int {
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

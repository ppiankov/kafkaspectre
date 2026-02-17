package reporter

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
)

// CheckTextReporter writes check results in human-readable text.
type CheckTextReporter struct {
	writer io.Writer
}

// NewCheckTextReporter creates a text reporter for check results.
func NewCheckTextReporter(w io.Writer) *CheckTextReporter {
	return &CheckTextReporter{writer: w}
}

// GenerateCheck emits a text report for check results.
func (r *CheckTextReporter) GenerateCheck(ctx context.Context, result *CheckResult) error {
	var writeErr error
	writef := func(format string, args ...any) {
		if writeErr != nil {
			return
		}
		_, writeErr = fmt.Fprintf(r.writer, format, args...)
	}

	writef("Kafka Topic Check Report\n")
	writef("========================\n\n")

	if result.Summary != nil {
		summary := result.Summary
		writef("Summary:\n")
		writef("  Repo Path:              %s\n", summary.RepoPath)
		writef("  Files Scanned:          %d\n", summary.FilesScanned)
		writef("  Topics In Repo:         %d\n", summary.RepoTopics)
		writef("  Topics In Cluster:      %d\n", summary.ClusterTopics)
		writef("  OK:                     %d\n", summary.OKCount)
		writef("  MISSING_IN_CLUSTER:     %d\n", summary.MissingInClusterCount)
		writef("  UNREFERENCED_IN_REPO:   %d\n", summary.UnreferencedInRepoCount)
		writef("  UNUSED:                 %d\n", summary.UnusedCount)
		writef("  Total Findings:         %d\n\n", summary.TotalFindings)
	}

	if len(result.Findings) == 0 {
		writef("No topic findings detected.\n")
		return writeErr
	}

	orderedStatuses := []CheckStatus{
		CheckStatusMissingInCluster,
		CheckStatusUnused,
		CheckStatusUnreferencedInRepo,
		CheckStatusOK,
	}

	for _, status := range orderedStatuses {
		group := filterCheckFindingsByStatus(result.Findings, status)
		if len(group) == 0 {
			continue
		}

		writef("%s (%d)\n", status, len(group))
		writef("%s\n\n", strings.Repeat("-", len(status)+5))

		sort.Slice(group, func(i, j int) bool {
			return group[i].Topic < group[j].Topic
		})

		for _, finding := range group {
			writef("[%s] %s\n", finding.Status, finding.Topic)
			if finding.Reason != "" {
				writef("  Reason: %s\n", finding.Reason)
			}
			if len(finding.ConsumerGroups) > 0 {
				writef("  Consumer Groups: %s\n", strings.Join(finding.ConsumerGroups, ", "))
			}
			if len(finding.References) > 0 {
				writef("  References:\n")
				limit := len(finding.References)
				if limit > 5 {
					limit = 5
				}
				for i := 0; i < limit; i++ {
					ref := finding.References[i]
					if ref.Line > 0 {
						writef("    - %s:%d (%s)\n", ref.File, ref.Line, ref.Source)
					} else {
						writef("    - %s (%s)\n", ref.File, ref.Source)
					}
				}
				if len(finding.References) > limit {
					writef("    - ... and %d more\n", len(finding.References)-limit)
				}
			}
			writef("\n")
		}
	}

	return writeErr
}

func filterCheckFindingsByStatus(findings []*CheckFinding, status CheckStatus) []*CheckFinding {
	out := make([]*CheckFinding, 0)
	for _, finding := range findings {
		if finding.Status == status {
			out = append(out, finding)
		}
	}
	return out
}

package main

import (
	"testing"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
	"github.com/ppiankov/kafkaspectre/internal/reporter"
)

// WO-46: a degraded scan (incomplete consumer-group read) must exit 4, not 6
// (findings) or 0 (success). A CI gate keying on exit codes must be able to
// tell "could not read" from "found something to delete."
func TestDegradedScanExitCode(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"orders": topic("orders", 1, 1),
		},
		ConsumerGroups:          map[string]*kafka.ConsumerGroupInfo{},
		ConsumerGroupReadErrors: []string{"describe consumer groups: broker unreachable"},
	}

	result := buildAuditResult(metadata, false, nil)
	if result.Reliability.ConsumerGroupsComplete {
		t.Fatal("expected incomplete consumer-group read")
	}

	// The DegradedScanError must take precedence over FindingsError.
	if result.UnusedCount > 0 {
		de := &DegradedScanError{FindingsCount: result.UnusedCount}
		if got := classifyError(de); got != ExitDegraded {
			t.Fatalf("classifyError(DegradedScanError) = %d, want %d", got, ExitDegraded)
		}
	}

	// DegradedScanError beats FindingsError even when both would apply.
	de := &DegradedScanError{FindingsCount: 10}
	if got := classifyError(de); got != ExitDegraded {
		t.Fatalf("classifyError(DegradedScanError) = %d, want %d", got, ExitDegraded)
	}

	// FindingsError still works when the scan is NOT degraded.
	fe := &FindingsError{Count: 10}
	if got := classifyError(fe); got != ExitFindings {
		t.Fatalf("classifyError(FindingsError) = %d, want %d", got, ExitFindings)
	}

	// A clean scan with no findings is still success.
	if got := classifyError(nil); got != ExitSuccess {
		t.Fatalf("classifyError(nil) = %d, want %d", got, ExitSuccess)
	}
}

// WO-46: a clean scan (complete consumer-group read) with findings must still
// exit 6, not 4.
func TestCleanScanWithFindingsExitCode(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"orders": topic("orders", 1, 1),
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
	}

	result := buildAuditResult(metadata, false, nil)
	if !result.Reliability.ConsumerGroupsComplete {
		t.Fatal("expected complete consumer-group read")
	}
	if result.UnusedCount == 0 {
		t.Fatal("expected at least one unused finding")
	}

	fe := &FindingsError{Count: result.UnusedCount}
	if got := classifyError(fe); got != ExitFindings {
		t.Fatalf("classifyError(FindingsError) = %d, want %d", got, ExitFindings)
	}
}

// WO-46: the DegradedScanError carries the finding count so the operator knows
// how many findings are unverified.
func TestDegradedScanErrorMessage(t *testing.T) {
	de := &DegradedScanError{FindingsCount: 42}
	want := "scan incomplete; 42 unverified findings"
	if de.Error() != want {
		t.Fatalf("DegradedScanError.Error() = %q, want %q", de.Error(), want)
	}
}

// WO-46: the check path must also return DegradedScanError for incomplete reads.
func TestCheckDegradedScanClassification(t *testing.T) {
	de := &DegradedScanError{FindingsCount: 5}
	if got := classifyError(de); got != ExitDegraded {
		t.Fatalf("classifyError(DegradedScanError from check) = %d, want %d", got, ExitDegraded)
	}
}

// WO-45: verify the degraded scan data flows correctly from buildAuditResult.
func TestDegradedScanReliabilityFromBuildAuditResult(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"orders":   topic("orders", 1, 1),
			"payments": topic("payments", 12, 3),
		},
		ConsumerGroups:          map[string]*kafka.ConsumerGroupInfo{},
		ConsumerGroupReadErrors: []string{"fetch offsets: timeout"},
	}

	result := buildAuditResult(metadata, false, nil)

	if result.Reliability.ConsumerGroupsComplete {
		t.Fatal("expected degraded reliability")
	}
	if len(result.Reliability.ReadErrors) != 1 {
		t.Fatalf("expected 1 read error, got %d", len(result.Reliability.ReadErrors))
	}

	// The cleanup list must be empty on a degraded scan.
	if len(result.Summary.RecommendedCleanup) != 0 {
		t.Fatalf("degraded scan published cleanup list: %v", result.Summary.RecommendedCleanup)
	}

	// No finding should carry a delete recommendation.
	for _, u := range result.UnusedTopics {
		if u.Recommendation != doNotActAdvice {
			t.Errorf("topic %q has recommendation %q, want do-not-act advice", u.Name, u.Recommendation)
		}
	}

	// Verify exit-code flow: DegradedScanError takes precedence.
	_ = reporter.SortUnusedTopicsBySeverity // keep import alive
	de := &DegradedScanError{FindingsCount: result.UnusedCount}
	if got := classifyError(de); got != ExitDegraded {
		t.Fatalf("degraded scan exit code = %d, want %d", got, ExitDegraded)
	}
}

package main

import (
	"errors"
	"testing"
	"time"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
	"github.com/ppiankov/kafkaspectre/internal/scanner"
)

// WO-46: a degraded scan must return DegradedScanError from the ACTUAL
// classifyAuditResult path, not just from a hand-constructed error. This kills
// the mutation where the DegradedScanError return is removed from runAudit.
func TestClassifyAuditResultDegradedScan(t *testing.T) {
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
	err := classifyAuditResult(result)

	var de *DegradedScanError
	if !errors.As(err, &de) {
		t.Fatalf("classifyAuditResult on a degraded scan returned %T, want *DegradedScanError", err)
	}
	if de.FindingsCount != result.UnusedCount {
		t.Fatalf("DegradedScanError.FindingsCount = %d, want %d", de.FindingsCount, result.UnusedCount)
	}
	if got := classifyError(err); got != ExitDegraded {
		t.Fatalf("classifyError(DegradedScanError) = %d, want %d", got, ExitDegraded)
	}
}

// WO-46: a clean scan with findings returns FindingsError, not DegradedScanError.
func TestClassifyAuditResultCleanWithFindings(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers:        []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics:         map[string]*kafka.TopicInfo{"orders": topic("orders", 1, 1)},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
	}

	result := buildAuditResult(metadata, false, nil)
	err := classifyAuditResult(result)

	var fe *FindingsError
	if !errors.As(err, &fe) {
		t.Fatalf("classifyAuditResult on a clean scan with findings returned %T, want *FindingsError", err)
	}
	if got := classifyError(err); got != ExitFindings {
		t.Fatalf("classifyError(FindingsError) = %d, want %d", got, ExitFindings)
	}
}

// WO-46: a clean scan with no findings returns nil.
func TestClassifyAuditResultCleanNoFindings(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"active": topic("active", 1, 1),
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
			"cg": {GroupID: "cg", State: "Stable", Topics: []string{"active"}},
		},
	}

	result := buildAuditResult(metadata, false, nil)
	if err := classifyAuditResult(result); err != nil {
		t.Fatalf("classifyAuditResult on a clean scan with no findings returned %v, want nil", err)
	}
}

// WO-46: degraded takes precedence over findings even when both would apply.
func TestDegradedPrecedence(t *testing.T) {
	de := &DegradedScanError{FindingsCount: 10}
	fe := &FindingsError{Count: 10}

	if got := classifyError(de); got != ExitDegraded {
		t.Fatalf("DegradedScanError classified as %d, want %d", got, ExitDegraded)
	}
	if got := classifyError(fe); got != ExitFindings {
		t.Fatalf("FindingsError classified as %d, want %d", got, ExitFindings)
	}
	if got := classifyError(nil); got != ExitSuccess {
		t.Fatalf("nil classified as %d, want %d", got, ExitSuccess)
	}
}

// WO-46: the check path returns DegradedScanError from the ACTUAL path.
func TestClassifyCheckResultDegraded(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{{ID: 1, Host: "b1", Port: 9092}},
		Topics: map[string]*kafka.TopicInfo{
			"orders": topic("orders", 1, 1),
		},
		ConsumerGroups:          map[string]*kafka.ConsumerGroupInfo{},
		ConsumerGroupReadErrors: []string{"fetch offsets: timeout"},
	}
	scanResult := &scanner.Result{
		RepoPath: "/repo",
		Topics:   map[string]*scanner.TopicReference{"orders": {Topic: "orders"}},
	}

	result := buildCheckResult(scanResult, metadata, false, nil)
	err := classifyCheckResult(result)

	var de *DegradedScanError
	if !errors.As(err, &de) {
		t.Fatalf("classifyCheckResult on a degraded scan returned %T, want *DegradedScanError", err)
	}
	if got := classifyError(err); got != ExitDegraded {
		t.Fatalf("classifyError = %d, want %d", got, ExitDegraded)
	}
}

// WO-46: DegradedScanError message format.
func TestDegradedScanErrorMessage(t *testing.T) {
	de := &DegradedScanError{FindingsCount: 42}
	want := "scan incomplete; 42 unverified findings"
	if de.Error() != want {
		t.Fatalf("Error() = %q, want %q", de.Error(), want)
	}
}

// WO-45: the default timeout is pinned to a literal so a revert from 60s to 10s
// is caught. The old self-referential test compared the constant to itself.
func TestDefaultTimeoutValue(t *testing.T) {
	if defaultQueryTimeout != 60*time.Second {
		t.Fatalf("defaultQueryTimeout = %v, want 60s (WO-45: must cover 200+ group clusters)", defaultQueryTimeout)
	}
}

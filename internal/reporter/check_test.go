package reporter

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckJSONReporterGenerateCheck(t *testing.T) {
	buf := &bytes.Buffer{}
	reporter := NewCheckJSONReporter(buf, false)
	result := sampleCheckResult()

	if err := reporter.GenerateCheck(context.Background(), result); err != nil {
		t.Fatalf("GenerateCheck error: %v", err)
	}

	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatalf("expected trailing newline")
	}

	var decoded CheckResult
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &decoded); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	if decoded.Summary == nil || decoded.Summary.RepoPath != "./my-app" {
		t.Fatalf("summary mismatch: %+v", decoded.Summary)
	}
	if len(decoded.Findings) != 4 {
		t.Fatalf("findings count = %d, want 4", len(decoded.Findings))
	}
}

func TestCheckTextReporterGenerateCheck(t *testing.T) {
	buf := &bytes.Buffer{}
	reporter := NewCheckTextReporter(buf)
	result := sampleCheckResult()

	if err := reporter.GenerateCheck(context.Background(), result); err != nil {
		t.Fatalf("GenerateCheck error: %v", err)
	}

	output := buf.String()
	wantContains := []string{
		"Kafka Topic Check Report",
		"Repo Path:              ./my-app",
		"[MISSING_IN_CLUSTER] orders.missing",
		"[UNUSED] stale.topic",
		"[UNREFERENCED_IN_REPO] shared.topic",
		"[OK] orders.events",
		"src/config.yaml:12 (yaml_json)",
	}

	for _, want := range wantContains {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q", want)
		}
	}
}

func sampleCheckResult() *CheckResult {
	return &CheckResult{
		Summary: &CheckSummary{
			RepoPath:                "./my-app",
			FilesScanned:            11,
			RepoTopics:              3,
			ClusterTopics:           3,
			TotalFindings:           4,
			OKCount:                 1,
			MissingInClusterCount:   1,
			UnreferencedInRepoCount: 1,
			UnusedCount:             1,
		},
		Findings: []*CheckFinding{
			{
				Topic:            "orders.events",
				Status:           CheckStatusOK,
				ReferencedInRepo: true,
				InCluster:        true,
				ConsumerGroups:   []string{"orders-cg"},
				References: []CheckReference{
					{File: "src/config.yaml", Line: 12, Source: "yaml_json"},
				},
				Reason: "topic exists in cluster and has active consumers",
			},
			{
				Topic:            "orders.missing",
				Status:           CheckStatusMissingInCluster,
				ReferencedInRepo: true,
				InCluster:        false,
				References: []CheckReference{
					{File: "src/config.yaml", Line: 14, Source: "yaml_json"},
				},
				Reason: "topic is referenced in code but does not exist in cluster",
			},
			{
				Topic:            "shared.topic",
				Status:           CheckStatusUnreferencedInRepo,
				ReferencedInRepo: false,
				InCluster:        true,
				ConsumerGroups:   []string{"shared-cg"},
				Reason:           "topic exists in cluster with consumers but was not found in repository",
			},
			{
				Topic:            "stale.topic",
				Status:           CheckStatusUnused,
				ReferencedInRepo: false,
				InCluster:        true,
				Reason:           "topic exists in cluster but has no active consumer groups",
			},
		},
	}
}

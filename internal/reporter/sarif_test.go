package reporter

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSARIFReporterGenerateCheck(t *testing.T) {
	buf := &bytes.Buffer{}
	reporter := NewSARIFReporter(buf, false)

	if err := reporter.GenerateCheck(context.Background(), sampleCheckResult()); err != nil {
		t.Fatalf("GenerateCheck error: %v", err)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatalf("expected trailing newline")
	}

	var output sarifReport
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	if output.Version != sarifVersion {
		t.Fatalf("version = %q, want %q", output.Version, sarifVersion)
	}
	if output.Schema != sarifSchema {
		t.Fatalf("schema = %q, want %q", output.Schema, sarifSchema)
	}
	if len(output.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(output.Runs))
	}

	run := output.Runs[0]
	if run.Tool.Driver.Name != sarifToolName {
		t.Fatalf("tool name = %q, want %q", run.Tool.Driver.Name, sarifToolName)
	}
	if len(run.Results) != 3 {
		t.Fatalf("results = %d, want 3 (OK findings should be omitted)", len(run.Results))
	}

	if _, ok := run.OriginalURIBaseIDs[sarifSrcRootBaseID]; !ok {
		t.Fatalf("missing %s in originalUriBaseIds", sarifSrcRootBaseID)
	}

	wantRuleIDs := map[string]bool{
		sarifRuleIDMissingInCluster:   false,
		sarifRuleIDUnusedTopic:        false,
		sarifRuleIDUnreferencedInRepo: false,
	}
	for _, rule := range run.Tool.Driver.Rules {
		if _, ok := wantRuleIDs[rule.ID]; ok {
			wantRuleIDs[rule.ID] = true
		}
	}
	for ruleID, found := range wantRuleIDs {
		if !found {
			t.Fatalf("expected rule %q in tool driver rules", ruleID)
		}
	}

	resultsByRule := make(map[string]sarifResult, len(run.Results))
	for _, result := range run.Results {
		resultsByRule[result.RuleID] = result
	}

	missing := resultsByRule[sarifRuleIDMissingInCluster]
	if missing.Level != "error" {
		t.Fatalf("missing-in-cluster level = %q, want error", missing.Level)
	}
	if len(missing.Locations) != 1 {
		t.Fatalf("missing-in-cluster locations = %d, want 1", len(missing.Locations))
	}
	location := missing.Locations[0]
	if location.PhysicalLocation.ArtifactLocation.URI != "src/config.yaml" {
		t.Fatalf("location uri = %q, want src/config.yaml", location.PhysicalLocation.ArtifactLocation.URI)
	}
	if location.PhysicalLocation.Region == nil || location.PhysicalLocation.Region.StartLine != 14 {
		t.Fatalf("location line mismatch")
	}

	if resultsByRule[sarifRuleIDUnusedTopic].Level != "warning" {
		t.Fatalf("unused-topic level = %q, want warning", resultsByRule[sarifRuleIDUnusedTopic].Level)
	}
	if resultsByRule[sarifRuleIDUnreferencedInRepo].Level != "warning" {
		t.Fatalf("unreferenced-in-repo level = %q, want warning", resultsByRule[sarifRuleIDUnreferencedInRepo].Level)
	}
}

func TestSARIFReporterGenerateAudit(t *testing.T) {
	buf := &bytes.Buffer{}
	reporter := NewSARIFReporter(buf, true)

	result := &AuditResult{
		UnusedTopics: []*UnusedTopic{
			{Name: "topic-high", Risk: "high", Reason: "no consumers", Recommendation: "investigate", Partitions: 12, ReplicationFactor: 3},
			{Name: "topic-medium", Risk: "medium", Reason: "no consumers", Recommendation: "review", Partitions: 4, ReplicationFactor: 2},
			{Name: "topic-low", Risk: "low", Reason: "no consumers", Recommendation: "delete", Partitions: 1, ReplicationFactor: 1},
		},
	}

	if err := reporter.GenerateAudit(context.Background(), result); err != nil {
		t.Fatalf("GenerateAudit error: %v", err)
	}
	if !strings.Contains(buf.String(), "\n  \"version\"") {
		t.Fatalf("expected pretty-printed SARIF output")
	}

	var output sarifReport
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(output.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(output.Runs))
	}

	run := output.Runs[0]
	if len(run.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(run.Results))
	}

	resultsByRule := make(map[string]sarifResult, len(run.Results))
	for _, entry := range run.Results {
		resultsByRule[entry.RuleID] = entry
	}

	if resultsByRule[sarifRuleIDHighRiskTopic].Level != "error" {
		t.Fatalf("high-risk level = %q, want error", resultsByRule[sarifRuleIDHighRiskTopic].Level)
	}
	if resultsByRule[sarifRuleIDMediumRiskTopic].Level != "warning" {
		t.Fatalf("medium-risk level = %q, want warning", resultsByRule[sarifRuleIDMediumRiskTopic].Level)
	}
	if resultsByRule[sarifRuleIDLowRiskTopic].Level != "note" {
		t.Fatalf("low-risk level = %q, want note", resultsByRule[sarifRuleIDLowRiskTopic].Level)
	}
}

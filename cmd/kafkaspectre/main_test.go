package main

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/kafkaspectre/internal/config"
	"github.com/ppiankov/kafkaspectre/internal/kafka"
	"github.com/ppiankov/kafkaspectre/internal/reporter"
	"github.com/ppiankov/kafkaspectre/internal/scanner"
	"github.com/spf13/cobra"
)

func TestBuildConsumersByTopic(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
			"cg-z": {
				GroupID: "cg-z",
				Topics:  []string{"topic-b", "topic-a", "topic-a"},
			},
			"cg-a": {
				GroupID: "cg-a",
				Topics:  []string{"topic-a"},
			},
			"cg-m": {
				GroupID: "cg-m",
				Topics:  []string{"topic-a", "topic-c"},
			},
		},
	}

	got := buildConsumersByTopic(metadata)
	want := map[string][]string{
		"topic-a": {"cg-a", "cg-m", "cg-z"},
		"topic-b": {"cg-z"},
		"topic-c": {"cg-m"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildConsumersByTopic() = %#v, want %#v", got, want)
	}
}

func TestBuildAuditResult(t *testing.T) {
	newMetadata := func() *kafka.ClusterMetadata {
		return &kafka.ClusterMetadata{
			Brokers: []kafka.BrokerInfo{
				{ID: 1, Host: "broker-1", Port: 9092},
			},
			Topics: map[string]*kafka.TopicInfo{
				"active-topic": {
					Name:              "active-topic",
					Partitions:        3,
					ReplicationFactor: 2,
					Config:            map[string]string{"retention.ms": "3600000"},
				},
				"low-topic": {
					Name:              "low-topic",
					Partitions:        1,
					ReplicationFactor: 1,
					Config:            map[string]string{"retention.ms": "60000"},
				},
				"medium-topic": {
					Name:              "medium-topic",
					Partitions:        2,
					ReplicationFactor: 1,
					Config:            map[string]string{"retention.ms": "60000"},
				},
				"high-topic": {
					Name:              "high-topic",
					Partitions:        1,
					ReplicationFactor: 3,
					Config:            map[string]string{"retention.ms": "60000"},
				},
				"__internal": {
					Name:              "__internal",
					Internal:          true,
					Partitions:        5,
					ReplicationFactor: 1,
				},
			},
			ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
				"cg-1": {GroupID: "cg-1", Topics: []string{"active-topic"}},
				"cg-2": {GroupID: "cg-2", Topics: []string{"active-topic"}},
			},
		}
	}

	t.Run("exclude-internal", func(t *testing.T) {
		result := buildAuditResult(newMetadata(), true, nil)

		if result.TotalTopics != 4 || result.InternalCount != 1 {
			t.Fatalf("topic counts mismatch: total=%d internal=%d", result.TotalTopics, result.InternalCount)
		}
		if result.ActiveCount != 1 || result.UnusedCount != 3 {
			t.Fatalf("active/unused mismatch: active=%d unused=%d", result.ActiveCount, result.UnusedCount)
		}

		if result.Summary.InternalTopics != 1 {
			t.Fatalf("summary internal topics = %d, want 1", result.Summary.InternalTopics)
		}
		if result.Summary.ClusterName != "broker-1" {
			t.Fatalf("cluster name = %q, want %q", result.Summary.ClusterName, "broker-1")
		}
		if result.Summary.TotalPartitions != 7 || result.Summary.UnusedPartitions != 4 || result.Summary.ActivePartitions != 3 {
			t.Fatalf("partition counts mismatch: %+v", result.Summary)
		}
		if result.Summary.HighRiskCount != 1 || result.Summary.MediumRiskCount != 1 || result.Summary.LowRiskCount != 1 {
			t.Fatalf("risk counts mismatch: high=%d medium=%d low=%d", result.Summary.HighRiskCount, result.Summary.MediumRiskCount, result.Summary.LowRiskCount)
		}
		if !approxEqual(result.Summary.UnusedPercentage, 75.0) {
			t.Fatalf("unused percentage = %f, want 75.0", result.Summary.UnusedPercentage)
		}
		if !approxEqual(result.Summary.UnusedPartitionsPercent, 57.14285714285714) {
			t.Fatalf("unused partition percent = %f, want 57.142857...", result.Summary.UnusedPartitionsPercent)
		}
		if result.Summary.ClusterHealthScore != "poor" {
			t.Fatalf("cluster health score = %q, want %q", result.Summary.ClusterHealthScore, "poor")
		}
		if result.Summary.PotentialSavingsInfo != "3 unused topics representing 4 partitions (57.1% of total partitions)" {
			t.Fatalf("potential savings info = %q", result.Summary.PotentialSavingsInfo)
		}

		if got, want := result.Summary.RecommendedCleanup, []string{"low-topic", "medium-topic", "high-topic"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("recommended cleanup = %v, want %v", got, want)
		}
		if got, want := unusedNames(result.UnusedTopics), []string{"high-topic", "low-topic", "medium-topic"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("unused topic order = %v, want %v", got, want)
		}
		if len(result.ActiveTopics) != 1 || result.ActiveTopics[0].ConsumerCount != 2 {
			t.Fatalf("active topics mismatch: %+v", result.ActiveTopics)
		}
	})

	t.Run("include-internal", func(t *testing.T) {
		result := buildAuditResult(newMetadata(), false, nil)

		if result.TotalTopics != 5 || result.InternalCount != 1 {
			t.Fatalf("topic counts mismatch: total=%d internal=%d", result.TotalTopics, result.InternalCount)
		}
		if result.ActiveCount != 1 || result.UnusedCount != 4 {
			t.Fatalf("active/unused mismatch: active=%d unused=%d", result.ActiveCount, result.UnusedCount)
		}

		if result.Summary.InternalTopics != 0 {
			t.Fatalf("summary internal topics = %d, want 0 when not excluded", result.Summary.InternalTopics)
		}
		if result.Summary.TotalPartitions != 12 || result.Summary.UnusedPartitions != 9 {
			t.Fatalf("partition counts mismatch: total=%d unused=%d", result.Summary.TotalPartitions, result.Summary.UnusedPartitions)
		}
		if !approxEqual(result.Summary.UnusedPercentage, 80.0) {
			t.Fatalf("unused percentage = %f, want 80.0", result.Summary.UnusedPercentage)
		}
		if result.Summary.ClusterHealthScore != "critical" {
			t.Fatalf("cluster health score = %q, want %q", result.Summary.ClusterHealthScore, "critical")
		}

		if got, want := result.Summary.RecommendedCleanup, []string{"low-topic", "__internal", "medium-topic", "high-topic"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("recommended cleanup = %v, want %v", got, want)
		}
	})
}

func TestBuildAuditResultExcludePatterns(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Topics: map[string]*kafka.TopicInfo{
			"keep-active": {
				Name:              "keep-active",
				Partitions:        2,
				ReplicationFactor: 1,
			},
			"keep-unused": {
				Name:              "keep-unused",
				Partitions:        1,
				ReplicationFactor: 1,
			},
			"skip-unused": {
				Name:              "skip-unused",
				Partitions:        3,
				ReplicationFactor: 1,
			},
			"__internal": {
				Name:       "__internal",
				Internal:   true,
				Partitions: 1,
			},
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
			"cg": {GroupID: "cg", Topics: []string{"keep-active"}},
		},
	}

	result := buildAuditResult(metadata, false, []string{"skip-*", "__*"})

	if result.TotalTopics != 2 || result.ActiveCount != 1 || result.UnusedCount != 1 {
		t.Fatalf("unexpected counts: total=%d active=%d unused=%d", result.TotalTopics, result.ActiveCount, result.UnusedCount)
	}
	if got := unusedNames(result.UnusedTopics); !reflect.DeepEqual(got, []string{"keep-unused"}) {
		t.Fatalf("unused topics = %v, want [keep-unused]", got)
	}
}

func TestBuildCheckResult(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Topics: map[string]*kafka.TopicInfo{
			"orders.events": {
				Name:              "orders.events",
				Partitions:        3,
				ReplicationFactor: 2,
			},
			"shared.topic": {
				Name:              "shared.topic",
				Partitions:        1,
				ReplicationFactor: 1,
			},
			"stale.topic": {
				Name:              "stale.topic",
				Partitions:        1,
				ReplicationFactor: 1,
			},
			"__internal": {
				Name:              "__internal",
				Internal:          true,
				Partitions:        1,
				ReplicationFactor: 1,
			},
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
			"orders-cg": {GroupID: "orders-cg", Topics: []string{"orders.events"}},
			"shared-cg": {GroupID: "shared-cg", Topics: []string{"shared.topic"}},
		},
	}

	scanResult := &scanner.Result{
		RepoPath:     "/tmp/my-app",
		FilesScanned: 5,
		Topics: map[string]*scanner.TopicReference{
			"orders.events": {
				Topic: "orders.events",
				Occurrences: []scanner.Reference{
					{Topic: "orders.events", File: "config/app.yaml", Line: 12, Source: scanner.SourceYAMLJSON},
				},
			},
			"orders.missing": {
				Topic: "orders.missing",
				Occurrences: []scanner.Reference{
					{Topic: "orders.missing", File: ".env", Line: 1, Source: scanner.SourceEnv},
				},
			},
			"stale.topic": {
				Topic: "stale.topic",
				Occurrences: []scanner.Reference{
					{Topic: "stale.topic", File: "src/main.go", Line: 8, Source: scanner.SourceRegex},
				},
			},
		},
	}

	result := buildCheckResult(scanResult, metadata, true, nil)
	if result.Summary == nil {
		t.Fatalf("expected summary")
	}

	if result.Summary.FilesScanned != 5 || result.Summary.RepoTopics != 3 || result.Summary.ClusterTopics != 3 {
		t.Fatalf("summary mismatch: %+v", result.Summary)
	}
	if result.Summary.TotalFindings != 4 {
		t.Fatalf("total findings = %d, want 4", result.Summary.TotalFindings)
	}
	if result.Summary.OKCount != 1 || result.Summary.MissingInClusterCount != 1 || result.Summary.UnusedCount != 1 || result.Summary.UnreferencedInRepoCount != 1 {
		t.Fatalf("status counts mismatch: %+v", result.Summary)
	}

	if got, want := findingTopics(result.Findings), []string{"orders.missing", "stale.topic", "shared.topic", "orders.events"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("findings order = %v, want %v", got, want)
	}

	byTopic := make(map[string]*reporter.CheckFinding, len(result.Findings))
	for _, finding := range result.Findings {
		byTopic[finding.Topic] = finding
	}

	if got := byTopic["orders.missing"]; got == nil || got.Status != reporter.CheckStatusMissingInCluster || !got.ReferencedInRepo || got.InCluster {
		t.Fatalf("orders.missing mismatch: %+v", got)
	}
	if got := byTopic["stale.topic"]; got == nil || got.Status != reporter.CheckStatusUnused || !got.ReferencedInRepo || !got.InCluster {
		t.Fatalf("stale.topic mismatch: %+v", got)
	}
	if got := byTopic["shared.topic"]; got == nil || got.Status != reporter.CheckStatusUnreferencedInRepo || got.ReferencedInRepo || !got.InCluster {
		t.Fatalf("shared.topic mismatch: %+v", got)
	}
	if got := byTopic["orders.events"]; got == nil || got.Status != reporter.CheckStatusOK || !got.ReferencedInRepo || !got.InCluster {
		t.Fatalf("orders.events mismatch: %+v", got)
	}
	if len(byTopic["orders.events"].References) != 1 || byTopic["orders.events"].References[0].File != "config/app.yaml" {
		t.Fatalf("orders.events references mismatch: %+v", byTopic["orders.events"].References)
	}
}

func TestBuildCheckResultExcludePatterns(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Topics: map[string]*kafka.TopicInfo{
			"keep.cluster": {Name: "keep.cluster", Partitions: 1, ReplicationFactor: 1},
			"skip.cluster": {Name: "skip.cluster", Partitions: 1, ReplicationFactor: 1},
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
			"cg-1": {GroupID: "cg-1", Topics: []string{"keep.cluster"}},
			"cg-2": {GroupID: "cg-2", Topics: []string{"skip.cluster"}},
		},
	}
	scanResult := &scanner.Result{
		RepoPath:     "/tmp/repo",
		FilesScanned: 2,
		Topics: map[string]*scanner.TopicReference{
			"keep.repo": {
				Topic:       "keep.repo",
				Occurrences: []scanner.Reference{{Topic: "keep.repo", File: "config.yaml", Line: 1, Source: scanner.SourceYAMLJSON}},
			},
			"skip.repo": {
				Topic:       "skip.repo",
				Occurrences: []scanner.Reference{{Topic: "skip.repo", File: "config.yaml", Line: 2, Source: scanner.SourceYAMLJSON}},
			},
		},
	}

	result := buildCheckResult(scanResult, metadata, false, []string{"skip.*"})
	if result.Summary.RepoTopics != 1 || result.Summary.ClusterTopics != 1 || result.Summary.TotalFindings != 2 {
		t.Fatalf("summary mismatch: %+v", result.Summary)
	}
	for _, finding := range result.Findings {
		if strings.HasPrefix(finding.Topic, "skip.") {
			t.Fatalf("unexpected excluded topic in findings: %q", finding.Topic)
		}
	}
}

func TestNormalizeExcludePatterns(t *testing.T) {
	got, err := normalizeExcludePatterns([]string{"", "  *.dlq  ", "orders.*"})
	if err != nil {
		t.Fatalf("normalizeExcludePatterns() error = %v", err)
	}
	want := []string{"*.dlq", "orders.*"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeExcludePatterns() = %v, want %v", got, want)
	}

	if _, err := normalizeExcludePatterns([]string{"["}); err == nil {
		t.Fatalf("expected invalid pattern error")
	}
}

func TestResolveAuditOptionsFromConfig(t *testing.T) {
	workingDir := t.TempDir()
	withWorkingDir(t, workingDir)
	t.Setenv("HOME", t.TempDir())

	configFile := filepath.Join(workingDir, config.DefaultFileName)
	content := `bootstrap_servers: config:9092
auth_mechanism: SCRAM-SHA-512
exclude_topics:
  - "__*"
  - "*.dlq"
exclude_internal: true
format: json
timeout: 45s
`
	if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resolved, err := resolveAuditOptions(newAuditCmd(), auditOptions{output: "text"})
	if err != nil {
		t.Fatalf("resolveAuditOptions() error = %v", err)
	}

	if resolved.bootstrapServer != "config:9092" {
		t.Fatalf("bootstrapServer = %q", resolved.bootstrapServer)
	}
	if resolved.authMechanism != "SCRAM-SHA-512" {
		t.Fatalf("authMechanism = %q", resolved.authMechanism)
	}
	if resolved.output != "json" {
		t.Fatalf("output = %q, want json", resolved.output)
	}
	if !resolved.excludeInternal {
		t.Fatalf("excludeInternal = false, want true")
	}
	if !reflect.DeepEqual(resolved.excludeTopics, []string{"__*", "*.dlq"}) {
		t.Fatalf("excludeTopics = %v", resolved.excludeTopics)
	}
	if resolved.timeout != 45*time.Second {
		t.Fatalf("timeout = %v, want 45s", resolved.timeout)
	}
}

func TestResolveAuditOptionsFlagsOverrideConfig(t *testing.T) {
	workingDir := t.TempDir()
	withWorkingDir(t, workingDir)
	t.Setenv("HOME", t.TempDir())

	configFile := filepath.Join(workingDir, config.DefaultFileName)
	content := `bootstrap_servers: config:9092
auth_mechanism: SCRAM-SHA-512
exclude_topics: ["__*", "*.dlq"]
exclude_internal: true
format: json
timeout: 45s
`
	if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newAuditCmd()
	if err := cmd.Flags().Set("bootstrap-server", "cli:9092"); err != nil {
		t.Fatalf("set bootstrap-server: %v", err)
	}
	if err := cmd.Flags().Set("output", "sarif"); err != nil {
		t.Fatalf("set output: %v", err)
	}
	if err := cmd.Flags().Set("exclude-internal", "false"); err != nil {
		t.Fatalf("set exclude-internal: %v", err)
	}
	if err := cmd.Flags().Set("exclude-topics", "cli-*"); err != nil {
		t.Fatalf("set exclude-topics: %v", err)
	}
	if err := cmd.Flags().Set("timeout", "3s"); err != nil {
		t.Fatalf("set timeout: %v", err)
	}

	opts := auditOptions{
		bootstrapServer: "cli:9092",
		output:          "sarif",
		excludeInternal: false,
		excludeTopics:   []string{"cli-*"},
		timeout:         3 * time.Second,
	}
	resolved, err := resolveAuditOptions(cmd, opts)
	if err != nil {
		t.Fatalf("resolveAuditOptions() error = %v", err)
	}

	if resolved.bootstrapServer != "cli:9092" {
		t.Fatalf("bootstrapServer = %q, want cli:9092", resolved.bootstrapServer)
	}
	if resolved.output != "sarif" {
		t.Fatalf("output = %q, want sarif", resolved.output)
	}
	if resolved.excludeInternal {
		t.Fatalf("excludeInternal = true, want false")
	}
	if !reflect.DeepEqual(resolved.excludeTopics, []string{"cli-*"}) {
		t.Fatalf("excludeTopics = %v, want [cli-*]", resolved.excludeTopics)
	}
	if resolved.timeout != 3*time.Second {
		t.Fatalf("timeout = %v, want 3s", resolved.timeout)
	}
	// Not set by CLI, should still come from config.
	if resolved.authMechanism != "SCRAM-SHA-512" {
		t.Fatalf("authMechanism = %q, want SCRAM-SHA-512", resolved.authMechanism)
	}
}

func TestClassifyRisk(t *testing.T) {
	cases := []struct {
		name        string
		partitions  int
		replication int
		wantRisk    string
		wantPrio    int
	}{
		{name: "high-partitions", partitions: 10, replication: 1, wantRisk: "high", wantPrio: 3},
		{name: "high-replication", partitions: 1, replication: 3, wantRisk: "high", wantPrio: 3},
		{name: "medium-partitions", partitions: 2, replication: 1, wantRisk: "medium", wantPrio: 2},
		{name: "medium-replication", partitions: 1, replication: 2, wantRisk: "medium", wantPrio: 2},
		{name: "low", partitions: 1, replication: 1, wantRisk: "low", wantPrio: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			topic := &kafka.TopicInfo{Partitions: tc.partitions, ReplicationFactor: tc.replication}
			risk, prio := classifyRisk(topic)
			if risk != tc.wantRisk || prio != tc.wantPrio {
				t.Fatalf("classifyRisk(%+v) = (%q,%d), want (%q,%d)", *topic, risk, prio, tc.wantRisk, tc.wantPrio)
			}
		})
	}
}

func TestRecommendationForRisk(t *testing.T) {
	cases := []struct {
		risk string
		want string
	}{
		{risk: "low", want: "Safe to delete after confirmation"},
		{risk: "medium", want: "Review before deletion"},
		{risk: "high", want: "Investigate before deletion"},
		{risk: "unknown", want: "Review before deletion"},
	}

	for _, tc := range cases {
		if got := recommendationForRisk(tc.risk); got != tc.want {
			t.Fatalf("recommendationForRisk(%q) = %q, want %q", tc.risk, got, tc.want)
		}
	}
}

func TestRecommendedCleanup(t *testing.T) {
	unused := []*reporter.UnusedTopic{
		{Name: "z-low", CleanupPriority: 1, Risk: "low"},
		{Name: "a-low", CleanupPriority: 1, Risk: "low"},
		{Name: "n-low", CleanupPriority: 2, Risk: "low"},
		{Name: "m-high", CleanupPriority: 2, Risk: "high"},
	}

	if got := recommendedCleanup(nil, 5); got != nil {
		t.Fatalf("recommendedCleanup(nil, 5) = %v, want nil", got)
	}
	if got := recommendedCleanup(unused, 0); got != nil {
		t.Fatalf("recommendedCleanup(unused, 0) = %v, want nil", got)
	}

	got := recommendedCleanup(unused, 3)
	want := []string{"a-low", "z-low", "m-high"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recommendedCleanup(unused, 3) = %v, want %v", got, want)
	}
}

func TestClusterHealthScore(t *testing.T) {
	cases := []struct {
		name    string
		percent float64
		want    string
	}{
		{name: "excellent-lower", percent: 0, want: "excellent"},
		{name: "excellent-upper", percent: 10, want: "excellent"},
		{name: "good-upper", percent: 25, want: "good"},
		{name: "fair-upper", percent: 50, want: "fair"},
		{name: "poor-upper", percent: 75, want: "poor"},
		{name: "critical-over", percent: 75.1, want: "critical"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clusterHealthScore(tc.percent); got != tc.want {
				t.Fatalf("clusterHealthScore(%f) = %q, want %q", tc.percent, got, tc.want)
			}
		})
	}
}

func TestPercent(t *testing.T) {
	cases := []struct {
		name        string
		numerator   int
		denominator int
		want        float64
	}{
		{name: "zero-denominator", numerator: 3, denominator: 0, want: 0},
		{name: "zero-numerator", numerator: 0, denominator: 4, want: 0},
		{name: "regular", numerator: 1, denominator: 4, want: 25},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := percent(tc.numerator, tc.denominator); !approxEqual(got, tc.want) {
				t.Fatalf("percent(%d, %d) = %f, want %f", tc.numerator, tc.denominator, got, tc.want)
			}
		})
	}
}

func TestRunAuditValidation(t *testing.T) {
	base := auditOptions{
		bootstrapServer: "localhost:9092",
		output:          "text",
		timeout:         defaultQueryTimeout,
	}

	cases := []struct {
		name    string
		opts    auditOptions
		wantErr string
	}{
		{
			name: "invalid-output",
			opts: auditOptions{
				bootstrapServer: base.bootstrapServer,
				output:          "yaml",
			},
			wantErr: "invalid output format",
		},
		{
			name: "auth-missing-password",
			opts: auditOptions{
				bootstrapServer: base.bootstrapServer,
				output:          base.output,
				authMechanism:   "PLAIN",
				username:        "user",
			},
			wantErr: "requires both --username and --password",
		},
		{
			name: "tls-cert-without-key",
			opts: auditOptions{
				bootstrapServer: base.bootstrapServer,
				output:          base.output,
				tlsCert:         "/tmp/client.crt",
			},
			wantErr: "--tls-cert and --tls-key must be provided together",
		},
		{
			name: "tls-key-without-cert",
			opts: auditOptions{
				bootstrapServer: base.bootstrapServer,
				output:          base.output,
				tlsKey:          "/tmp/client.key",
			},
			wantErr: "--tls-cert and --tls-key must be provided together",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			if opts.timeout == 0 {
				opts.timeout = base.timeout
			}

			err := runAudit(&cobra.Command{}, opts)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestRunCheckValidation(t *testing.T) {
	repoDir := t.TempDir()
	notDirFile := filepath.Join(t.TempDir(), "repo.txt")
	if err := os.WriteFile(notDirFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write notDirFile: %v", err)
	}

	base := checkOptions{
		repo:            repoDir,
		bootstrapServer: "localhost:9092",
		output:          "text",
		timeout:         defaultQueryTimeout,
	}

	cases := []struct {
		name    string
		opts    checkOptions
		wantErr string
	}{
		{
			name: "invalid-output",
			opts: checkOptions{
				repo:            repoDir,
				bootstrapServer: base.bootstrapServer,
				output:          "yaml",
			},
			wantErr: "invalid output format",
		},
		{
			name: "auth-missing-password",
			opts: checkOptions{
				repo:            repoDir,
				bootstrapServer: base.bootstrapServer,
				output:          base.output,
				authMechanism:   "PLAIN",
				username:        "user",
			},
			wantErr: "requires both --username and --password",
		},
		{
			name: "tls-cert-without-key",
			opts: checkOptions{
				repo:            repoDir,
				bootstrapServer: base.bootstrapServer,
				output:          base.output,
				tlsCert:         "/tmp/client.crt",
			},
			wantErr: "--tls-cert and --tls-key must be provided together",
		},
		{
			name: "missing-repo",
			opts: checkOptions{
				repo:            filepath.Join(t.TempDir(), "missing"),
				bootstrapServer: base.bootstrapServer,
				output:          base.output,
			},
			wantErr: "repo path",
		},
		{
			name: "repo-not-directory",
			opts: checkOptions{
				repo:            notDirFile,
				bootstrapServer: base.bootstrapServer,
				output:          base.output,
			},
			wantErr: "not a directory",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			if opts.timeout == 0 {
				opts.timeout = base.timeout
			}

			err := runCheck(&cobra.Command{}, opts)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestNewRootCmdIncludesCheck(t *testing.T) {
	cmd := newRootCmd()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "check" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected root command to include check subcommand")
	}
}

func TestNewRootCmdHasVerboseFlag(t *testing.T) {
	cmd := newRootCmd()

	flag := cmd.PersistentFlags().Lookup("verbose")
	if flag == nil {
		t.Fatalf("expected verbose persistent flag to be defined")
	}
	if flag.Shorthand != "v" {
		t.Fatalf("verbose shorthand = %q, want %q", flag.Shorthand, "v")
	}
}

func TestMetadataStats(t *testing.T) {
	topicCount, partitionCount := metadataStats(nil)
	if topicCount != 0 || partitionCount != 0 {
		t.Fatalf("metadataStats(nil) = (%d, %d), want (0, 0)", topicCount, partitionCount)
	}

	metadata := &kafka.ClusterMetadata{
		Topics: map[string]*kafka.TopicInfo{
			"a": {Name: "a", Partitions: 3},
			"b": {Name: "b", Partitions: 2},
			"c": {Name: "c", Partitions: 1},
		},
	}

	topicCount, partitionCount = metadataStats(metadata)
	if topicCount != 3 || partitionCount != 6 {
		t.Fatalf("metadataStats(metadata) = (%d, %d), want (3, 6)", topicCount, partitionCount)
	}
}

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func findingTopics(findings []*reporter.CheckFinding) []string {
	topics := make([]string, len(findings))
	for i, finding := range findings {
		topics[i] = finding.Topic
	}
	return topics
}

func unusedNames(unused []*reporter.UnusedTopic) []string {
	names := make([]string, len(unused))
	for i, topic := range unused {
		names[i] = topic.Name
	}
	return names
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%q): %v", dir, err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore wd: %v", chdirErr)
		}
	})
}

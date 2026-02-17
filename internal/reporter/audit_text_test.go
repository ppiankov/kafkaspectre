package reporter

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

func TestAuditTextReporterGenerateAudit(t *testing.T) {
	cases := []struct {
		name         string
		result       *AuditResult
		wantContains []string
		wantOrder    [][2]string
	}{
		{
			name: "with-summary",
			result: &AuditResult{
				Summary: &AuditSummary{
					ClusterName:                 "cluster-1",
					TotalBrokers:                2,
					TotalConsumerGroups:         3,
					TotalTopicsIncludingInternal: 5,
					TotalTopics:                 4,
					ActiveTopics:                2,
					UnusedTopics:                2,
					InternalTopics:              1,
					UnusedPercentage:            50.0,
					TotalPartitions:             10,
					ActivePartitions:            6,
					UnusedPartitions:            4,
					UnusedPartitionsPercent:     40.0,
					HighRiskCount:               1,
					MediumRiskCount:             0,
					LowRiskCount:                1,
					ClusterHealthScore:          "B",
					PotentialSavingsInfo:        "none",
				},
				UnusedTopics: []*UnusedTopic{
					{
						Name:              "low-topic",
						Partitions:        1,
						ReplicationFactor: 1,
						RetentionHuman:    "1 days",
						CleanupPolicy:     "delete",
						Reason:            "no consumers",
						Risk:              "low",
						Recommendation:    "delete",
					},
					{
						Name:              "high-topic",
						Partitions:        2,
						ReplicationFactor: 2,
						RetentionHuman:    "2 days",
						CleanupPolicy:     "compact",
						Reason:            "critical",
						Risk:              "high",
						Recommendation:    "review",
					},
				},
				ActiveTopics: []*ActiveTopic{
					{
						Name:              "z-topic",
						Partitions:        1,
						ReplicationFactor: 1,
						ConsumerGroups:    []string{"cg-1", "cg-2"},
					},
					{
						Name:              "a-topic",
						Partitions:        3,
						ReplicationFactor: 2,
						ConsumerGroups:    []string{"cg-1", "cg-2", "cg-3", "cg-4", "cg-5"},
					},
				},
				UnusedCount: 2,
				ActiveCount: 2,
			},
			wantContains: []string{
				"Kafka Cluster Audit Report",
				"Cluster: cluster-1 (2 brokers, 3 consumer groups)",
				"Unused (no consumers):      2 (50.0%)",
				"Unused Topics (No Consumer Groups)",
				"Risk Breakdown:",
				"Cluster Health: B",
				"Potential Savings: none",
				"[UNUSED] high-topic",
				"[UNUSED] low-topic",
				"[ACTIVE] a-topic",
				"[ACTIVE] z-topic",
				"... and 2 more",
				"Cleanup Recommendations",
			},
			wantOrder: [][2]string{
				{"[UNUSED] high-topic", "[UNUSED] low-topic"},
				{"[ACTIVE] a-topic", "[ACTIVE] z-topic"},
			},
		},
		{
			name: "no-unused-summary-nil",
			result: &AuditResult{
				Summary:       nil,
				UnusedTopics:  []*UnusedTopic{},
				ActiveTopics:  []*ActiveTopic{},
				TotalTopics:   3,
				ActiveCount:   2,
				UnusedCount:   0,
				InternalCount: 1,
			},
			wantContains: []string{
				"Total Topics:    3",
				"Active Topics:   2 (with consumers)",
				"Unused Topics:   0 (no consumers)",
				"Internal Topics: 1 (excluded from analysis)",
				"No unused topics detected. All topics have active consumer groups.",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			reporter := NewAuditTextReporter(buf, false)

			if err := reporter.GenerateAudit(context.Background(), tc.result); err != nil {
				t.Fatalf("GenerateAudit error: %v", err)
			}

			output := buf.String()
			for _, want := range tc.wantContains {
				if !strings.Contains(output, want) {
					t.Fatalf("expected output to contain %q", want)
				}
			}
			for _, pair := range tc.wantOrder {
				first := strings.Index(output, pair[0])
				second := strings.Index(output, pair[1])
				if first == -1 || second == -1 || first >= second {
					t.Fatalf("expected %q before %q", pair[0], pair[1])
				}
			}
		})
	}
}

func TestTextReporterGenerate(t *testing.T) {
	lastCommit := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{
			{ID: 1, Host: "broker-a", Port: 9092, Rack: "rack-1"},
			{ID: 2, Host: "broker-b", Port: 9093},
		},
		Topics: map[string]*kafka.TopicInfo{
			"__internal": {Name: "__internal", Internal: true},
			"user-a": {
				Name:              "user-a",
				Partitions:        3,
				ReplicationFactor: 2,
				Config: map[string]string{
					"retention.ms":   "3600000",
					"cleanup.policy": "delete",
				},
			},
			"user-b": {
				Name:              "user-b",
				Partitions:        1,
				ReplicationFactor: 1,
				Config:            map[string]string{},
			},
			"user-c": {
				Name:              "user-c",
				Partitions:        2,
				ReplicationFactor: 3,
				Config:            map[string]string{},
			},
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
			"group-a": {
				GroupID:    "group-a",
				State:      "Stable",
				Members:    2,
				Topics:     []string{"user-a", "user-b"},
				LastCommit: lastCommit,
				Lag: map[string]int64{
					"user-a": 10,
					"user-b": 20,
				},
			},
			"group-b": {
				GroupID: "group-b",
				State:   "Empty",
				Members: 1,
				Topics:  []string{"user-b"},
				Lag:     map[string]int64{},
			},
		},
	}

	cases := []struct {
		name         string
		metadata     *kafka.ClusterMetadata
		wantContains []string
	}{
		{
			name:     "full-report",
			metadata: metadata,
			wantContains: []string{
				"Kafka Cluster Overview",
				"Broker 1: broker-a:9092 (rack: rack-1)",
				"Broker 2: broker-b:9093",
				"Topics: 4 total (3 user, 1 internal)",
				"[Topic] user-a",
				"Retention: 3600000 ms",
				"Cleanup Policy: delete",
				"Consumer Groups: group-a",
				"[Topic] user-b",
				"Consumer Groups: group-a, group-b",
				"[Topic] user-c",
				"Consumer Groups: none",
				"Consumer Groups: 2",
				"[Group] group-a",
				"State: Stable",
				"Members: 2",
				"Topics: user-a, user-b",
				"Total Lag: 30 messages",
				"user-a: 10",
				"user-b: 20",
				"Last Commit: 2024-03-04 05:06:07",
				"[Group] group-b",
				"State: Empty",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			reporter := NewTextReporter(buf, false)
			if err := reporter.Generate(context.Background(), tc.metadata); err != nil {
				t.Fatalf("Generate error: %v", err)
			}

			output := buf.String()
			for _, want := range tc.wantContains {
				if !strings.Contains(output, want) {
					t.Fatalf("expected output to contain %q", want)
				}
			}
		})
	}
}

func TestTextReporterAuditStubs(t *testing.T) {
	cases := []struct {
		name    string
		call    func() error
		wantErr bool
	}{
		{
			name:    "text-reporter-generate-audit",
			call:    func() error { return NewTextReporter(&bytes.Buffer{}, false).GenerateAudit(context.Background(), &AuditResult{}) },
			wantErr: true,
		},
		{
			name:    "audit-text-reporter-generate",
			call:    func() error { return NewAuditTextReporter(&bytes.Buffer{}, false).Generate(context.Background(), &kafka.ClusterMetadata{}) },
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

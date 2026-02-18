package reporter

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

func TestAuditJSONReporterOutput(t *testing.T) {
	fetchedAt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{
			{ID: 1, Host: "broker-a", Port: 9092},
			{ID: 2, Host: "broker-b", Port: 9093},
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
			"cg-1": {GroupID: "cg-1"},
			"cg-2": {GroupID: "cg-2"},
		},
		FetchedAt: fetchedAt,
	}

	summary := &AuditSummary{
		ClusterName:         "cluster-1",
		TotalBrokers:        2,
		TotalConsumerGroups: 2,
	}

	cases := []struct {
		name        string
		pretty      bool
		activeCount int
		active      []*ActiveTopic
		wantActive  bool
	}{
		{
			name:        "includes-active",
			pretty:      true,
			activeCount: 2,
			active: []*ActiveTopic{
				{Name: "active-a", Partitions: 1, ReplicationFactor: 1},
				{Name: "active-b", Partitions: 2, ReplicationFactor: 2},
			},
			wantActive: true,
		},
		{
			name:        "omits-active",
			pretty:      false,
			activeCount: 51,
			active: []*ActiveTopic{
				{Name: "active-a", Partitions: 1, ReplicationFactor: 1},
			},
			wantActive: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			reporter := NewAuditJSONReporter(buf, tc.pretty)
			result := &AuditResult{
				Summary:      summary,
				UnusedTopics: []*UnusedTopic{{Name: "unused-a"}},
				ActiveTopics: tc.active,
				ActiveCount:  tc.activeCount,
				Metadata:     metadata,
			}

			if err := reporter.GenerateAudit(context.Background(), result); err != nil {
				t.Fatalf("GenerateAudit error: %v", err)
			}
			if !strings.HasSuffix(buf.String(), "\n") {
				t.Fatalf("expected trailing newline")
			}

			var output AuditJSONOutput
			if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &output); err != nil {
				t.Fatalf("unmarshal output: %v", err)
			}

			if output.Summary == nil || output.Summary.ClusterName != "cluster-1" {
				t.Fatalf("summary missing or mismatch")
			}
			if output.ClusterMetadata == nil {
				t.Fatalf("cluster metadata missing")
			}
			if output.ClusterMetadata.ConsumerCount != len(metadata.ConsumerGroups) {
				t.Fatalf("consumer count = %d, want %d", output.ClusterMetadata.ConsumerCount, len(metadata.ConsumerGroups))
			}
			if output.ClusterMetadata.FetchedAt != fetchedAt.Format("2006-01-02 15:04:05 MST") {
				t.Fatalf("fetched_at = %q, want %q", output.ClusterMetadata.FetchedAt, fetchedAt.Format("2006-01-02 15:04:05 MST"))
			}
			if len(output.ClusterMetadata.Brokers) != len(metadata.Brokers) {
				t.Fatalf("brokers = %d, want %d", len(output.ClusterMetadata.Brokers), len(metadata.Brokers))
			}

			if tc.wantActive {
				if len(output.ActiveTopics) != len(tc.active) {
					t.Fatalf("active topics = %d, want %d", len(output.ActiveTopics), len(tc.active))
				}
			} else if output.ActiveTopics != nil {
				t.Fatalf("expected active topics to be omitted")
			}
		})
	}
}

func TestJSONReporterGenerate(t *testing.T) {
	cases := []struct {
		name   string
		pretty bool
	}{
		{name: "compact", pretty: false},
		{name: "pretty", pretty: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			metadata := &kafka.ClusterMetadata{
				Brokers: []kafka.BrokerInfo{
					{ID: 1, Host: "broker", Port: 9092},
				},
				Topics: map[string]*kafka.TopicInfo{
					"orders": {Name: "orders", Partitions: 3},
				},
				ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{},
				FetchedAt:      time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC),
			}

			buf := &bytes.Buffer{}
			reporter := NewJSONReporter(buf, tc.pretty)
			if err := reporter.Generate(context.Background(), metadata); err != nil {
				t.Fatalf("Generate error: %v", err)
			}

			var decoded kafka.ClusterMetadata
			if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &decoded); err != nil {
				t.Fatalf("unmarshal output: %v", err)
			}
			if len(decoded.Brokers) != 1 || decoded.Brokers[0].Host != "broker" {
				t.Fatalf("decoded brokers mismatch")
			}
			if len(decoded.Topics) != 1 {
				t.Fatalf("decoded topics mismatch")
			}
		})
	}
}

func TestJSONReporterStubs(t *testing.T) {
	cases := []struct {
		name    string
		call    func() error
		wantErr bool
	}{
		{
			name: "json-reporter-generate-audit",
			call: func() error {
				return NewJSONReporter(&bytes.Buffer{}, false).GenerateAudit(context.Background(), &AuditResult{})
			},
			wantErr: false,
		},
		{
			name: "audit-json-reporter-generate",
			call: func() error {
				return NewAuditJSONReporter(&bytes.Buffer{}, false).Generate(context.Background(), &kafka.ClusterMetadata{})
			},
			wantErr: false,
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

package reporter

import (
	"reflect"
	"testing"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

func TestRiskLevel(t *testing.T) {
	cases := []struct {
		name  string
		risk  string
		want  int
	}{
		{name: "high", risk: "high", want: 3},
		{name: "medium", risk: "medium", want: 2},
		{name: "low", risk: "low", want: 1},
		{name: "unknown", risk: "unknown", want: 0},
		{name: "empty", risk: "", want: 0},
		{name: "case-sensitive", risk: "HIGH", want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := riskLevel(tc.risk); got != tc.want {
				t.Fatalf("riskLevel(%q) = %d, want %d", tc.risk, got, tc.want)
			}
		})
	}
}

func TestFormatRetentionMs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "infinite"},
		{name: "negative", in: "-1", want: "infinite"},
		{name: "invalid", in: "abc", want: "abc"},
		{name: "days-hours", in: "90000000", want: "1 days 1 hours"},
		{name: "days", in: "172800000", want: "2 days"},
		{name: "hours", in: "3600000", want: "1 hours"},
		{name: "minutes", in: "300000", want: "5 minutes"},
		{name: "milliseconds", in: "500", want: "500 ms"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatRetentionMs(tc.in); got != tc.want {
				t.Fatalf("FormatRetentionMs(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFilterInterestingConfig(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]string
		want map[string]string
	}{
		{
			name: "filters-nonimportant",
			in: map[string]string{
				"retention.ms":        "1000",
				"cleanup.policy":      "delete",
				"compression.type":    "lz4",
				"unrelated.setting":   "ignored",
				"min.insync.replicas": "2",
			},
			want: map[string]string{
				"retention.ms":        "1000",
				"cleanup.policy":      "delete",
				"compression.type":    "lz4",
				"min.insync.replicas": "2",
			},
		},
		{
			name: "empty",
			in:   map[string]string{},
			want: map[string]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FilterInterestingConfig(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("FilterInterestingConfig() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestBuildUnusedTopic(t *testing.T) {
	cases := []struct {
		name       string
		config     map[string]string
		wantHuman  string
		wantConfig map[string]string
	}{
		{
			name: "retention-and-interesting",
			config: map[string]string{
				"retention.ms":        "3600000",
				"cleanup.policy":      "delete",
				"min.insync.replicas": "2",
				"segment.bytes":       "1048576",
				"not.interesting":     "skip",
			},
			wantHuman: "1 hours",
			wantConfig: map[string]string{
				"retention.ms":        "3600000",
				"cleanup.policy":      "delete",
				"min.insync.replicas": "2",
				"segment.bytes":       "1048576",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			topic := &kafka.TopicInfo{
				Name:              "orders",
				Partitions:        3,
				ReplicationFactor: 2,
				Config:            tc.config,
			}

			got := BuildUnusedTopic(topic, "no consumers", "review", "medium", 5)
			if got.RetentionHuman != tc.wantHuman {
				t.Fatalf("RetentionHuman = %q, want %q", got.RetentionHuman, tc.wantHuman)
			}
			if got.Risk != "medium" || got.CleanupPriority != 5 {
				t.Fatalf("risk/priority mismatch: %q/%d", got.Risk, got.CleanupPriority)
			}
			if !reflect.DeepEqual(got.InterestingConfig, tc.wantConfig) {
				t.Fatalf("InterestingConfig = %#v, want %#v", got.InterestingConfig, tc.wantConfig)
			}
		})
	}
}

func TestBuildActiveTopicConsumerCount(t *testing.T) {
	cases := []struct {
		name      string
		consumers []string
		wantCount int
	}{
		{name: "none", consumers: nil, wantCount: 0},
		{name: "some", consumers: []string{"cg-1", "cg-2"}, wantCount: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			topic := &kafka.TopicInfo{
				Name:              "payments",
				Partitions:        6,
				ReplicationFactor: 3,
			}
			got := BuildActiveTopic(topic, tc.consumers)
			if got.ConsumerCount != tc.wantCount {
				t.Fatalf("ConsumerCount = %d, want %d", got.ConsumerCount, tc.wantCount)
			}
		})
	}
}

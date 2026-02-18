package reporter

import (
	"reflect"
	"testing"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

func TestRiskLevel(t *testing.T) {
	cases := []struct {
		name string
		risk string
		want int
	}{
		{name: "high", risk: "high", want: 3},
		{name: "medium", risk: "medium", want: 2},
		{name: "low", risk: "low", want: 1},
		{name: "unknown", risk: "unknown", want: 0},
		{name: "empty", risk: "", want: 0},
		{name: "case-sensitive", risk: "HIGH", want: 0},
		{name: "mixed-case", risk: "Medium", want: 0},
		{name: "Low", risk: "Low", want: 0},
		{name: "invalid", risk: "invalid-risk", want: 0},
		{name: "numeric", risk: "1", want: 0},
		{name: "symbols", risk: "high!", want: 0},
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
		{name: "zero", in: "0", want: "0 ms"},
		{name: "large-days", in: "2592000000", want: "30 days"},
		{name: "exact-24h", in: "86400000", want: "1 days"},
		{name: "exact-1h", in: "3600000", want: "1 hours"},
		{name: "exact-1m", in: "60000", want: "1 minutes"},
		{name: "exact-1s", in: "1000", want: "1000 ms"},
		{name: "mixed-days-hours-minutes", in: "93780000", want: "1 days 2 hours"},
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
		{
			name: "all-important-keys",
			in: map[string]string{
				"retention.ms":        "1000",
				"retention.bytes":     "1073741824",
				"cleanup.policy":      "compact,delete",
				"min.insync.replicas": "2",
				"compression.type":    "snappy",
				"max.message.bytes":   "1048588",
				"segment.ms":          "604800000",
				"segment.bytes":       "1073741824",
				"delete.retention.ms": "86400000",
			},
			want: map[string]string{
				"retention.ms":        "1000",
				"retention.bytes":     "1073741824",
				"cleanup.policy":      "compact,delete",
				"min.insync.replicas": "2",
				"compression.type":    "snappy",
				"max.message.bytes":   "1048588",
				"segment.ms":          "604800000",
				"segment.bytes":       "1073741824",
				"delete.retention.ms": "86400000",
			},
		},
		{
			name: "partial-important-keys",
			in: map[string]string{
				"retention.ms":    "1000",
				"cleanup.policy":  "delete",
				"unrelated.key.1": "value1",
				"unrelated.key.2": "value2",
			},
			want: map[string]string{
				"retention.ms":   "1000",
				"cleanup.policy": "delete",
			},
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
		{
			name: "no-retention-config",
			config: map[string]string{
				"cleanup.policy": "compact",
			},
			wantHuman: "infinite",
			wantConfig: map[string]string{
				"cleanup.policy": "compact",
			},
		},
		{
			name:       "empty-config",
			config:     map[string]string{},
			wantHuman:  "infinite",
			wantConfig: map[string]string{},
		},
		{
			name: "infinite-retention",
			config: map[string]string{
				"retention.ms":   "-1",
				"cleanup.policy": "delete",
			},
			wantHuman: "infinite",
			wantConfig: map[string]string{
				"retention.ms":   "-1",
				"cleanup.policy": "delete",
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
		name       string
		consumers  []string
		wantCount  int
		wantGroups []string
	}{
		{name: "none", consumers: nil, wantCount: 0, wantGroups: nil},
		{name: "some", consumers: []string{"cg-1", "cg-2"}, wantCount: 2, wantGroups: []string{"cg-1", "cg-2"}},
		{name: "empty-slice", consumers: []string{}, wantCount: 0, wantGroups: []string{}},
		{name: "single", consumers: []string{"cg-1"}, wantCount: 1, wantGroups: []string{"cg-1"}},
		{name: "many", consumers: []string{"cg-1", "cg-2", "cg-3", "cg-4", "cg-5"}, wantCount: 5, wantGroups: []string{"cg-1", "cg-2", "cg-3", "cg-4", "cg-5"}},
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
			// Handle nil vs empty slice comparison
			if tc.wantGroups == nil && got.ConsumerGroups != nil {
				t.Fatalf("ConsumerGroups = %v, want nil", got.ConsumerGroups)
			}
			if tc.wantGroups != nil && !reflect.DeepEqual(got.ConsumerGroups, tc.wantGroups) {
				t.Fatalf("ConsumerGroups = %v, want %v", got.ConsumerGroups, tc.wantGroups)
			}
			if got.Name != "payments" {
				t.Fatalf("Name = %q, want %q", got.Name, "payments")
			}
			if got.Partitions != 6 {
				t.Fatalf("Partitions = %d, want %d", got.Partitions, 6)
			}
			if got.ReplicationFactor != 3 {
				t.Fatalf("ReplicationFactor = %d, want %d", got.ReplicationFactor, 3)
			}
		})
	}
}

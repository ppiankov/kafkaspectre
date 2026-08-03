package reporter

import "github.com/ppiankov/kafkaspectre/internal/kafka"

// WO-49: ConfigRisk represents a topic configuration that poses a risk.
type ConfigRisk struct {
	Topic          string `json:"topic"`
	RiskType       string `json:"risk_type"`
	Current        string `json:"current"`
	Recommendation string `json:"recommendation"`
	Severity       string `json:"severity"`
}

// AssessConfigRisk evaluates a topic's configuration for risk signals.
// WO-49: the data is already fetched by DescribeTopicConfigs — this function
// just assesses it against deterministic rules.
func AssessConfigRisk(topic *kafka.TopicInfo, brokerCount int) []ConfigRisk {
	var risks []ConfigRisk

	// RF=1 on a multi-broker cluster: no fault tolerance.
	if topic.ReplicationFactor == 1 && brokerCount > 1 {
		risks = append(risks, ConfigRisk{
			Topic:          topic.Name,
			RiskType:       "under_replicated",
			Current:        "replication.factor=1",
			Recommendation: "Increase replication factor to at least 2 for fault tolerance",
			Severity:       "high",
		})
	}

	retentionMs := topic.Config["retention.ms"]
	if retentionMs == "-1" || retentionMs == "" {
		if retentionMs == "-1" {
			risks = append(risks, ConfigRisk{
				Topic:          topic.Name,
				RiskType:       "infinite_retention",
				Current:        "retention.ms=-1 (infinite)",
				Recommendation: "Set a finite retention to bound storage growth",
				Severity:       "medium",
			})
		}
	}

	minISR := topic.Config["min.insync.replicas"]
	if minISR == "1" && topic.ReplicationFactor >= 3 {
		risks = append(risks, ConfigRisk{
			Topic:          topic.Name,
			RiskType:       "weak_durability",
			Current:        "min.insync.replicas=1 with RF>=3",
			Recommendation: "Increase min.insync.replicas to 2 for acknowledged-write durability",
			Severity:       "medium",
		})
	}

	cleanupPolicy := topic.Config["cleanup.policy"]
	if cleanupPolicy == "compact" && topic.Partitions > 12 {
		risks = append(risks, ConfigRisk{
			Topic:          topic.Name,
			RiskType:       "compact_high_partition",
			Current:        "cleanup.policy=compact with " + itoa(topic.Partitions) + " partitions",
			Recommendation: "Verify compaction is intended for this partition count",
			Severity:       "low",
		})
	}

	return risks
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	if neg {
		digits = "-" + digits
	}
	return digits
}

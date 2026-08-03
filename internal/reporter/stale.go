package reporter

// WO-47: StaleTopic represents a topic with active consumers but high lag.
// Unlike an unused topic (no consumers), a stale topic IS being consumed —
// the consumer is just falling behind. This is a distinct health signal.
type StaleTopic struct {
	Name              string   `json:"name"`
	Partitions        int      `json:"partitions"`
	ReplicationFactor int      `json:"replication_factor"`
	TotalLag          int64    `json:"total_lag"`
	ConsumerGroups    []string `json:"consumer_groups"`
	Recommendation    string   `json:"recommendation"`
	Risk              string   `json:"risk"`
}

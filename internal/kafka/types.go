package kafka

import "time"

// ClusterMetadata contains the complete metadata fetched from a Kafka cluster
type ClusterMetadata struct {
	Topics         map[string]*TopicInfo
	ConsumerGroups map[string]*ConsumerGroupInfo
	Brokers        []BrokerInfo
	FetchedAt      time.Time
}

// TopicInfo contains metadata about a Kafka topic
type TopicInfo struct {
	Name              string
	Partitions        int
	ReplicationFactor int
	Config            map[string]string
	CreatedAt         time.Time
	Internal          bool // System topics like __consumer_offsets
}

// ConsumerGroupInfo contains metadata about a Kafka consumer group
type ConsumerGroupInfo struct {
	GroupID     string
	State       string // Stable, Empty, Dead, etc.
	Members     int
	Topics      []string
	Lag         map[string]int64 // topic -> total lag
	LastCommit  time.Time
	Coordinator int32 // Broker ID
}

// BrokerInfo contains metadata about a Kafka broker
type BrokerInfo struct {
	ID   int32
	Host string
	Port int32
	Rack string
}

// Config holds the configuration for connecting to Kafka
type Config struct {
	BootstrapServers string
	AuthMechanism    string // PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
	Username         string
	Password         string
	TLSEnabled       bool // Enable TLS without client certificates
	TLSCertFile      string
	TLSKeyFile       string
	TLSCAFile        string
	QueryTimeout     time.Duration
}

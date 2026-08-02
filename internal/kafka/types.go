package kafka

import (
	"strings"
	"time"
)

// ClusterMetadata contains the complete metadata fetched from a Kafka cluster
type ClusterMetadata struct {
	Topics         map[string]*TopicInfo
	ConsumerGroups map[string]*ConsumerGroupInfo
	Brokers        []BrokerInfo
	FetchedAt      time.Time

	// ConsumerGroupReadErrors names the consumer-group reads that failed. When
	// non-empty the consumer-group picture is incomplete and "no consumers"
	// cannot be distinguished from "could not read consumers".
	//
	// WO-27: a DescribeGroups failure previously left ConsumerGroups empty and
	// the audit reported every topic in the cluster as unused with a delete
	// recommendation. Callers must treat a degraded read as unusable for
	// unused-topic verdicts rather than as evidence of absence.
	ConsumerGroupReadErrors []string
}

// ConsumerGroupsComplete reports whether every consumer-group read succeeded.
//
// WO-27: unused-topic verdicts are only sound when this is true.
// WO-27: read completeness gate
func (m *ClusterMetadata) ConsumerGroupsComplete() bool {
	return m != nil && len(m.ConsumerGroupReadErrors) == 0
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

// ManagedOwner reports which service owns this topic as backing store.
//
// WO-26: derived from the topic name at the point of use rather than stored on
// the struct, so a caller that constructs a TopicInfo directly cannot end up
// with an unclassified managed topic and a delete recommendation.
// WO-26: name-derived managed classification
func (t *TopicInfo) ManagedOwner() ManagedTopicOwner {
	if t == nil {
		return OwnerNone
	}
	return ManagedTopicOwnerFor(t.Name)
}

// IsManaged reports whether this topic is backing store for a known service.
// WO-26: report whether topic is managed
func (t *TopicInfo) IsManaged() bool {
	return t.ManagedOwner() != OwnerNone
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

// abandonedGroupStates are the consumer-group states that carry no live members.
var abandonedGroupStates = map[string]struct{}{
	"empty": {},
	"dead":  {},
}

// IsAbandoned reports whether the group has no live members and therefore does
// not on its own make a topic active.
//
// WO-29: State was captured but never read, so an Empty or Dead group — the
// very signal that a topic is no longer consumed — marked its topics ACTIVE and
// hid them from the unused list.
// WO-29: abandoned consumer group detection
func (c *ConsumerGroupInfo) IsAbandoned() bool {
	if c == nil {
		return true
	}
	_, abandoned := abandonedGroupStates[strings.ToLower(strings.TrimSpace(c.State))]
	return abandoned
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

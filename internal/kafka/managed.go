package kafka

import "strings"

// ManagedTopicOwner names the service that owns a provider-managed topic.
type ManagedTopicOwner string

const (
	// OwnerNone marks a topic as not managed by any known service.
	OwnerNone ManagedTopicOwner = ""
	// OwnerKafka covers broker-internal topics such as __consumer_offsets.
	OwnerKafka ManagedTopicOwner = "Kafka broker"
	// OwnerSchemaRegistry covers the Confluent Schema Registry backing store.
	OwnerSchemaRegistry ManagedTopicOwner = "Confluent Schema Registry"
	// OwnerConfluent covers Confluent Platform internal topics.
	OwnerConfluent ManagedTopicOwner = "Confluent Platform"
	// OwnerConnect covers Kafka Connect backing topics.
	OwnerConnect ManagedTopicOwner = "Kafka Connect"
	// OwnerMSK covers AWS MSK service-managed topics.
	OwnerMSK ManagedTopicOwner = "AWS MSK"
)

// exactManagedTopics maps topic names that are managed in full to their owner.
// Deletion of any of these destroys the owning service's state.
var exactManagedTopics = map[string]ManagedTopicOwner{
	"_schemas":        OwnerSchemaRegistry,
	"connect-configs": OwnerConnect,
	"connect-offsets": OwnerConnect,
	"connect-status":  OwnerConnect,
}

// managedTopicPrefixes maps topic name prefixes to the owning service. Order
// matters: the first match wins, so more specific prefixes precede general ones.
var managedTopicPrefixes = []struct {
	prefix string
	owner  ManagedTopicOwner
}{
	{"__amazon_msk_", OwnerMSK},
	{"_confluent", OwnerConfluent},
	{"__", OwnerKafka},
}

// ManagedTopicOwnerFor reports which service manages a topic, or OwnerNone.
//
// WO-26: the previous `strings.HasPrefix(topic, "__")` test missed every
// single-underscore and hyphenated service topic, so `_schemas` and the
// connect-* topics were reported unused with a "safe to delete" recommendation.
// Deleting _schemas destroys every registered schema in the cluster.
// Matching is exact-then-prefix and deterministic — no heuristics.
func ManagedTopicOwnerFor(topic string) ManagedTopicOwner {
	if owner, ok := exactManagedTopics[topic]; ok {
		return owner
	}

	for _, candidate := range managedTopicPrefixes {
		if strings.HasPrefix(topic, candidate.prefix) {
			return candidate.owner
		}
	}

	return OwnerNone
}

// IsManagedTopic reports whether a topic is backing store for a known service.
func IsManagedTopic(topic string) bool {
	return ManagedTopicOwnerFor(topic) != OwnerNone
}

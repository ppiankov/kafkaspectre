package kafka

import (
	"path"
	"strings"
)

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
	// OwnerStreams covers Kafka Streams changelog and repartition topics.
	OwnerStreams ManagedTopicOwner = "Kafka Streams"
	// OwnerMirrorMaker covers MirrorMaker 2 internal topics.
	OwnerMirrorMaker ManagedTopicOwner = "MirrorMaker 2"
	// OwnerOperator marks a topic declared managed by the operator.
	OwnerOperator ManagedTopicOwner = "operator-declared"
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

// managedTopicSuffixes maps topic name suffixes to the owning service.
//
// WO-41: Kafka Streams identifies its backing topics by SUFFIX, not prefix —
// `<application.id>-<store>-changelog` and `-repartition`. Changelogs are read
// by restore consumers via assign(), so they never have a consumer group and
// were classified "unused, safe to delete". Deleting one destroys the state
// store and the application cannot restore.
var managedTopicSuffixes = []struct {
	suffix string
	owner  ManagedTopicOwner
}{
	{"-changelog", OwnerStreams},
	{"-repartition", OwnerStreams},
	// A topic legitimately named `orders-changelog` in a shop that runs no
	// Streams app is held out by this rule. summary.managed_topics_held_out
	// names how many topics that affected so the hold-out is discoverable.
	{".checkpoints.internal", OwnerMirrorMaker},
}

// managedTopicPrefixes2 covers MirrorMaker 2 internals, which use dotted
// cluster-qualified names rather than a single fixed prefix.
//
// `heartbeats` is deliberately NOT matched: MirrorMaker uses that bare name, but
// so do many ordinary applications, and silently holding a user topic out of the
// audit is worse than listing a MirrorMaker one. Declare it via managed_topics
// if your cluster runs MirrorMaker with default heartbeat naming.
var mirrorMakerPrefixes = []string{"mm2-offsets.", "mm2-configs.", "mm2-status."}

// extraManagedPatterns holds operator-declared glob patterns for backing topics
// this tool cannot recognise by name — renamed Connect topics
// (`docker-connect-configs`) and custom Streams application IDs.
//
// WO-41: name-based recognition is best-effort by construction. A deployment
// that renames its backing topics must be able to declare them.
var extraManagedPatterns []string

// SetExtraManagedPatterns declares additional managed-topic glob patterns.
func SetExtraManagedPatterns(patterns []string) {
	extraManagedPatterns = append([]string(nil), patterns...)
}

// ManagedTopicOwnerFor reports which service manages a topic, or OwnerNone.
//
// WO-26: the previous `strings.HasPrefix(topic, "__")` test missed every
// single-underscore and hyphenated service topic, so `_schemas` and the
// connect-* topics were reported unused with a "safe to delete" recommendation.
// Deleting _schemas destroys every registered schema in the cluster.
// Matching is exact-then-prefix and deterministic — no heuristics.
// WO-26: classify managed topic by name
func ManagedTopicOwnerFor(topic string) ManagedTopicOwner {
	if owner, ok := exactManagedTopics[topic]; ok {
		return owner
	}

	for _, candidate := range managedTopicSuffixes {
		if strings.HasSuffix(topic, candidate.suffix) {
			return candidate.owner
		}
	}

	for _, prefix := range mirrorMakerPrefixes {
		if strings.HasPrefix(topic, prefix) {
			return OwnerMirrorMaker
		}
	}

	for _, candidate := range managedTopicPrefixes {
		if strings.HasPrefix(topic, candidate.prefix) {
			return candidate.owner
		}
	}

	// WO-41: operator-declared patterns are checked last so they can only ADD
	// protection, never downgrade a topic this tool already recognises.
	for _, pattern := range extraManagedPatterns {
		if matched, err := path.Match(pattern, topic); err == nil && matched {
			return OwnerOperator
		}
	}

	return OwnerNone
}

// IsManagedTopic reports whether a topic is backing store for a known service.
// WO-26: report whether topic is managed
func IsManagedTopic(topic string) bool {
	return ManagedTopicOwnerFor(topic) != OwnerNone
}

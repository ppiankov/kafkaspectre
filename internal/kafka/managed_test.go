package kafka

import "testing"

// WO-26: the previous "__" prefix test missed every single-underscore and
// hyphenated service topic. Each case below was reported as an unused topic
// with a delete recommendation before this classification existed.
func TestManagedTopicOwnerFor(t *testing.T) {
	cases := []struct {
		name  string
		topic string
		want  ManagedTopicOwner
	}{
		{name: "schema-registry", topic: "_schemas", want: OwnerSchemaRegistry},
		{name: "connect-configs", topic: "connect-configs", want: OwnerConnect},
		{name: "connect-offsets", topic: "connect-offsets", want: OwnerConnect},
		{name: "connect-status", topic: "connect-status", want: OwnerConnect},
		{name: "confluent-prefix", topic: "_confluent-command", want: OwnerConfluent},
		{name: "confluent-metrics", topic: "_confluent-metrics", want: OwnerConfluent},
		{name: "msk-canary", topic: "__amazon_msk_canary", want: OwnerMSK},
		{name: "consumer-offsets", topic: "__consumer_offsets", want: OwnerKafka},
		{name: "transaction-state", topic: "__transaction_state", want: OwnerKafka},

		{name: "ordinary", topic: "orders", want: OwnerNone},
		{name: "ordinary-hyphen", topic: "user-events", want: OwnerNone},
		{name: "ordinary-underscore", topic: "user_events", want: OwnerNone},
		{name: "connect-lookalike", topic: "connect-my-app-events", want: OwnerNone},
		{name: "schemas-lookalike", topic: "_schemas_backup", want: OwnerNone},
		{name: "empty", topic: "", want: OwnerNone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ManagedTopicOwnerFor(tc.topic); got != tc.want {
				t.Fatalf("ManagedTopicOwnerFor(%q) = %q, want %q", tc.topic, got, tc.want)
			}
			if got, want := IsManagedTopic(tc.topic), tc.want != OwnerNone; got != want {
				t.Fatalf("IsManagedTopic(%q) = %v, want %v", tc.topic, got, want)
			}
		})
	}
}

// WO-27: completeness gates whether unused verdicts are sound.
func TestConsumerGroupsComplete(t *testing.T) {
	cases := []struct {
		name     string
		metadata *ClusterMetadata
		want     bool
	}{
		{name: "nil", metadata: nil, want: false},
		{name: "clean", metadata: &ClusterMetadata{}, want: true},
		{
			name:     "degraded",
			metadata: &ClusterMetadata{ConsumerGroupReadErrors: []string{"describe failed"}},
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.metadata.ConsumerGroupsComplete(); got != tc.want {
				t.Fatalf("ConsumerGroupsComplete() = %v, want %v", got, tc.want)
			}
		})
	}
}

// WO-29: State was captured but never read, so Empty and Dead groups marked
// their topics active and hid genuinely unused topics.
func TestConsumerGroupIsAbandoned(t *testing.T) {
	cases := []struct {
		name  string
		group *ConsumerGroupInfo
		want  bool
	}{
		{name: "stable", group: &ConsumerGroupInfo{State: "Stable"}, want: false},
		{name: "preparing-rebalance", group: &ConsumerGroupInfo{State: "PreparingRebalance"}, want: false},
		{name: "completing-rebalance", group: &ConsumerGroupInfo{State: "CompletingRebalance"}, want: false},
		{name: "empty", group: &ConsumerGroupInfo{State: "Empty"}, want: true},
		{name: "dead", group: &ConsumerGroupInfo{State: "Dead"}, want: true},
		{name: "lowercase-empty", group: &ConsumerGroupInfo{State: "empty"}, want: true},
		{name: "padded-dead", group: &ConsumerGroupInfo{State: " Dead "}, want: true},
		{name: "unknown-state", group: &ConsumerGroupInfo{State: "Whatever"}, want: false},
		{name: "nil", group: nil, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.group.IsAbandoned(); got != tc.want {
				t.Fatalf("IsAbandoned() = %v, want %v", got, tc.want)
			}
		})
	}
}

// WO-41: Streams changelog/repartition topics are read by restore consumers via
// assign(), so they never have a consumer group and were scored "safe to
// delete". Deleting a changelog destroys the state store.
func TestManagedTopicSuffixesAndMirrorMaker(t *testing.T) {
	cases := []struct {
		name  string
		topic string
		want  ManagedTopicOwner
	}{
		{name: "streams-changelog", topic: "payments-app-orders-store-changelog", want: OwnerStreams},
		{name: "streams-repartition", topic: "payments-app-orders-store-repartition", want: OwnerStreams},
		{name: "mm2-offsets", topic: "mm2-offsets.us-east.internal", want: OwnerMirrorMaker},
		{name: "mm2-configs", topic: "mm2-configs.us-east.internal", want: OwnerMirrorMaker},
		{name: "mm2-status", topic: "mm2-status.us-east.internal", want: OwnerMirrorMaker},
		{name: "checkpoints", topic: "us-east.checkpoints.internal", want: OwnerMirrorMaker},
		// `heartbeats` is intentionally NOT matched — too common an ordinary
		// topic name to swallow silently. Declare it via managed_topics.
		{name: "bare-heartbeats-not-matched", topic: "heartbeats", want: OwnerNone},

		// Ordinary topics that merely contain a similar word must not match.
		{name: "changelog-in-middle", topic: "user-changelog-events", want: OwnerNone},
		{name: "repartition-in-middle", topic: "repartition-service-events", want: OwnerNone},
		{name: "mm2-lookalike", topic: "mm2-application-events", want: OwnerNone},
		{name: "heartbeat-singular", topic: "heartbeat", want: OwnerNone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ManagedTopicOwnerFor(tc.topic); got != tc.want {
				t.Fatalf("ManagedTopicOwnerFor(%q) = %q, want %q", tc.topic, got, tc.want)
			}
		})
	}
}

// WO-41: renamed Connect and Streams topics cannot be recognised by name, so the
// operator must be able to declare them. Patterns may only ADD protection.
func TestExtraManagedPatterns(t *testing.T) {
	original := extraManagedPatterns
	t.Cleanup(func() { extraManagedPatterns = original })

	SetExtraManagedPatterns([]string{"docker-connect-*", "acme-*-state"})

	for _, topic := range []string{"docker-connect-configs", "acme-billing-state"} {
		if got := ManagedTopicOwnerFor(topic); got != OwnerOperator {
			t.Errorf("ManagedTopicOwnerFor(%q) = %q, want %q", topic, got, OwnerOperator)
		}
	}
	if got := ManagedTopicOwnerFor("orders"); got != OwnerNone {
		t.Errorf("operator patterns must not capture unrelated topics; got %q", got)
	}
	// A topic this tool already recognises keeps its specific owner.
	if got := ManagedTopicOwnerFor("_schemas"); got != OwnerSchemaRegistry {
		t.Errorf("operator patterns downgraded a known owner to %q", got)
	}
}

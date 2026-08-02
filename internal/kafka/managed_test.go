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

// WO-26: msk topics also match the "__" prefix, so ordering in
// managedTopicPrefixes decides the owner. The more specific prefix must win.
func TestManagedTopicPrefixSpecificityOrder(t *testing.T) {
	if got := ManagedTopicOwnerFor("__amazon_msk_canary"); got != OwnerMSK {
		t.Fatalf("MSK topic attributed to %q, want %q", got, OwnerMSK)
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

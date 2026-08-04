package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
	"github.com/spf13/cobra"
)

// WO-60: Multi-cluster topic diff.
//
// Killgate-compliant design (three-way VectorCouncil/Gemini/GPT, 2026-08-04):
// - Flat per-topic observations, NOT directional absence buckets
// - Per-cluster Reliability signal (WO-27/WO-38 pattern)
// - Per-cluster fetched_at to expose TOCTOU window
// - No normative labels ("mismatch", "missing", "drift")
// - No prose disclaimer note (compliant schema doesn't need one)

// ClusterSnapshot is the per-cluster metadata for the diff output.
type ClusterSnapshot struct {
	Address        string `json:"address"`
	TopicsObserved int    `json:"topics_observed"`
	FetchedAt      string `json:"fetched_at"`
}

// DiffReliability records whether each cluster's read was complete.
type DiffReliability struct {
	ClusterAComplete bool     `json:"cluster_a_complete"`
	ClusterBComplete bool     `json:"cluster_b_complete"`
	ReadErrors       []string `json:"read_errors,omitempty"`
}

// TopicObservation is a single topic's per-cluster factual observation.
// The consumer derives differences; the tool does not serialize them.
type TopicObservation struct {
	Name               string `json:"name"`
	ObservedInA        bool   `json:"observed_in_a"`
	ObservedInB        bool   `json:"observed_in_b"`
	PartitionsA        int    `json:"partitions_a,omitempty"`
	PartitionsB        int    `json:"partitions_b,omitempty"`
	ReplicationFactorA int    `json:"replication_factor_a,omitempty"`
	ReplicationFactorB int    `json:"replication_factor_b,omitempty"`
	Internal           bool   `json:"internal"`
}

// DiffResult is the killgate-compliant output schema.
type DiffResult struct {
	Tool        string             `json:"tool"`
	Timestamp   string             `json:"timestamp"`
	ClusterA    ClusterSnapshot    `json:"cluster_a"`
	ClusterB    ClusterSnapshot    `json:"cluster_b"`
	Topics      []TopicObservation `json:"topics"`
	Reliability DiffReliability    `json:"reliability"`
}

// WO-60/killgate: cluster diff command with compliant schema.
func newDiffCmd() *cobra.Command {
	var (
		clusterA, clusterB         string
		authMechismA, authMechismB string
		tlsA, tlsB                 bool
		excludeInternal            bool
	)

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Compare topic lists between two Kafka clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			if clusterA == "" || clusterB == "" {
				return fmt.Errorf("--cluster-a and --cluster-b are both required")
			}

			result := runDiff(cmd.Context(), clusterA, clusterB, authMechismA, authMechismB, tlsA, tlsB, excludeInternal)

			if !result.Reliability.ClusterAComplete || !result.Reliability.ClusterBComplete {
				slog.Warn("diff completed with incomplete reads", "cluster_a_complete", result.Reliability.ClusterAComplete, "cluster_b_complete", result.Reliability.ClusterBComplete)
			}

			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", data)
			return err
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&clusterA, "cluster-a", "", "First cluster bootstrap server(s)")
	flags.StringVar(&clusterB, "cluster-b", "", "Second cluster bootstrap server(s)")
	flags.StringVar(&authMechismA, "auth-mechanism-a", "", "Auth mechanism for cluster A (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512, AWS_MSK_IAM)")
	flags.StringVar(&authMechismB, "auth-mechanism-b", "", "Auth mechanism for cluster B (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512, AWS_MSK_IAM)")
	flags.BoolVar(&tlsA, "tls-a", false, "Enable TLS for cluster A")
	flags.BoolVar(&tlsB, "tls-b", false, "Enable TLS for cluster B")
	flags.BoolVar(&excludeInternal, "exclude-internal", true, "Exclude internal (__-prefixed) topics")

	_ = cmd.MarkFlagRequired("cluster-a")
	_ = cmd.MarkFlagRequired("cluster-b")

	return cmd
}

// runDiff fetches both clusters and produces the compliant diff result.
func runDiff(ctx context.Context, addrA, addrB, authA, authB string, tlsA, tlsB, excludeInternal bool) DiffResult {
	result := DiffResult{
		Tool:      "kafkaspectre",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		ClusterA:  ClusterSnapshot{Address: addrA},
		ClusterB:  ClusterSnapshot{Address: addrB},
		Reliability: DiffReliability{
			ClusterAComplete: true,
			ClusterBComplete: true,
		},
	}

	topicsA, fetchedAtA, errA := fetchTopicListWithMeta(ctx, addrA, authA, tlsA)
	if errA != nil {
		result.Reliability.ClusterAComplete = false
		result.Reliability.ReadErrors = append(result.Reliability.ReadErrors, fmt.Sprintf("cluster-a: %v", errA))
		topicsA = map[string]*kafka.TopicInfo{}
	}
	result.ClusterA.FetchedAt = fetchedAtA
	result.ClusterA.TopicsObserved = len(topicsA)

	topicsB, fetchedAtB, errB := fetchTopicListWithMeta(ctx, addrB, authB, tlsB)
	if errB != nil {
		result.Reliability.ClusterBComplete = false
		result.Reliability.ReadErrors = append(result.Reliability.ReadErrors, fmt.Sprintf("cluster-b: %v", errB))
		topicsB = map[string]*kafka.TopicInfo{}
	}
	result.ClusterB.FetchedAt = fetchedAtB
	result.ClusterB.TopicsObserved = len(topicsB)

	result.Topics = buildTopicObservations(topicsA, topicsB, excludeInternal)

	return result
}

// fetchTopicListWithMeta connects to a cluster and returns topics + fetch timestamp.
func fetchTopicListWithMeta(ctx context.Context, brokers, authMechanism string, tls bool) (map[string]*kafka.TopicInfo, string, error) {
	cfg := kafka.Config{
		BootstrapServers: brokers,
		AuthMechanism:    authMechanism,
		TLSEnabled:       tls,
		QueryTimeout:     defaultQueryTimeout,
	}

	inspector, err := kafka.NewInspector(cfg)
	if err != nil {
		return nil, "", err
	}
	defer inspector.Close()

	queryCtx, cancel := context.WithTimeout(ctx, cfg.QueryTimeout)
	defer cancel()

	metadata, err := inspector.FetchMetadata(queryCtx)
	if err != nil {
		return nil, "", err
	}

	return metadata.Topics, metadata.FetchedAt.UTC().Format(time.RFC3339), nil
}

// buildTopicObservations produces a flat list of per-topic factual observations.
// Killgate: no directional buckets, no normative labels. The consumer derives
// differences from observed_in_a/observed_in_b booleans.
func buildTopicObservations(topicsA, topicsB map[string]*kafka.TopicInfo, excludeInternal bool) []TopicObservation {
	allNames := make(map[string]struct{})
	for name, t := range topicsA {
		if excludeInternal && t.Internal {
			continue
		}
		allNames[name] = struct{}{}
	}
	for name, t := range topicsB {
		if excludeInternal && t.Internal {
			continue
		}
		allNames[name] = struct{}{}
	}

	names := make([]string, 0, len(allNames))
	for name := range allNames {
		names = append(names, name)
	}
	sort.Strings(names)

	observations := make([]TopicObservation, 0, len(names))
	for _, name := range names {
		obs := TopicObservation{Name: name}

		if t, ok := topicsA[name]; ok {
			obs.ObservedInA = true
			obs.PartitionsA = t.Partitions
			obs.ReplicationFactorA = t.ReplicationFactor
			obs.Internal = t.Internal
		}
		if t, ok := topicsB[name]; ok {
			obs.ObservedInB = true
			obs.PartitionsB = t.Partitions
			obs.ReplicationFactorB = t.ReplicationFactor
			if !obs.Internal {
				obs.Internal = t.Internal
			}
		}

		observations = append(observations, obs)
	}

	return observations
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
	"github.com/spf13/cobra"
)

// WO-60: Multi-cluster topic diff.
//
// Evidence Composition Law compliance: this command reports FACTUAL observations
// from two authoritative, closed-world topic lists. It does NOT derive negative
// conclusions ("topic is missing", "drift detected") or recommend remediation.
// Differences may be intentional (DR topology, staging-only, lifecycle stage).

// DiffResult is the output of a cluster-to-cluster topic comparison.
type DiffResult struct {
	Tool      string `json:"tool"`
	Timestamp string `json:"timestamp"`
	ClusterA  string `json:"cluster_a"`
	ClusterB  string `json:"cluster_b"`
	// TopicsPresentInANotB lists topics that exist in cluster A but not in B.
	// This is a factual observation from two complete ListTopics calls, not a
	// conclusion that the topic "should" be in B.
	TopicsPresentInANotB []TopicDiff `json:"topics_present_in_a_not_b"`
	// TopicsPresentInBNotA lists topics that exist in cluster B but not in A.
	TopicsPresentInBNotA []TopicDiff `json:"topics_present_in_b_not_a"`
	// ConfigMismatches lists topics present in both clusters with different
	// partition count or replication factor.
	ConfigMismatches []ConfigMismatch `json:"config_mismatches"`
	// TopicsInBoth lists topics present in both clusters with matching config.
	TopicsInBoth int `json:"topics_in_both"`
	// Note explains the evidence-compliance boundary.
	Note string `json:"note"`
}

// WO-60: factual topic observation from a cluster diff.
type TopicDiff struct {
	Name              string `json:"name"`
	Partitions        int    `json:"partitions"`
	ReplicationFactor int    `json:"replication_factor"`
	Internal          bool   `json:"internal"`
}

// WO-60: a topic present in both clusters with a configuration difference.
type ConfigMismatch struct {
	Name               string `json:"name"`
	PartitionsA        int    `json:"partitions_a"`
	PartitionsB        int    `json:"partitions_b"`
	ReplicationFactorA int    `json:"replication_factor_a"`
	ReplicationFactorB int    `json:"replication_factor_b"`
}

// WO-60: cluster diff command for factual topic comparison.
func newDiffCmd() *cobra.Command {
	var (
		clusterA, clusterB         string
		authMechismA, authMechismB string
		tlsA, tlsB                 bool
		excludeInternal            bool
		output                     string
	)

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Compare topic lists between two Kafka clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			if clusterA == "" || clusterB == "" {
				return fmt.Errorf("--cluster-a and --cluster-b are both required")
			}

			output = strings.ToLower(strings.TrimSpace(output))
			if output == "" {
				output = "json"
			}

			topicsA, err := fetchTopicList(cmd.Context(), clusterA, authMechismA, tlsA)
			if err != nil {
				return fmt.Errorf("cluster-a: %w", err)
			}
			topicsB, err := fetchTopicList(cmd.Context(), clusterB, authMechismB, tlsB)
			if err != nil {
				return fmt.Errorf("cluster-b: %w", err)
			}

			result := compareTopics(clusterA, clusterB, topicsA, topicsB, excludeInternal)

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
	flags.BoolVar(&excludeInternal, "exclude-internal", true, "Exclude internal (__-prefixed) topics from comparison")
	flags.StringVar(&output, "output", "json", "Output format (json)")

	_ = cmd.MarkFlagRequired("cluster-a")
	_ = cmd.MarkFlagRequired("cluster-b")

	return cmd
}

// fetchTopicList connects to a cluster and returns its topic map.
func fetchTopicList(ctx context.Context, brokers, authMechanism string, tls bool) (map[string]*kafka.TopicInfo, error) {
	cfg := kafka.Config{
		BootstrapServers: brokers,
		AuthMechanism:    authMechanism,
		TLSEnabled:       tls,
		QueryTimeout:     defaultQueryTimeout,
	}

	inspector, err := kafka.NewInspector(cfg)
	if err != nil {
		return nil, err
	}
	defer inspector.Close()

	queryCtx, cancel := context.WithTimeout(ctx, cfg.QueryTimeout)
	defer cancel()

	metadata, err := inspector.FetchMetadata(queryCtx)
	if err != nil {
		return nil, err
	}

	return metadata.Topics, nil
}

// compareTopics performs the factual set-difference between two topic maps.
func compareTopics(clusterA, clusterB string, topicsA, topicsB map[string]*kafka.TopicInfo, excludeInternal bool) DiffResult {
	result := DiffResult{
		Tool:     "kafkaspectre",
		ClusterA: clusterA,
		ClusterB: clusterB,
		Note:     "Differences are factual observations from two complete topic lists, not recommendations. Differences may be intentional (DR topology, staging-only topics, lifecycle stage).",
	}

	for name, topicA := range topicsA {
		if excludeInternal && topicA.Internal {
			continue
		}

		topicB, inB := topicsB[name]
		if !inB {
			result.TopicsPresentInANotB = append(result.TopicsPresentInANotB, TopicDiff{
				Name:              name,
				Partitions:        topicA.Partitions,
				ReplicationFactor: topicA.ReplicationFactor,
				Internal:          topicA.Internal,
			})
			continue
		}

		if topicA.Partitions != topicB.Partitions || topicA.ReplicationFactor != topicB.ReplicationFactor {
			result.ConfigMismatches = append(result.ConfigMismatches, ConfigMismatch{
				Name:               name,
				PartitionsA:        topicA.Partitions,
				PartitionsB:        topicB.Partitions,
				ReplicationFactorA: topicA.ReplicationFactor,
				ReplicationFactorB: topicB.ReplicationFactor,
			})
		} else {
			result.TopicsInBoth++
		}
	}

	for name, topicB := range topicsB {
		if excludeInternal && topicB.Internal {
			continue
		}
		if _, inA := topicsA[name]; !inA {
			result.TopicsPresentInBNotA = append(result.TopicsPresentInBNotA, TopicDiff{
				Name:              name,
				Partitions:        topicB.Partitions,
				ReplicationFactor: topicB.ReplicationFactor,
				Internal:          topicB.Internal,
			})
		}
	}

	sort.Slice(result.TopicsPresentInANotB, func(i, j int) bool {
		return result.TopicsPresentInANotB[i].Name < result.TopicsPresentInANotB[j].Name
	})
	sort.Slice(result.TopicsPresentInBNotA, func(i, j int) bool {
		return result.TopicsPresentInBNotA[i].Name < result.TopicsPresentInBNotA[j].Name
	})
	sort.Slice(result.ConfigMismatches, func(i, j int) bool {
		return result.ConfigMismatches[i].Name < result.ConfigMismatches[j].Name
	})

	return result
}

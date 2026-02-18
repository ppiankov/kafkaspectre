package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

// Inspector provides methods to fetch metadata from a Kafka cluster
type Inspector struct {
	client *kgo.Client
	admin  *kadm.Client
	config Config
}

// NewInspector creates a new Kafka inspector with the given configuration
func NewInspector(cfg Config) (*Inspector, error) {
	// Parse bootstrap servers
	seeds := strings.Split(cfg.BootstrapServers, ",")
	for i, seed := range seeds {
		seeds[i] = strings.TrimSpace(seed)
	}

	// Build client options
	opts := []kgo.Opt{
		kgo.SeedBrokers(seeds...),
		kgo.RequestTimeoutOverhead(cfg.QueryTimeout),
	}

	// Configure SASL authentication
	if cfg.AuthMechanism != "" {
		saslOpt, err := buildSASL(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to configure SASL: %w", err)
		}
		opts = append(opts, saslOpt)
	}

	// Configure TLS
	if cfg.TLSEnabled || cfg.TLSCertFile != "" || cfg.TLSCAFile != "" {
		tlsConfig, err := buildTLS(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to configure TLS: %w", err)
		}
		opts = append(opts, kgo.DialTLSConfig(tlsConfig))
	}

	// Create franz-go client
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka client: %w", err)
	}

	// Ping the cluster to verify connectivity (with retry for transient failures)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.QueryTimeout)
	defer cancel()

	if err := withRetry(ctx, "ping broker", func() error {
		return client.Ping(ctx)
	}); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to Kafka cluster: %w", err)
	}

	// Create admin client for metadata operations
	admin := kadm.NewClient(client)

	return &Inspector{
		client: client,
		admin:  admin,
		config: cfg,
	}, nil
}

// Close closes the Kafka client connection
func (i *Inspector) Close() {
	if i.client != nil {
		i.client.Close()
	}
}

// FetchMetadata fetches comprehensive metadata from the Kafka cluster
func (i *Inspector) FetchMetadata(ctx context.Context) (*ClusterMetadata, error) {
	metadata := &ClusterMetadata{
		Topics:         make(map[string]*TopicInfo),
		ConsumerGroups: make(map[string]*ConsumerGroupInfo),
		Brokers:        []BrokerInfo{},
		FetchedAt:      time.Now(),
	}

	// Fetch broker metadata
	var brokerMeta kadm.Metadata
	if err := withRetry(ctx, "fetch broker metadata", func() error {
		var metaErr error
		brokerMeta, metaErr = i.admin.Metadata(ctx)
		return metaErr
	}); err != nil {
		return nil, fmt.Errorf("failed to fetch broker metadata: %w", err)
	}

	for _, broker := range brokerMeta.Brokers {
		rack := ""
		if broker.Rack != nil {
			rack = *broker.Rack
		}
		metadata.Brokers = append(metadata.Brokers, BrokerInfo{
			ID:   broker.NodeID,
			Host: broker.Host,
			Port: broker.Port,
			Rack: rack,
		})
	}

	// Fetch topic metadata
	var topicDetails kadm.TopicDetails
	if err := withRetry(ctx, "list topics", func() error {
		var listErr error
		topicDetails, listErr = i.admin.ListTopics(ctx)
		return listErr
	}); err != nil {
		return nil, fmt.Errorf("failed to list topics: %w", err)
	}

	for topic, details := range topicDetails {
		// Calculate replication factor from first partition
		replicationFactor := 0
		if len(details.Partitions) > 0 {
			replicationFactor = len(details.Partitions[0].Replicas)
		}

		// Determine if it's a system/internal topic
		isInternal := strings.HasPrefix(topic, "__")

		metadata.Topics[topic] = &TopicInfo{
			Name:              topic,
			Partitions:        len(details.Partitions),
			ReplicationFactor: replicationFactor,
			Config:            make(map[string]string),
			Internal:          isInternal,
		}
	}

	// Fetch topic configurations
	topicNames := make([]string, 0, len(metadata.Topics))
	for name := range metadata.Topics {
		topicNames = append(topicNames, name)
	}

	configs, err := i.admin.DescribeTopicConfigs(ctx, topicNames...)
	if err != nil {
		// Non-fatal: continue without configs
		slog.Warn("failed to fetch topic configs", "error", err, "topic_count", len(topicNames))
	} else {
		for _, config := range configs {
			if topicInfo, exists := metadata.Topics[config.Name]; exists {
				for _, entry := range config.Configs {
					if entry.Value != nil {
						topicInfo.Config[entry.Key] = *entry.Value
					}
				}
			}
		}
	}

	// Fetch consumer groups
	var groups kadm.ListedGroups
	if err := withRetry(ctx, "list consumer groups", func() error {
		var groupErr error
		groups, groupErr = i.admin.ListGroups(ctx)
		return groupErr
	}); err != nil {
		return nil, fmt.Errorf("failed to list consumer groups: %w", err)
	}

	groupIDs := make([]string, 0, len(groups))
	for groupID := range groups {
		groupIDs = append(groupIDs, groupID)
	}

	if len(groupIDs) > 0 {
		describedGroups, err := i.admin.DescribeGroups(ctx, groupIDs...)
		if err != nil {
			// Non-fatal: continue without consumer group details
			slog.Warn("failed to describe consumer groups", "error", err, "consumer_group_count", len(groupIDs))
		} else {
			for _, described := range describedGroups.Sorted() {
				coordinator := int32(-1)
				if described.Coordinator.NodeID != -1 {
					coordinator = described.Coordinator.NodeID
				}

				metadata.ConsumerGroups[described.Group] = &ConsumerGroupInfo{
					GroupID:     described.Group,
					State:       described.State,
					Members:     len(described.Members),
					Topics:      []string{}, // Will be populated from offsets
					Lag:         make(map[string]int64),
					Coordinator: coordinator,
				}
			}
		}

		// Fetch committed offsets to determine which topics each group is consuming
		// Note: For Phase 1, we simplify lag calculation
		for _, groupID := range groupIDs {
			if groupInfo, exists := metadata.ConsumerGroups[groupID]; exists {
				// Fetch offsets for this specific group
				offsets, err := i.admin.FetchOffsets(ctx, groupID)
				if err != nil {
					// Non-fatal: skip this group
					continue
				}

				topicsSet := make(map[string]bool)

				// Iterate through the offset response
				for topic := range offsets {
					topicsSet[topic] = true
				}

				// Convert topics set to list
				topicList := make([]string, 0, len(topicsSet))
				for topic := range topicsSet {
					topicList = append(topicList, topic)
				}

				groupInfo.Topics = topicList
			}
		}
	}

	return metadata, nil
}

// buildSASL creates SASL authentication options based on the mechanism
func buildSASL(cfg Config) (kgo.Opt, error) {
	switch strings.ToUpper(cfg.AuthMechanism) {
	case "PLAIN":
		return kgo.SASL(plain.Auth{
			User: cfg.Username,
			Pass: cfg.Password,
		}.AsMechanism()), nil

	case "SCRAM-SHA-256":
		mechanism := scram.Auth{
			User: cfg.Username,
			Pass: cfg.Password,
		}.AsSha256Mechanism()
		return kgo.SASL(mechanism), nil

	case "SCRAM-SHA-512":
		mechanism := scram.Auth{
			User: cfg.Username,
			Pass: cfg.Password,
		}.AsSha512Mechanism()
		return kgo.SASL(mechanism), nil

	default:
		return nil, fmt.Errorf("unsupported SASL mechanism: %s", cfg.AuthMechanism)
	}
}

// buildTLS creates TLS configuration from the provided cert files
func buildTLS(cfg Config) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Load client certificate if provided
	if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Load CA certificate if provided
	if cfg.TLSCAFile != "" {
		caCert, err := os.ReadFile(cfg.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}

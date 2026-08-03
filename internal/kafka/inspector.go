package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

// WO-51: separate short timeout for the connection Ping. The metadata timeout
// (60s default) is for bulk reads; a Ping must fail fast on an unreachable host.
const pingTimeout = 10 * time.Second

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

	// Ping the cluster to verify connectivity (with retry for transient failures).
	//
	// WO-51: a Ping is a liveness check and must fail fast. The metadata timeout
	// (60s default) is for bulk reads on large clusters, not for "is this host
	// alive." Without a separate short timeout, an unreachable broker silently
	// dropping SYN packets makes the operator wait 60s for a connection error.
	pingCtx, pingCancel := context.WithTimeout(context.Background(), pingTimeout)
	defer pingCancel()

	if err := withRetry(pingCtx, "ping broker", func() error {
		return client.Ping(pingCtx)
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
	// WO-43: ListGroups has the same partial-shard contract as DescribeGroups —
	// kadm returns a populated map alongside a *ShardErrors when one broker is
	// unreachable. Hard-aborting here discarded every group the other brokers
	// returned and failed the whole command, so a single unreachable broker took
	// out an audit that two thirds of the cluster could have answered.
	var groups kadm.ListedGroups
	if err := withRetry(ctx, "list consumer groups", func() error {
		var groupErr error
		groups, groupErr = i.admin.ListGroups(ctx)
		return groupErr
	}); err != nil {
		if len(groups) == 0 {
			return nil, fmt.Errorf("failed to list consumer groups: %w", err)
		}
		slog.Warn("partial consumer group listing", "error", err, "listed_group_count", len(groups))
		metadata.ConsumerGroupReadErrors = append(metadata.ConsumerGroupReadErrors,
			fmt.Sprintf("list consumer groups (partial, %d listed): %v", len(groups), err))
	}

	groupIDs := make([]string, 0, len(groups))
	for groupID := range groups {
		groupIDs = append(groupIDs, groupID)
	}

	if len(groupIDs) > 0 {
		describedGroups, err := i.admin.DescribeGroups(ctx, groupIDs...)
		if err != nil {
			// WO-27: this read is NOT cosmetic. Without it no topic gets a
			// consumer attached and every topic looks unused, so record the
			// failure instead of silently continuing with an empty picture.
			slog.Warn("failed to describe consumer groups", "error", err, "consumer_group_count", len(groupIDs))
			metadata.ConsumerGroupReadErrors = append(metadata.ConsumerGroupReadErrors,
				fmt.Sprintf("describe consumer groups (%d groups): %v", len(groupIDs), err))
		}
		// WO-43: kadm returns BOTH a populated result and an error on a partial
		// shard failure, so `described` must be consumed even when err != nil.
		// Discarding it meant one unreachable coordinator blanked out consumer
		// data for the whole cluster when most of it was readable. The read is
		// still marked incomplete above — partial data never claims completeness.
		{
			for _, described := range describedGroups.Sorted() {
				// WO-27: DescribeGroups can succeed overall while individual
				// groups carry their own error. Such a group yields no topics,
				// which would silently look like "this group consumes nothing".
				if described.Err != nil {
					slog.Warn("failed to describe consumer group", "error", described.Err, "group_id", described.Group)
					metadata.ConsumerGroupReadErrors = append(metadata.ConsumerGroupReadErrors,
						fmt.Sprintf("describe group %q: %v", described.Group, described.Err))
					continue
				}

				coordinator := int32(-1)
				if described.Coordinator.NodeID != -1 {
					coordinator = described.Coordinator.NodeID
				}

				metadata.ConsumerGroups[described.Group] = &ConsumerGroupInfo{
					GroupID: described.Group,
					State:   described.State,
					Members: len(described.Members),
					// WO-28: seed from live member assignments so a group that
					// holds partitions but has not committed offsets yet still
					// marks its topics as consumed.
					Topics:      assignedTopics(described),
					Lag:         make(map[string]int64),
					Coordinator: coordinator,
				}
			}
		}

		// Fetch committed offsets to determine which topics each group is consuming.
		//
		// WO-45: this loop was sequential — one FetchOffsets call per group, one
		// at a time. On a cluster with 215 groups the total wall time exceeded
		// the default 10s timeout, producing 136 read errors and a degraded
		// scan. A bounded worker pool makes the total time scale with the
		// slowest single fetch, not the sum.
		i.fetchOffsetsConcurrently(ctx, groupIDs, metadata)

		// WO-56: compute lag efficiently using already-fetched data.
		// The previous code called i.admin.Lag() which internally re-fetched
		// DescribeGroups + FetchOffsets + ListEndOffsets — doubling broker
		// load and causing 86 of 216 groups to time out on production MSK.
		// We already have describedGroups and per-group offsets; the only
		// new data needed is end offsets (one batch call).
		i.computeLagEfficiently(ctx, describedGroups, groupIDs, metadata)
	}

	return metadata, nil
}

// fetchOffsetsWorkers caps concurrent FetchOffsets calls. WO-45: the sequential
// loop timed out on clusters with 200+ groups; 16 workers bring a 215-group
// cluster from ~3 minutes to ~15 seconds.
// WO-45: concurrent offset fetching worker pool
const fetchOffsetsWorkers = 16

// fetchOffsetsConcurrently fetches committed offsets for every group using a
// bounded worker pool.
//
// WO-45: replaces the sequential loop that timed out on large clusters. Each
// goroutine writes to its own pre-allocated results[idx] slot. After wg.Wait(),
// a single-threaded pass aggregates results into the shared metadata — no mutex
// is needed because all goroutines are done.
// WO-56: compute lag using already-fetched DescribeGroups and offsets plus
// a single batch ListEndOffsets call, instead of kadm.Lag() which re-fetches
// everything from scratch.
//
// WO-57: per-group lag errors are recorded so incomplete lag collection marks
// the scan as degraded.
func (i *Inspector) computeLagEfficiently(ctx context.Context, describedGroups kadm.DescribedGroups, groupIDs []string, metadata *ClusterMetadata) {
	// Collect all topic names that any group consumes for the end-offsets fetch.
	allTopicsSet := make(map[string]struct{})
	for _, info := range metadata.ConsumerGroups {
		for _, topic := range info.Topics {
			allTopicsSet[topic] = struct{}{}
		}
	}
	allTopics := make([]string, 0, len(allTopicsSet))
	for topic := range allTopicsSet {
		allTopics = append(allTopics, topic)
	}

	if len(allTopics) == 0 {
		return
	}

	// One batch call for end offsets — this is the only new network request.
	endOffsets, endOffsetsErr := i.admin.ListEndOffsets(ctx, allTopics...)
	if endOffsetsErr != nil {
		slog.Warn("failed to fetch end offsets for lag computation", "error", endOffsetsErr, "topic_count", len(allTopics))
		metadata.ConsumerGroupReadErrors = append(metadata.ConsumerGroupReadErrors,
			fmt.Sprintf("fetch end offsets (%d topics): %v", len(allTopics), endOffsetsErr))
		return
	}

	// For each described group, fetch its committed offsets and compute lag.
	for _, groupID := range groupIDs {
		described, exists := describedGroups[groupID]
		if !exists || described.Err != nil {
			continue
		}
		info, exists := metadata.ConsumerGroups[groupID]
		if !exists {
			continue
		}

		commits, err := i.admin.FetchOffsets(ctx, groupID)
		if err != nil {
			// WO-57: record per-group lag errors so the scan is marked incomplete.
			slog.Warn("failed to fetch offsets for lag", "error", err, "group_id", groupID)
			metadata.ConsumerGroupReadErrors = append(metadata.ConsumerGroupReadErrors,
				fmt.Sprintf("fetch offsets for lag %q: %v", groupID, err))
			continue
		}

		lag := kadm.CalculateGroupLag(described, commits, endOffsets)
		for _, topicLag := range lag.TotalByTopic().Sorted() {
			info.Lag[topicLag.Topic] = topicLag.Lag
		}
	}
}

// offsetFetcher fetches the set of topics a group has committed offsets for.
// WO-52: extracted as a function type so the concurrent path is testable
// without a live broker.
// WO-52: testable seam for concurrent offset fetching
type offsetFetcher func(ctx context.Context, groupID string) (map[string]struct{}, error)

// WO-45: concurrent offsets fetch via worker pool
func (i *Inspector) fetchOffsetsConcurrently(ctx context.Context, groupIDs []string, metadata *ClusterMetadata) {
	fetcher := func(ctx context.Context, groupID string) (map[string]struct{}, error) {
		offsets, err := i.admin.FetchOffsets(ctx, groupID)
		if err != nil {
			return nil, err
		}
		topics := make(map[string]struct{}, len(offsets))
		for topic := range offsets {
			topics[topic] = struct{}{}
		}
		return topics, nil
	}
	fetchOffsetsForGroups(ctx, groupIDs, metadata, fetcher)
}

// fetchOffsetsForGroups fetches committed offsets for every group using a
// bounded worker pool. WO-45: replaces the sequential loop. WO-52: takes a
// fetcher function so the concurrent path is testable without a live broker.
//
// Each goroutine writes to its own pre-allocated results[idx] slot. After
// wg.Wait(), a single-threaded pass aggregates results — no mutex needed.
// WO-45: bounded concurrent offset fetching with testable seam
func fetchOffsetsForGroups(ctx context.Context, groupIDs []string, metadata *ClusterMetadata, fetch offsetFetcher) {
	type result struct {
		groupID string
		topics  []string
		err     error
	}

	results := make([]result, len(groupIDs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, fetchOffsetsWorkers)

	for idx, groupID := range groupIDs {
		if _, exists := metadata.ConsumerGroups[groupID]; !exists {
			continue
		}

		wg.Add(1)
		go func(idx int, groupID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			offsetTopics, err := fetch(ctx, groupID)
			if err != nil {
				results[idx] = result{groupID: groupID, err: err}
				return
			}

			info := metadata.ConsumerGroups[groupID]
			topicsSet := make(map[string]struct{}, len(info.Topics)+len(offsetTopics))
			for _, topic := range info.Topics {
				topicsSet[topic] = struct{}{}
			}
			for topic := range offsetTopics {
				topicsSet[topic] = struct{}{}
			}

			topicList := make([]string, 0, len(topicsSet))
			for topic := range topicsSet {
				topicList = append(topicList, topic)
			}
			sort.Strings(topicList)

			results[idx] = result{groupID: groupID, topics: topicList}
		}(idx, groupID)
	}

	wg.Wait()

	// WO-54: all result aggregation is single-threaded after wg.Wait(). No mutex
	// is needed — the goroutines are done and each wrote to its own results slot.
	for _, r := range results {
		if r.err != nil {
			slog.Warn("failed to fetch consumer group offsets", "error", r.err, "group_id", r.groupID)
			metadata.ConsumerGroupReadErrors = append(metadata.ConsumerGroupReadErrors,
				fmt.Sprintf("fetch offsets for group %q: %v", r.groupID, r.err))
			continue
		}
		if info, exists := metadata.ConsumerGroups[r.groupID]; exists {
			info.Topics = r.topics
		}
	}
}

// assignedTopics returns the topics a group's live members currently hold.
//
// WO-28: topic attribution previously came only from committed offsets, so a
// group with live assignments but no commits yet — a consumer between rebalance
// and first commit, or one storing offsets externally — contributed no topics
// and its topics were reported unused.
func assignedTopics(described kadm.DescribedGroup) []string {
	assigned := described.AssignedPartitions()
	topics := make([]string, 0, len(assigned))
	for topic := range assigned {
		topics = append(topics, topic)
	}
	sort.Strings(topics)

	return topics
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

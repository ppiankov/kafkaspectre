package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ppiankov/kafkaspectre/internal/config"
	"github.com/ppiankov/kafkaspectre/internal/kafka"
	"github.com/ppiankov/kafkaspectre/internal/logging"
	"github.com/ppiankov/kafkaspectre/internal/reporter"
	"github.com/ppiankov/kafkaspectre/internal/scanner"
	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

const defaultQueryTimeout = 10 * time.Second

// Exit codes for structured error reporting.
const (
	ExitSuccess    = 0 // success
	ExitInternal   = 1 // internal error
	ExitInvalidArg = 2 // invalid arguments
	ExitNotFound   = 3 // not found (repo path, cluster unreachable)
	ExitNetwork    = 5 // network error (Kafka connection failures)
	ExitFindings   = 6 // findings detected (unused topics, check mismatches)
)

// FindingsError indicates the command succeeded but findings were detected.
type FindingsError struct {
	Count int
}

func (e *FindingsError) Error() string {
	return fmt.Sprintf("%d findings detected", e.Count)
}

func classifyError(err error) int {
	if err == nil {
		return ExitSuccess
	}

	var fe *FindingsError
	if errors.As(err, &fe) {
		return ExitFindings
	}

	if os.IsNotExist(err) {
		return ExitNotFound
	}

	msg := strings.ToLower(err.Error())

	if strings.Contains(msg, "not a directory") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "no such file") {
		return ExitNotFound
	}

	if strings.Contains(msg, "dial") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "network is unreachable") {
		return ExitNetwork
	}

	if strings.Contains(msg, "required") ||
		strings.Contains(msg, "invalid") ||
		strings.Contains(msg, "must be") ||
		strings.Contains(msg, "expected") {
		return ExitInvalidArg
	}

	return ExitInternal
}

func main() {
	logging.Init(false)

	if err := newRootCmd().Execute(); err != nil {
		exitCode := classifyError(err)
		var fe *FindingsError
		if errors.As(err, &fe) {
			slog.Info("findings detected", "count", fe.Count)
		} else {
			slog.Error("command failed", "error", err, "hint", "use 'kafkaspectre --help' for usage information")
		}
		os.Exit(exitCode)
	}
}

type auditOptions struct {
	bootstrapServer string
	authMechanism   string
	username        string
	password        string
	tlsEnabled      bool
	tlsCert         string
	tlsKey          string
	tlsCA           string
	output          string
	excludeInternal bool
	excludeTopics   []string
	includeManaged  bool
	timeout         time.Duration
}

type checkOptions struct {
	repo            string
	bootstrapServer string
	authMechanism   string
	username        string
	password        string
	tlsEnabled      bool
	tlsCert         string
	tlsKey          string
	tlsCA           string
	output          string
	excludeInternal bool
	excludeTopics   []string
	includeManaged  bool
	timeout         time.Duration
}

func newRootCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:           "kafkaspectre",
		Short:         "KafkaSpectre audits Kafka clusters for unused topics",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			logging.Init(verbose)
		},
	}

	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")

	cmd.AddCommand(newAuditCmd())
	cmd.AddCommand(newCheckCmd())
	cmd.AddCommand(newVersionCmd())

	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out, "version: %s\n", Version); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(out, "commit:  %s\n", GitCommit); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(out, "date:    %s\n", BuildDate); err != nil {
				return err
			}
			return nil
		},
	}
}

func newAuditCmd() *cobra.Command {
	var opts auditOptions

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit a Kafka cluster for unused topics",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveAuditOptions(cmd, opts)
			if err != nil {
				return err
			}
			return runAudit(cmd, resolved)
		},
	}

	registerConnectionFlags(cmd.Flags(), opts.connection())

	return cmd
}

func newCheckCmd() *cobra.Command {
	var opts checkOptions

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Scan a repository for topic references and compare with Kafka",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveCheckOptions(cmd, opts)
			if err != nil {
				return err
			}
			return runCheck(cmd, resolved)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.repo, "repo", "", "Path to repository to scan for topic references")
	registerConnectionFlags(flags, opts.connection())

	if err := cmd.MarkFlagRequired("repo"); err != nil {
		panic(err)
	}

	return cmd
}

func resolveAuditOptions(cmd *cobra.Command, opts auditOptions) (auditOptions, error) {
	if err := resolveConnectionOptions(cmd, opts.connection()); err != nil {
		return opts, err
	}

	patterns, err := normalizeExcludePatterns(opts.excludeTopics)
	if err != nil {
		return opts, err
	}
	opts.excludeTopics = patterns

	return opts, nil
}

func resolveCheckOptions(cmd *cobra.Command, opts checkOptions) (checkOptions, error) {
	if err := resolveConnectionOptions(cmd, opts.connection()); err != nil {
		return opts, err
	}

	patterns, err := normalizeExcludePatterns(opts.excludeTopics)
	if err != nil {
		return opts, err
	}
	opts.excludeTopics = patterns

	return opts, nil
}

// resolveConnectionOptions layers config file, environment, and defaults under
// whatever the operator passed explicitly.
//
// WO-36: resolveAuditOptions and resolveCheckOptions performed these same four
// steps against two identical-but-separate option structs.
func resolveConnectionOptions(cmd *cobra.Command, c connectionOptions) error {
	cfg, cfgPath, err := config.Load()
	if err != nil {
		return err
	}
	if cfg != nil {
		slog.Debug("loaded defaults from config", "path", cfgPath)
		applyConnectionConfigDefaults(cmd, cfg, c)
	}

	applyEnvCredentials(cmd, c)
	applyDefaultTimeout(cmd, c)

	return nil
}

func flagChanged(cmd *cobra.Command, name string) bool {
	if cmd == nil {
		return false
	}

	flag := cmd.Flags().Lookup(name)
	if flag == nil {
		return false
	}

	return flag.Changed
}

func runAudit(cmd *cobra.Command, opts auditOptions) error {
	start := time.Now()

	conn := opts.connection()

	excludePatterns, err := normalizeExcludePatterns(opts.excludeTopics)
	if err != nil {
		return err
	}

	output, err := resolvedOutput(opts.output)
	if err != nil {
		return err
	}
	if err := validateConnection(conn); err != nil {
		return err
	}

	kafkaCfg := buildKafkaConfig(conn)

	inspector, err := kafka.NewInspector(kafkaCfg)
	if err != nil {
		return err
	}
	defer inspector.Close()

	ctx, cancel := context.WithTimeout(cmd.Context(), kafkaCfg.QueryTimeout)
	defer cancel()

	slog.Info("connecting to Kafka", "bootstrap_servers", opts.bootstrapServer)

	metadata, err := inspector.FetchMetadata(ctx)
	if err != nil {
		return err
	}

	result := buildAuditResultWithOptions(metadata, opts.excludeInternal, excludePatterns, opts.includeManaged)
	result.Tool = "kafkaspectre"
	result.Version = Version
	result.Timestamp = time.Now().UTC().Format(time.RFC3339)

	if output == "text" {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "KafkaSpectre Audit\n")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Broker: %s\n", opts.bootstrapServer)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Topics: %d (internal excluded: %d)\n", result.Summary.TotalTopics, result.Summary.InternalTopics)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Consumer Groups: %d\n", result.Summary.TotalConsumerGroups)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "--------------------------------------------------\n")
		if err != nil {
			return err
		}
	}

	var generateErr error
	switch output {
	case "json":
		auditReporter := reporter.NewAuditJSONReporter(cmd.OutOrStdout(), false)
		generateErr = auditReporter.GenerateAudit(context.Background(), result)
	case "sarif":
		sarifReporter := reporter.NewSARIFReporter(cmd.OutOrStdout(), false)
		generateErr = sarifReporter.GenerateAudit(context.Background(), result)
	case "spectrehub":
		hubReporter := reporter.NewSpectreHubReporter(cmd.OutOrStdout(), opts.bootstrapServer)
		generateErr = hubReporter.GenerateAudit(context.Background(), result)
	case "text":
		auditReporter := reporter.NewAuditTextReporter(cmd.OutOrStdout(), false)
		generateErr = auditReporter.GenerateAudit(context.Background(), result)
	default:
		return fmt.Errorf("unsupported output format %q", output)
	}

	if generateErr != nil {
		return generateErr
	}

	if output == "text" && result.UnusedCount == 0 {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "\nNo issues detected. %d topics scanned.\n", result.Summary.TotalTopics)
		if err != nil {
			return err
		}
	}

	topicCount, partitionCount := metadataStats(metadata)
	slog.Info("audit completed",
		"topic_count", topicCount,
		"partition_count", partitionCount,
		"consumer_group_count", len(metadata.ConsumerGroups),
		"duration", time.Since(start),
	)

	if result.UnusedCount > 0 {
		return &FindingsError{Count: result.UnusedCount}
	}

	return nil
}

func runCheck(cmd *cobra.Command, opts checkOptions) error {
	start := time.Now()

	conn := opts.connection()

	excludePatterns, err := normalizeExcludePatterns(opts.excludeTopics)
	if err != nil {
		return err
	}

	output, err := resolvedOutput(opts.output)
	if err != nil {
		return err
	}
	if err := validateConnection(conn); err != nil {
		return err
	}
	if strings.TrimSpace(opts.repo) == "" {
		return errors.New("repo path is required")
	}

	repoPath, err := filepath.Abs(opts.repo)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}
	repoInfo, err := os.Stat(repoPath)
	if err != nil {
		return fmt.Errorf("repo path %q: %w", opts.repo, err)
	}
	if !repoInfo.IsDir() {
		return fmt.Errorf("repo path %q is not a directory", opts.repo)
	}

	kafkaCfg := buildKafkaConfig(conn)

	inspector, err := kafka.NewInspector(kafkaCfg)
	if err != nil {
		return err
	}
	defer inspector.Close()

	ctx, cancel := context.WithTimeout(cmd.Context(), kafkaCfg.QueryTimeout)
	defer cancel()

	slog.Info("connecting to Kafka", "bootstrap_servers", opts.bootstrapServer)

	metadata, err := inspector.FetchMetadata(ctx)
	if err != nil {
		return err
	}

	repoScanner := scanner.NewRepoScanner()
	scanResult, err := repoScanner.Scan(cmd.Context(), repoPath)
	if err != nil {
		return err
	}

	result := buildCheckResultWithOptions(scanResult, metadata, opts.excludeInternal, excludePatterns, opts.includeManaged)
	result.Tool = "kafkaspectre"
	result.Version = Version
	result.Timestamp = time.Now().UTC().Format(time.RFC3339)

	if output == "text" {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "KafkaSpectre Check\n")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Broker: %s\n", opts.bootstrapServer)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Repository: %s\n", opts.repo)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Cluster Topics: %d\n", result.Summary.ClusterTopics)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Repository Topics: %d\n", result.Summary.RepoTopics)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Total Consumer Groups: %d\n", len(metadata.ConsumerGroups))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "--------------------------------------------------\n")
		if err != nil {
			return err
		}
	}

	var generateErr error
	switch output {
	case "json":
		checkReporter := reporter.NewCheckJSONReporter(cmd.OutOrStdout(), false)
		generateErr = checkReporter.GenerateCheck(context.Background(), result)
	case "sarif":
		sarifReporter := reporter.NewSARIFReporter(cmd.OutOrStdout(), false)
		generateErr = sarifReporter.GenerateCheck(context.Background(), result)
	case "spectrehub":
		hubReporter := reporter.NewSpectreHubReporter(cmd.OutOrStdout(), opts.bootstrapServer)
		generateErr = hubReporter.GenerateCheck(context.Background(), result)
	case "text":
		checkReporter := reporter.NewCheckTextReporter(cmd.OutOrStdout())
		generateErr = checkReporter.GenerateCheck(context.Background(), result)
	default:
		return fmt.Errorf("unsupported output format %q", output)
	}

	if generateErr != nil {
		return generateErr
	}

	if output == "text" && result.Summary.TotalFindings == 0 {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "\nNo issues detected. %d topics scanned in repository and cluster.\n", result.Summary.RepoTopics+result.Summary.ClusterTopics)
		if err != nil {
			return err
		}
	}

	topicCount, partitionCount := metadataStats(metadata)
	slog.Info("check completed",
		"topic_count", topicCount,
		"partition_count", partitionCount,
		"consumer_group_count", len(metadata.ConsumerGroups),
		"duration", time.Since(start),
	)

	findingsCount := result.Summary.TotalFindings - result.Summary.OKCount
	if findingsCount > 0 {
		return &FindingsError{Count: findingsCount}
	}

	return nil
}

func buildAuditResult(metadata *kafka.ClusterMetadata, excludeInternal bool, excludeTopics []string) *reporter.AuditResult {
	return buildAuditResultWithOptions(metadata, excludeInternal, excludeTopics, false)
}

// buildAuditResultWithOptions classifies topics into unused and active sets.
//
// WO-27: when the consumer-group picture is incomplete, "no consumers found"
// is not evidence that a topic is unused, so no delete advice is emitted.
// WO-26: topics owned by a managed service are never deletion candidates.
func buildAuditResultWithOptions(metadata *kafka.ClusterMetadata, excludeInternal bool, excludeTopics []string, includeManaged bool) *reporter.AuditResult {
	consumersByTopic, abandonedByTopic := buildConsumersByTopicWithState(metadata)
	consumerDataComplete := metadata.ConsumerGroupsComplete()

	unusedTopics := make([]*reporter.UnusedTopic, 0)
	activeTopics := make([]*reporter.ActiveTopic, 0)

	internalTopics := 0
	managedHeldOut := 0
	managedTopics := make([]*reporter.UnusedTopic, 0)
	totalTopics := 0
	totalPartitions := 0
	unusedPartitions := 0
	activePartitions := 0
	highRisk := 0
	mediumRisk := 0
	lowRisk := 0

	for _, topic := range metadata.Topics {
		if topic.Internal {
			internalTopics++
			if excludeInternal {
				continue
			}
		}
		if shouldExcludeTopic(topic.Name, excludeTopics) {
			continue
		}
		// WO-26: managed topics are backing store for a live service. They are
		// reported separately and never count toward the analysis totals, so a
		// Schema Registry topic cannot inflate the unused percentage, the
		// savings pitch, or the exit code.
		//
		// --exclude-internal has already decided whether broker-internal topics
		// survive to here, so this check applies to ALL managed topics including
		// internal ones. One block, before the analyze counters, for both modes:
		// --include-managed surfaces them in the managed_topics list; otherwise
		// they are only counted so the hold-out is discoverable.
		if topic.IsManaged() {
			if includeManaged {
				risk, priority := classifyRisk(topic)
				recommendation := unusedRecommendation(topic, risk, consumerDataComplete)
				managed := reporter.BuildUnusedTopic(topic, unusedReason(topic, abandonedByTopic[topic.Name], consumerDataComplete), recommendation, risk, priority)
				managed.AbandonedConsumerGroups = abandonedByTopic[topic.Name]
				managedTopics = append(managedTopics, managed)
			} else {
				managedHeldOut++
			}
			continue
		}

		totalTopics++
		totalPartitions += topic.Partitions

		consumers := consumersByTopic[topic.Name]
		if len(consumers) == 0 {
			risk, priority := classifyRisk(topic)
			recommendation := unusedRecommendation(topic, risk, consumerDataComplete)
			unused := reporter.BuildUnusedTopic(topic, unusedReason(topic, abandonedByTopic[topic.Name], consumerDataComplete), recommendation, risk, priority)
			unused.AbandonedConsumerGroups = abandonedByTopic[topic.Name]

			unusedTopics = append(unusedTopics, unused)
			unusedPartitions += topic.Partitions
			switch risk {
			case "high":
				highRisk++
			case "medium":
				mediumRisk++
			case "low":
				lowRisk++
			}
		} else {
			activeTopics = append(activeTopics, reporter.BuildActiveTopic(topic, consumers))
			activePartitions += topic.Partitions
		}
	}

	// WO-31: order by risk descending at the source so the JSON, SARIF and
	// SpectreHub reporters inherit severity order too. Previously only the text
	// reporter re-sorted, leaving every machine-consumed output name-ordered.
	reporter.SortUnusedTopicsBySeverity(unusedTopics)
	reporter.SortUnusedTopicsBySeverity(managedTopics)
	sort.Slice(activeTopics, func(i, j int) bool {
		return activeTopics[i].Name < activeTopics[j].Name
	})

	unusedCount := len(unusedTopics)
	activeCount := len(activeTopics)
	unusedPercent := percent(unusedCount, totalTopics)
	unusedPartitionsPercent := percent(unusedPartitions, totalPartitions)

	internalExcluded := 0
	if excludeInternal {
		internalExcluded = internalTopics
	}

	clusterName := "unknown"
	if len(metadata.Brokers) > 0 {
		clusterName = metadata.Brokers[0].Host
	}

	summary := &reporter.AuditSummary{
		ClusterName:                  clusterName,
		TotalBrokers:                 len(metadata.Brokers),
		TotalTopicsIncludingInternal: len(metadata.Topics),
		TotalTopics:                  totalTopics,
		UnusedTopics:                 unusedCount,
		ActiveTopics:                 activeCount,
		InternalTopics:               internalExcluded,
		UnusedPercentage:             unusedPercent,
		TotalPartitions:              totalPartitions,
		UnusedPartitions:             unusedPartitions,
		ActivePartitions:             activePartitions,
		UnusedPartitionsPercent:      unusedPartitionsPercent,
		TotalConsumerGroups:          len(metadata.ConsumerGroups),
		HighRiskCount:                highRisk,
		MediumRiskCount:              mediumRisk,
		LowRiskCount:                 lowRisk,
		ManagedTopicsHeldOut:         managedHeldOut,
		RecommendedCleanup:           recommendedCleanup(unusedTopics, 10, consumerDataComplete),
		ClusterHealthScore:           clusterHealthScore(unusedPercent),
		PotentialSavingsInfo:         fmt.Sprintf("%d unused topics representing %d partitions (%.1f%% of total partitions)", unusedCount, unusedPartitions, unusedPartitionsPercent),
	}

	return &reporter.AuditResult{
		Summary:       summary,
		UnusedTopics:  unusedTopics,
		ManagedTopics: managedTopics,
		ActiveTopics:  activeTopics,
		Metadata:      metadata,
		TotalTopics:   totalTopics,
		UnusedCount:   unusedCount,
		ActiveCount:   activeCount,
		InternalCount: internalTopics,
		Reliability: reporter.ScanReliability{
			ConsumerGroupsComplete: consumerDataComplete,
			ReadErrors:             readErrors(metadata),
		},
	}
}

func readErrors(metadata *kafka.ClusterMetadata) []string {
	if metadata == nil {
		return nil
	}
	return append([]string(nil), metadata.ConsumerGroupReadErrors...)
}

// unusedReason explains why a topic has no consumers, distinguishing a genuine
// absence from an unreadable cluster and from abandoned-only consumption.
//
// WO-27/WO-29: "No consumer groups found" was previously emitted for all three
// cases, which are operationally very different.
func unusedReason(topic *kafka.TopicInfo, abandoned []string, consumerDataComplete bool) string {
	if !consumerDataComplete {
		return "Consumer group data could not be read; unused status is UNVERIFIED"
	}
	if len(abandoned) > 0 {
		return fmt.Sprintf("No active consumer groups; %d abandoned group(s) reference this topic", len(abandoned))
	}
	if owner := topic.ManagedOwner(); owner != kafka.OwnerNone {
		return fmt.Sprintf("No consumer groups found; topic is managed by %s", owner)
	}
	return "No consumer groups found"
}

// doNotDeletePrefix marks a recommendation that forbids deletion. It is the
// single token both the per-topic advice and the summary cleanup list key on.
//
// WO-39: the two used to be decided independently and disagreed.
const doNotDeletePrefix = "DO NOT DELETE"

// doNotActAdvice is the recommendation emitted when the scan was degraded.
const doNotActAdvice = "Do not act on this finding — re-run once the cluster is fully readable"

// deletable reports whether a finding's own recommendation permits deletion.
//
// WO-39: the invariant is that nothing may name a topic for cleanup unless this
// returns true for it, so the report cannot contradict itself.
func deletable(topic *reporter.UnusedTopic) bool {
	if topic == nil || topic.ManagedBy != "" {
		return false
	}
	if strings.HasPrefix(topic.Recommendation, doNotDeletePrefix) {
		return false
	}
	return topic.Recommendation != doNotActAdvice
}

// unusedRecommendation decides what to advise for a topic with no consumers.
//
// WO-26: a managed topic is never a deletion candidate regardless of risk score.
// WO-27: an unverified reading must not carry deletion advice at all.
func unusedRecommendation(topic *kafka.TopicInfo, risk string, consumerDataComplete bool) string {
	if owner := topic.ManagedOwner(); owner != kafka.OwnerNone {
		return fmt.Sprintf("%s — backing store for %s", doNotDeletePrefix, owner)
	}
	if !consumerDataComplete {
		return doNotActAdvice
	}
	return recommendationForRisk(risk)
}

func buildConsumersByTopic(metadata *kafka.ClusterMetadata) map[string][]string {
	active, _ := buildConsumersByTopicWithState(metadata)
	return active
}

// buildConsumersByTopicWithState splits each topic's consumer groups into those
// with live members and those that are abandoned.
//
// WO-29: ConsumerGroupInfo.State was captured but never read, so a group in
// Empty or Dead state marked its topics ACTIVE. An abandoned group holding
// stale offsets is evidence a topic is NO LONGER consumed; treating it as an
// active consumer inverted the signal and hid the topic from the unused list.
func buildConsumersByTopicWithState(metadata *kafka.ClusterMetadata) (active map[string][]string, abandoned map[string][]string) {
	activeSet := make(map[string]map[string]struct{})
	abandonedSet := make(map[string]map[string]struct{})

	for _, group := range metadata.ConsumerGroups {
		target := activeSet
		if group.IsAbandoned() {
			target = abandonedSet
		}
		for _, topic := range group.Topics {
			if _, ok := target[topic]; !ok {
				target[topic] = make(map[string]struct{})
			}
			target[topic][group.GroupID] = struct{}{}
		}
	}

	return flattenGroupSets(activeSet), flattenGroupSets(abandonedSet)
}

func flattenGroupSets(sets map[string]map[string]struct{}) map[string][]string {
	out := make(map[string][]string, len(sets))
	for topic, groups := range sets {
		list := make([]string, 0, len(groups))
		for group := range groups {
			list = append(list, group)
		}
		sort.Strings(list)
		out[topic] = list
	}

	return out
}

func buildCheckResult(scanResult *scanner.Result, metadata *kafka.ClusterMetadata, excludeInternal bool, excludeTopics []string) *reporter.CheckResult {
	return buildCheckResultWithOptions(scanResult, metadata, excludeInternal, excludeTopics, false)
}

func buildCheckResultWithOptions(scanResult *scanner.Result, metadata *kafka.ClusterMetadata, excludeInternal bool, excludeTopics []string, includeManaged bool) *reporter.CheckResult {
	consumersByTopic := buildConsumersByTopic(metadata)
	consumerDataComplete := metadata.ConsumerGroupsComplete()

	clusterTopics := make(map[string]*kafka.TopicInfo, len(metadata.Topics))
	for name, topic := range metadata.Topics {
		if topic.Internal && excludeInternal {
			continue
		}
		if shouldExcludeTopic(name, excludeTopics) {
			continue
		}
		clusterTopics[name] = topic
	}

	repoTopics := make(map[string]*scanner.TopicReference, len(scanResult.Topics))
	for topic, ref := range scanResult.Topics {
		if shouldExcludeTopic(topic, excludeTopics) {
			continue
		}
		repoTopics[topic] = ref
	}

	// heldOut reports whether a managed topic should be dropped from the union.
	//
	// WO-42 originally filtered the repo side unconditionally, which had two
	// faults. It used a different predicate from the cluster side (missing the
	// !Internal companion), so a repo reference to __consumer_offsets became a
	// false UNREFERENCED_IN_REPO. And it suppressed the GENUINE case: a Connect
	// worker pointing at a backing topic that was never created is a real
	// MISSING_IN_CLUSTER finding worth keeping. A managed topic is only
	// uninteresting once we have confirmed it exists.
	heldOut := func(name string, topic *kafka.TopicInfo) bool {
		if includeManaged {
			return false
		}
		if topic == nil || !topic.IsManaged() || topic.Internal {
			return false
		}
		return true
	}

	allTopics := make(map[string]struct{}, len(clusterTopics)+len(repoTopics))
	for topic := range repoTopics {
		// A managed topic confirmed present in the cluster needs no report; one
		// that is referenced but ABSENT is a genuine missing-topic finding.
		if _, inCluster := clusterTopics[topic]; inCluster && heldOut(topic, clusterTopics[topic]) {
			continue
		}
		allTopics[topic] = struct{}{}
	}
	for topic := range clusterTopics {
		if heldOut(topic, clusterTopics[topic]) {
			continue
		}
		allTopics[topic] = struct{}{}
	}

	names := make([]string, 0, len(allTopics))
	for topic := range allTopics {
		names = append(names, topic)
	}
	sort.Strings(names)

	findings := make([]*reporter.CheckFinding, 0, len(names))
	summary := &reporter.CheckSummary{
		RepoPath:      scanResult.RepoPath,
		FilesScanned:  scanResult.FilesScanned,
		RepoTopics:    len(repoTopics),
		ClusterTopics: len(clusterTopics),
		TotalFindings: len(names),
	}

	for _, topic := range names {
		repoRef, referencedInRepo := repoTopics[topic]
		_, inCluster := clusterTopics[topic]
		consumerGroups := append([]string(nil), consumersByTopic[topic]...)
		hasConsumers := inCluster && len(consumerGroups) > 0

		status, reason := classifyCheckStatus(referencedInRepo, inCluster, hasConsumers, consumerDataComplete)
		finding := &reporter.CheckFinding{
			Topic:            topic,
			Status:           status,
			ReferencedInRepo: referencedInRepo,
			InCluster:        inCluster,
			ConsumerGroups:   consumerGroups,
			Reason:           reason,
		}
		if repoRef != nil {
			finding.References = convertCheckReferences(repoRef.Occurrences)
		}

		findings = append(findings, finding)

		switch status {
		case reporter.CheckStatusOK:
			summary.OKCount++
		case reporter.CheckStatusMissingInCluster:
			summary.MissingInClusterCount++
		case reporter.CheckStatusUnreferencedInRepo:
			summary.UnreferencedInRepoCount++
		case reporter.CheckStatusUnused:
			summary.UnusedCount++
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]
		if left.Status != right.Status {
			return checkStatusSortValue(left.Status) < checkStatusSortValue(right.Status)
		}
		return left.Topic < right.Topic
	})

	return &reporter.CheckResult{
		Summary:  summary,
		Findings: findings,
		// WO-38: the check path previously had no reliability signal at all, so
		// a failed DescribeGroups made it report every cluster topic as UNUSED
		// with a confident reason and exit 6 — the same fail-open bug WO-27
		// fixed for audit, still live on this surface.
		Reliability: reporter.ScanReliability{
			ConsumerGroupsComplete: consumerDataComplete,
			ReadErrors:             readErrors(metadata),
		},
	}
}

func convertCheckReferences(refs []scanner.Reference) []reporter.CheckReference {
	out := make([]reporter.CheckReference, 0, len(refs))
	for _, ref := range refs {
		out = append(out, reporter.CheckReference{
			File:   ref.File,
			Line:   ref.Line,
			Source: ref.Source,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Source < out[j].Source
	})

	return out
}

func normalizeExcludePatterns(patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, nil
	}

	normalized := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}

		if _, err := path.Match(pattern, "topic"); err != nil {
			return nil, fmt.Errorf("invalid exclude topic pattern %q: %w", pattern, err)
		}

		normalized = append(normalized, pattern)
	}

	if len(normalized) == 0 {
		return nil, nil
	}

	return normalized, nil
}

func shouldExcludeTopic(topic string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := path.Match(pattern, topic)
		if err != nil {
			continue
		}
		if matched {
			return true
		}
	}

	return false
}

func metadataStats(metadata *kafka.ClusterMetadata) (topicCount int, partitionCount int) {
	if metadata == nil {
		return 0, 0
	}

	topicCount = len(metadata.Topics)
	for _, topic := range metadata.Topics {
		partitionCount += topic.Partitions
	}

	return topicCount, partitionCount
}

// classifyCheckStatus decides a topic's check status and the reason shown.
//
// WO-38: the UNUSED reasons asserted "has no active consumer groups" as fact.
// When the consumer-group read failed the tool saw zero groups for every topic,
// so that sentence was a confident claim about data it never read.
func classifyCheckStatus(referencedInRepo, inCluster, hasConsumers, consumerDataComplete bool) (reporter.CheckStatus, string) {
	switch {
	case referencedInRepo && !inCluster:
		return reporter.CheckStatusMissingInCluster, "topic is referenced in code but does not exist in cluster"
	case inCluster && !hasConsumers:
		if !consumerDataComplete {
			return reporter.CheckStatusUnused, "consumer group data could not be read; unused status is UNVERIFIED"
		}
		if referencedInRepo {
			return reporter.CheckStatusUnused, "topic is referenced in code and exists in cluster but has no active consumer groups"
		}
		return reporter.CheckStatusUnused, "topic exists in cluster but has no active consumer groups"
	case !referencedInRepo && inCluster:
		return reporter.CheckStatusUnreferencedInRepo, "topic exists in cluster with consumers but was not found in repository"
	default:
		return reporter.CheckStatusOK, "topic exists in cluster and has active consumers"
	}
}

func checkStatusSortValue(status reporter.CheckStatus) int {
	switch status {
	case reporter.CheckStatusMissingInCluster:
		return 0
	case reporter.CheckStatusUnused:
		return 1
	case reporter.CheckStatusUnreferencedInRepo:
		return 2
	case reporter.CheckStatusOK:
		return 3
	default:
		return 4
	}
}

func classifyRisk(topic *kafka.TopicInfo) (string, int) {
	if topic.Partitions >= 10 || topic.ReplicationFactor >= 3 {
		return "high", 3
	}
	if topic.Partitions >= 2 || topic.ReplicationFactor == 2 {
		return "medium", 2
	}
	return "low", 1
}

func recommendationForRisk(risk string) string {
	switch risk {
	case "low":
		return "Safe to delete after confirmation"
	case "medium":
		return "Review before deletion"
	case "high":
		return "Investigate before deletion"
	default:
		return "Review before deletion"
	}
}

// recommendedCleanup names the topics safest to delete first.
//
// This list is the field automation reads — docs/cleanup-guide.md builds delete
// lists from it. It must therefore never contain a topic whose own
// recommendation forbids deletion.
//
// WO-39: it previously took the raw unused slice, so a default `audit --output
// json` listed __consumer_offsets for cleanup while the same document said
// "DO NOT DELETE — backing store for Kafka broker" for that topic, and a
// degraded scan published a named delete list built from a cluster it could not
// read. Deletability is decided once, by deletable(), and both the per-topic
// recommendation and this list derive from it.
func recommendedCleanup(unused []*reporter.UnusedTopic, limit int, consumerDataComplete bool) []string {
	if len(unused) == 0 || limit <= 0 || !consumerDataComplete {
		return nil
	}

	candidates := make([]*reporter.UnusedTopic, 0, len(unused))
	for _, topic := range unused {
		if deletable(topic) {
			candidates = append(candidates, topic)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CleanupPriority != candidates[j].CleanupPriority {
			return candidates[i].CleanupPriority < candidates[j].CleanupPriority
		}
		if candidates[i].Risk != candidates[j].Risk {
			return candidates[i].Risk < candidates[j].Risk
		}
		return candidates[i].Name < candidates[j].Name
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	names := make([]string, len(candidates))
	for i, topic := range candidates {
		names[i] = topic.Name
	}
	return names
}

func clusterHealthScore(unusedPercent float64) string {
	switch {
	case unusedPercent <= 10:
		return "excellent"
	case unusedPercent <= 25:
		return "good"
	case unusedPercent <= 50:
		return "fair"
	case unusedPercent <= 75:
		return "poor"
	default:
		return "critical"
	}
}

func percent(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return (float64(numerator) / float64(denominator)) * 100
}

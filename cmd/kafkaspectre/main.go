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

	flags := cmd.Flags()
	flags.StringVar(&opts.bootstrapServer, "bootstrap-server", "", "Kafka bootstrap server(s) (host:port, comma-separated)")
	flags.StringVar(&opts.authMechanism, "auth-mechanism", "", "SASL mechanism (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)")
	flags.StringVar(&opts.username, "username", "", "SASL username")
	flags.StringVar(&opts.password, "password", "", "SASL password")
	flags.BoolVar(&opts.tlsEnabled, "tls", false, "Enable TLS")
	flags.StringVar(&opts.tlsCert, "tls-cert", "", "Path to TLS client certificate")
	flags.StringVar(&opts.tlsKey, "tls-key", "", "Path to TLS client private key")
	flags.StringVar(&opts.tlsCA, "tls-ca", "", "Path to TLS CA certificate")
	flags.StringVar(&opts.output, "output", "text", "Output format (json|sarif|spectrehub|text)")
	flags.BoolVar(&opts.excludeInternal, "exclude-internal", false, "Exclude internal topics from analysis")
	flags.StringSliceVar(&opts.excludeTopics, "exclude-topics", nil, "Exclude topics by name or glob pattern (repeatable)")
	flags.BoolVar(&opts.includeManaged, "include-managed", false, "Include service-managed topics (Schema Registry, Connect) in analysis")
	flags.DurationVar(&opts.timeout, "timeout", 0, "Kafka query timeout (for example: 10s, 1m)")

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
	flags.StringVar(&opts.bootstrapServer, "bootstrap-server", "", "Kafka bootstrap server(s) (host:port, comma-separated)")
	flags.StringVar(&opts.authMechanism, "auth-mechanism", "", "SASL mechanism (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)")
	flags.StringVar(&opts.username, "username", "", "SASL username")
	flags.StringVar(&opts.password, "password", "", "SASL password")
	flags.BoolVar(&opts.tlsEnabled, "tls", false, "Enable TLS")
	flags.StringVar(&opts.tlsCert, "tls-cert", "", "Path to TLS client certificate")
	flags.StringVar(&opts.tlsKey, "tls-key", "", "Path to TLS client private key")
	flags.StringVar(&opts.tlsCA, "tls-ca", "", "Path to TLS CA certificate")
	flags.StringVar(&opts.output, "output", "text", "Output format (json|sarif|spectrehub|text)")
	flags.BoolVar(&opts.excludeInternal, "exclude-internal", false, "Exclude internal topics from analysis")
	flags.StringSliceVar(&opts.excludeTopics, "exclude-topics", nil, "Exclude topics by name or glob pattern (repeatable)")
	flags.BoolVar(&opts.includeManaged, "include-managed", false, "Include service-managed topics (Schema Registry, Connect) in analysis")
	flags.DurationVar(&opts.timeout, "timeout", 0, "Kafka query timeout (for example: 10s, 1m)")

	if err := cmd.MarkFlagRequired("repo"); err != nil {
		panic(err)
	}

	return cmd
}

func resolveAuditOptions(cmd *cobra.Command, opts auditOptions) (auditOptions, error) {
	cfg, cfgPath, err := config.Load()
	if err != nil {
		return opts, err
	}
	if cfg != nil {
		slog.Debug("loaded defaults from config", "path", cfgPath)
		opts = applyAuditConfigDefaults(cmd, opts, cfg)
	}

	patterns, err := normalizeExcludePatterns(opts.excludeTopics)
	if err != nil {
		return opts, err
	}
	opts.excludeTopics = patterns

	// WO-34: credentials come from the environment, never the config file, so a
	// secured cluster works without repeating --username/--password each run.
	if !flagChanged(cmd, "username") && opts.username == "" {
		opts.username, _ = config.CredentialsFromEnv()
	}
	if !flagChanged(cmd, "password") && opts.password == "" {
		_, opts.password = config.CredentialsFromEnv()
	}

	// WO-37: only substitute the default when the flag was genuinely absent.
	// Keying on `timeout == 0` made an explicit `--timeout 0` indistinguishable
	// from "not set", so it was silently rewritten to 10s and the
	// "timeout must be greater than zero" guard was unreachable.
	if opts.timeout == 0 && !flagChanged(cmd, "timeout") {
		opts.timeout = defaultQueryTimeout
	}

	return opts, nil
}

func resolveCheckOptions(cmd *cobra.Command, opts checkOptions) (checkOptions, error) {
	cfg, cfgPath, err := config.Load()
	if err != nil {
		return opts, err
	}
	if cfg != nil {
		slog.Debug("loaded defaults from config", "path", cfgPath)
		opts = applyCheckConfigDefaults(cmd, opts, cfg)
	}

	patterns, err := normalizeExcludePatterns(opts.excludeTopics)
	if err != nil {
		return opts, err
	}
	opts.excludeTopics = patterns

	// WO-34: see resolveAuditOptions — credentials are environment-sourced.
	if !flagChanged(cmd, "username") && opts.username == "" {
		opts.username, _ = config.CredentialsFromEnv()
	}
	if !flagChanged(cmd, "password") && opts.password == "" {
		_, opts.password = config.CredentialsFromEnv()
	}

	// WO-37: see resolveAuditOptions — an explicit --timeout 0 must reach the
	// validation guard rather than being replaced by the default.
	if opts.timeout == 0 && !flagChanged(cmd, "timeout") {
		opts.timeout = defaultQueryTimeout
	}

	return opts, nil
}

func applyAuditConfigDefaults(cmd *cobra.Command, opts auditOptions, cfg *config.Config) auditOptions {
	if !flagChanged(cmd, "bootstrap-server") && strings.TrimSpace(opts.bootstrapServer) == "" && strings.TrimSpace(cfg.BootstrapServers) != "" {
		opts.bootstrapServer = cfg.BootstrapServers
	}
	if !flagChanged(cmd, "auth-mechanism") && strings.TrimSpace(opts.authMechanism) == "" && strings.TrimSpace(cfg.AuthMechanism) != "" {
		opts.authMechanism = cfg.AuthMechanism
	}
	if !flagChanged(cmd, "output") && strings.TrimSpace(cfg.Format) != "" {
		opts.output = cfg.Format
	}
	if !flagChanged(cmd, "exclude-internal") && cfg.ExcludeInternal != nil {
		opts.excludeInternal = *cfg.ExcludeInternal
	}
	if !flagChanged(cmd, "exclude-topics") && len(cfg.ExcludeTopics) > 0 {
		opts.excludeTopics = append([]string(nil), cfg.ExcludeTopics...)
	}
	if !flagChanged(cmd, "timeout") && cfg.HasTimeout {
		opts.timeout = cfg.Timeout
	}
	// WO-34: TLS material completes the connection surface so a secured cluster
	// can be expressed in config instead of on every command line.
	if !flagChanged(cmd, "tls") && cfg.TLSEnabled != nil {
		opts.tlsEnabled = *cfg.TLSEnabled
	}
	if !flagChanged(cmd, "tls-cert") && strings.TrimSpace(cfg.TLSCertFile) != "" {
		opts.tlsCert = cfg.TLSCertFile
	}
	if !flagChanged(cmd, "tls-key") && strings.TrimSpace(cfg.TLSKeyFile) != "" {
		opts.tlsKey = cfg.TLSKeyFile
	}
	if !flagChanged(cmd, "tls-ca") && strings.TrimSpace(cfg.TLSCAFile) != "" {
		opts.tlsCA = cfg.TLSCAFile
	}

	return opts
}

func applyCheckConfigDefaults(cmd *cobra.Command, opts checkOptions, cfg *config.Config) checkOptions {
	if !flagChanged(cmd, "bootstrap-server") && strings.TrimSpace(opts.bootstrapServer) == "" && strings.TrimSpace(cfg.BootstrapServers) != "" {
		opts.bootstrapServer = cfg.BootstrapServers
	}
	if !flagChanged(cmd, "auth-mechanism") && strings.TrimSpace(opts.authMechanism) == "" && strings.TrimSpace(cfg.AuthMechanism) != "" {
		opts.authMechanism = cfg.AuthMechanism
	}
	if !flagChanged(cmd, "output") && strings.TrimSpace(cfg.Format) != "" {
		opts.output = cfg.Format
	}
	if !flagChanged(cmd, "exclude-internal") && cfg.ExcludeInternal != nil {
		opts.excludeInternal = *cfg.ExcludeInternal
	}
	if !flagChanged(cmd, "exclude-topics") && len(cfg.ExcludeTopics) > 0 {
		opts.excludeTopics = append([]string(nil), cfg.ExcludeTopics...)
	}
	if !flagChanged(cmd, "timeout") && cfg.HasTimeout {
		opts.timeout = cfg.Timeout
	}
	// WO-34: TLS material completes the connection surface so a secured cluster
	// can be expressed in config instead of on every command line.
	if !flagChanged(cmd, "tls") && cfg.TLSEnabled != nil {
		opts.tlsEnabled = *cfg.TLSEnabled
	}
	if !flagChanged(cmd, "tls-cert") && strings.TrimSpace(cfg.TLSCertFile) != "" {
		opts.tlsCert = cfg.TLSCertFile
	}
	if !flagChanged(cmd, "tls-key") && strings.TrimSpace(cfg.TLSKeyFile) != "" {
		opts.tlsKey = cfg.TLSKeyFile
	}
	if !flagChanged(cmd, "tls-ca") && strings.TrimSpace(cfg.TLSCAFile) != "" {
		opts.tlsCA = cfg.TLSCAFile
	}

	return opts
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

	if strings.TrimSpace(opts.bootstrapServer) == "" {
		return errors.New("bootstrap-server is required")
	}

	excludePatterns, err := normalizeExcludePatterns(opts.excludeTopics)
	if err != nil {
		return err
	}

	output := strings.ToLower(strings.TrimSpace(opts.output))
	if output == "" {
		output = "text"
	}
	if output != "json" && output != "sarif" && output != "spectrehub" && output != "text" {
		return fmt.Errorf("invalid output format %q (expected json, sarif, spectrehub, or text)", opts.output)
	}
	if opts.authMechanism != "" && (opts.username == "" || opts.password == "") {
		return errors.New("auth-mechanism requires both --username and --password")
	}
	if (opts.tlsCert == "") != (opts.tlsKey == "") {
		return errors.New("--tls-cert and --tls-key must be provided together")
	}
	if opts.timeout <= 0 {
		return errors.New("timeout must be greater than zero")
	}

	kafkaCfg := kafka.Config{
		BootstrapServers: opts.bootstrapServer,
		AuthMechanism:    opts.authMechanism,
		Username:         opts.username,
		Password:         opts.password,
		TLSEnabled:       opts.tlsEnabled,
		TLSCertFile:      opts.tlsCert,
		TLSKeyFile:       opts.tlsKey,
		TLSCAFile:        opts.tlsCA,
		QueryTimeout:     opts.timeout,
	}

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

	if strings.TrimSpace(opts.bootstrapServer) == "" {
		return errors.New("bootstrap-server is required")
	}
	excludePatterns, err := normalizeExcludePatterns(opts.excludeTopics)
	if err != nil {
		return err
	}

	output := strings.ToLower(strings.TrimSpace(opts.output))
	if output == "" {
		output = "text"
	}
	if output != "json" && output != "sarif" && output != "spectrehub" && output != "text" {
		return fmt.Errorf("invalid output format %q (expected json, sarif, spectrehub, or text)", opts.output)
	}
	if opts.authMechanism != "" && (opts.username == "" || opts.password == "") {
		return errors.New("auth-mechanism requires both --username and --password")
	}
	if (opts.tlsCert == "") != (opts.tlsKey == "") {
		return errors.New("--tls-cert and --tls-key must be provided together")
	}
	if opts.timeout <= 0 {
		return errors.New("timeout must be greater than zero")
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

	kafkaCfg := kafka.Config{
		BootstrapServers: opts.bootstrapServer,
		AuthMechanism:    opts.authMechanism,
		Username:         opts.username,
		Password:         opts.password,
		TLSEnabled:       opts.tlsEnabled,
		TLSCertFile:      opts.tlsCert,
		TLSKeyFile:       opts.tlsKey,
		TLSCAFile:        opts.tlsCA,
		QueryTimeout:     opts.timeout,
	}

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
	managedTopics := 0
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
		// WO-26: managed topics are backing store for a live service, so listing
		// them as cleanup candidates is actively dangerous. Broker-internal
		// ("__") topics are deliberately NOT covered here — they are already
		// governed by --exclude-internal, and overriding that would silently
		// change the meaning of an existing flag.
		if topic.IsManaged() && !topic.Internal && !includeManaged {
			managedTopics++
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
		RecommendedCleanup:           recommendedCleanup(unusedTopics, 10),
		ClusterHealthScore:           clusterHealthScore(unusedPercent),
		PotentialSavingsInfo:         fmt.Sprintf("%d unused topics representing %d partitions (%.1f%% of total partitions)", unusedCount, unusedPartitions, unusedPartitionsPercent),
	}

	return &reporter.AuditResult{
		Summary:       summary,
		UnusedTopics:  unusedTopics,
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

// unusedRecommendation decides what to advise for a topic with no consumers.
//
// WO-26: a managed topic is never a deletion candidate regardless of risk score.
// WO-27: an unverified reading must not carry deletion advice at all.
func unusedRecommendation(topic *kafka.TopicInfo, risk string, consumerDataComplete bool) string {
	if owner := topic.ManagedOwner(); owner != kafka.OwnerNone {
		return fmt.Sprintf("DO NOT DELETE — backing store for %s", owner)
	}
	if !consumerDataComplete {
		return "Do not act on this finding — re-run once the cluster is fully readable"
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

	clusterTopics := make(map[string]*kafka.TopicInfo, len(metadata.Topics))
	for name, topic := range metadata.Topics {
		if topic.Internal && excludeInternal {
			continue
		}
		if shouldExcludeTopic(name, excludeTopics) {
			continue
		}
		// WO-26: a Schema Registry or Connect backing topic is never a repo
		// reference gap, and must not be reported as an unused topic here
		// either. Broker-internal topics stay under --exclude-internal.
		if topic.IsManaged() && !topic.Internal && !includeManaged {
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

	allTopics := make(map[string]struct{}, len(clusterTopics)+len(repoTopics))
	for topic := range repoTopics {
		allTopics[topic] = struct{}{}
	}
	for topic := range clusterTopics {
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

		status, reason := classifyCheckStatus(referencedInRepo, inCluster, hasConsumers)
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

func classifyCheckStatus(referencedInRepo, inCluster, hasConsumers bool) (reporter.CheckStatus, string) {
	switch {
	case referencedInRepo && !inCluster:
		return reporter.CheckStatusMissingInCluster, "topic is referenced in code but does not exist in cluster"
	case inCluster && !hasConsumers:
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

func recommendedCleanup(unused []*reporter.UnusedTopic, limit int) []string {
	if len(unused) == 0 || limit <= 0 {
		return nil
	}

	candidates := make([]*reporter.UnusedTopic, len(unused))
	copy(candidates, unused)

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

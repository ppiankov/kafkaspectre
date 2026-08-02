package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ppiankov/kafkaspectre/internal/config"
	"github.com/ppiankov/kafkaspectre/internal/kafka"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// connectionOptions is a pointer view over the connection and output fields
// that audit and check both carry.
//
// WO-36: those fields, their flag registrations, their config-defaults
// application, their validation, and their kafka.Config construction were each
// written twice — once per command. A change to any shared concern had to be
// made in both places, and the second site is the one that gets forgotten;
// WO-31 is a concrete instance of exactly that divergence.
//
// A pointer view rather than an embedded struct keeps auditOptions and
// checkOptions plain value types, so existing callers and tests that build them
// with field literals are unaffected.
type connectionOptions struct {
	bootstrapServer *string
	authMechanism   *string
	username        *string
	password        *string
	tlsEnabled      *bool
	tlsCert         *string
	tlsKey          *string
	tlsCA           *string
	output          *string
	excludeInternal *bool
	excludeTopics   *[]string
	includeManaged  *bool
	timeout         *time.Duration
}

func (o *auditOptions) connection() connectionOptions {
	return connectionOptions{
		bootstrapServer: &o.bootstrapServer,
		authMechanism:   &o.authMechanism,
		username:        &o.username,
		password:        &o.password,
		tlsEnabled:      &o.tlsEnabled,
		tlsCert:         &o.tlsCert,
		tlsKey:          &o.tlsKey,
		tlsCA:           &o.tlsCA,
		output:          &o.output,
		excludeInternal: &o.excludeInternal,
		excludeTopics:   &o.excludeTopics,
		includeManaged:  &o.includeManaged,
		timeout:         &o.timeout,
	}
}

func (o *checkOptions) connection() connectionOptions {
	return connectionOptions{
		bootstrapServer: &o.bootstrapServer,
		authMechanism:   &o.authMechanism,
		username:        &o.username,
		password:        &o.password,
		tlsEnabled:      &o.tlsEnabled,
		tlsCert:         &o.tlsCert,
		tlsKey:          &o.tlsKey,
		tlsCA:           &o.tlsCA,
		output:          &o.output,
		excludeInternal: &o.excludeInternal,
		excludeTopics:   &o.excludeTopics,
		includeManaged:  &o.includeManaged,
		timeout:         &o.timeout,
	}
}

// registerConnectionFlags declares the flags shared by audit and check.
//
// WO-36: single definition of the flag names, defaults, and help strings. Both
// commands previously repeated all eleven declarations verbatim.
func registerConnectionFlags(flags *pflag.FlagSet, c connectionOptions) {
	flags.StringVar(c.bootstrapServer, "bootstrap-server", "", "Kafka bootstrap server(s) (host:port, comma-separated)")
	flags.StringVar(c.authMechanism, "auth-mechanism", "", "SASL mechanism (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)")
	flags.StringVar(c.username, "username", "", "SASL username")
	flags.StringVar(c.password, "password", "", "SASL password")
	flags.BoolVar(c.tlsEnabled, "tls", false, "Enable TLS")
	flags.StringVar(c.tlsCert, "tls-cert", "", "Path to TLS client certificate")
	flags.StringVar(c.tlsKey, "tls-key", "", "Path to TLS client private key")
	flags.StringVar(c.tlsCA, "tls-ca", "", "Path to TLS CA certificate")
	flags.StringVar(c.output, "output", "text", "Output format (json|sarif|spectrehub|text)")
	flags.BoolVar(c.excludeInternal, "exclude-internal", false, "Exclude internal topics from analysis")
	flags.StringSliceVar(c.excludeTopics, "exclude-topics", nil, "Exclude topics by name or glob pattern (repeatable)")
	flags.BoolVar(c.includeManaged, "include-managed", false, "Include service-managed topics (Schema Registry, Connect) in analysis")
	flags.DurationVar(c.timeout, "timeout", 0, "Kafka query timeout (for example: 10s, 1m)")
}

// applyConnectionConfigDefaults fills unset options from the config file.
//
// WO-36: applyAuditConfigDefaults and applyCheckConfigDefaults were line-for-line
// identical apart from their parameter type.
func applyConnectionConfigDefaults(cmd *cobra.Command, cfg *config.Config, c connectionOptions) {
	if !flagChanged(cmd, "bootstrap-server") && strings.TrimSpace(*c.bootstrapServer) == "" && strings.TrimSpace(cfg.BootstrapServers) != "" {
		*c.bootstrapServer = cfg.BootstrapServers
	}
	if !flagChanged(cmd, "auth-mechanism") && strings.TrimSpace(*c.authMechanism) == "" && strings.TrimSpace(cfg.AuthMechanism) != "" {
		*c.authMechanism = cfg.AuthMechanism
	}
	if !flagChanged(cmd, "output") && strings.TrimSpace(cfg.Format) != "" {
		*c.output = cfg.Format
	}
	if !flagChanged(cmd, "exclude-internal") && cfg.ExcludeInternal != nil {
		*c.excludeInternal = *cfg.ExcludeInternal
	}
	if !flagChanged(cmd, "exclude-topics") && len(cfg.ExcludeTopics) > 0 {
		*c.excludeTopics = append([]string(nil), cfg.ExcludeTopics...)
	}
	if !flagChanged(cmd, "timeout") && cfg.HasTimeout {
		*c.timeout = cfg.Timeout
	}
	// WO-34: TLS material completes the connection surface so a secured cluster
	// can be expressed in config instead of on every command line.
	if !flagChanged(cmd, "tls") && cfg.TLSEnabled != nil {
		*c.tlsEnabled = *cfg.TLSEnabled
	}
	if !flagChanged(cmd, "tls-cert") && strings.TrimSpace(cfg.TLSCertFile) != "" {
		*c.tlsCert = cfg.TLSCertFile
	}
	if !flagChanged(cmd, "tls-key") && strings.TrimSpace(cfg.TLSKeyFile) != "" {
		*c.tlsKey = cfg.TLSKeyFile
	}
	if !flagChanged(cmd, "tls-ca") && strings.TrimSpace(cfg.TLSCAFile) != "" {
		*c.tlsCA = cfg.TLSCAFile
	}
	// WO-41: without this the operator escape hatch is unreachable — the very
	// "parsed but never wired" defect class this review set out to find.
	// Applied process-wide once, before any scanning begins.
	kafka.SetExtraManagedPatterns(cfg.ManagedTopics)
}

// applyEnvCredentials sources SASL credentials from the environment.
//
// WO-34: credentials are deliberately not readable from the config file.
func applyEnvCredentials(cmd *cobra.Command, c connectionOptions) {
	envUser, envPass := config.CredentialsFromEnv()
	if !flagChanged(cmd, "username") && *c.username == "" {
		*c.username = envUser
	}
	if !flagChanged(cmd, "password") && *c.password == "" {
		*c.password = envPass
	}
}

// applyDefaultTimeout substitutes the default only when --timeout was absent.
//
// WO-37: keying on a zero value made an explicit `--timeout 0` indistinguishable
// from "flag not set", so it was rewritten to the default and the
// "timeout must be greater than zero" guard became unreachable.
func applyDefaultTimeout(cmd *cobra.Command, c connectionOptions) {
	if *c.timeout == 0 && !flagChanged(cmd, "timeout") {
		*c.timeout = defaultQueryTimeout
	}
}

// resolvedOutput validates the requested output format and returns it normalized.
//
// WO-36: the allowlist lived in runAudit and runCheck independently, so a new
// format had to be added twice.
func resolvedOutput(raw string) (string, error) {
	output := strings.ToLower(strings.TrimSpace(raw))
	if output == "" {
		output = "text"
	}
	if output != "json" && output != "sarif" && output != "spectrehub" && output != "text" {
		return "", fmt.Errorf("invalid output format %q (expected json, sarif, spectrehub, or text)", raw)
	}
	return output, nil
}

// validateConnection enforces the connection preconditions shared by both
// commands. Error strings are byte-identical to the previous per-command checks.
//
// WO-36: this block was duplicated in runAudit and runCheck.
func validateConnection(c connectionOptions) error {
	if strings.TrimSpace(*c.bootstrapServer) == "" {
		return errors.New("bootstrap-server is required")
	}
	if *c.authMechanism != "" && (*c.username == "" || *c.password == "") {
		return errors.New("auth-mechanism requires both --username and --password")
	}
	if (*c.tlsCert == "") != (*c.tlsKey == "") {
		return errors.New("--tls-cert and --tls-key must be provided together")
	}
	if *c.timeout <= 0 {
		return errors.New("timeout must be greater than zero")
	}
	return nil
}

// buildKafkaConfig assembles the client configuration.
//
// WO-36: constructed identically in runAudit and runCheck.
func buildKafkaConfig(c connectionOptions) kafka.Config {
	return kafka.Config{
		BootstrapServers: *c.bootstrapServer,
		AuthMechanism:    *c.authMechanism,
		Username:         *c.username,
		Password:         *c.password,
		TLSEnabled:       *c.tlsEnabled,
		TLSCertFile:      *c.tlsCert,
		TLSKeyFile:       *c.tlsKey,
		TLSCAFile:        *c.tlsCA,
		QueryTimeout:     *c.timeout,
	}
}

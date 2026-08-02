package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ppiankov/kafkaspectre/internal/config"
	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

// WO-34: config fixture helper
func withConfig(t *testing.T, body string) {
	t.Helper()
	workingDir := t.TempDir()
	withWorkingDir(t, workingDir)
	setHomeDir(t, t.TempDir())

	if body == "" {
		return
	}
	path := filepath.Join(workingDir, config.DefaultFileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// WO-34: the defect was that a config naming auth_mechanism made EVERY
// invocation fail, because username and password could only come from flags.
// Testing config.CredentialsFromEnv alone does not cover that — this asserts the
// credentials actually reach the resolved options.
func TestEnvCredentialsReachResolvedOptions(t *testing.T) {
	withConfig(t, "bootstrap_servers: kafka:9092\nauth_mechanism: SCRAM-SHA-512\n")
	t.Setenv(config.UsernameEnvVar, "kafka-user")
	t.Setenv(config.PasswordEnvVar, "env-supplied-value")

	resolved, err := resolveAuditOptions(newAuditCmd(), auditOptions{output: "text"})
	if err != nil {
		t.Fatalf("resolveAuditOptions: %v", err)
	}

	if resolved.authMechanism != "SCRAM-SHA-512" {
		t.Fatalf("authMechanism = %q", resolved.authMechanism)
	}
	if resolved.username != "kafka-user" {
		t.Fatalf("username = %q, want it sourced from %s", resolved.username, config.UsernameEnvVar)
	}
	if resolved.password == "" {
		t.Fatalf("password was not sourced from %s", config.PasswordEnvVar)
	}

	// The whole point: this combination must now validate.
	if err := validateConnection(resolved.connection()); err != nil {
		t.Fatalf("config-only secured connection still fails: %v", err)
	}
}

// WO-34: an explicit flag must still beat the environment.
func TestExplicitCredentialFlagBeatsEnv(t *testing.T) {
	withConfig(t, "")
	t.Setenv(config.UsernameEnvVar, "env-user")

	cmd := newAuditCmd()
	if err := cmd.Flags().Parse([]string{"--username", "flag-user"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	resolved, err := resolveAuditOptions(cmd, auditOptions{output: "text", username: "flag-user"})
	if err != nil {
		t.Fatalf("resolveAuditOptions: %v", err)
	}
	if resolved.username != "flag-user" {
		t.Fatalf("username = %q, want the explicit flag to win", resolved.username)
	}
}

// WO-34: TLS config keys are useless unless they reach the Kafka client.
// Parsing them into a Config struct proves nothing on its own.
func TestTLSConfigReachesKafkaConfig(t *testing.T) {
	withConfig(t, "bootstrap_servers: kafka:9092\ntls: true\ntls_cert: /certs/client.pem\ntls_key: /certs/client.key\ntls_ca: /certs/ca.pem\n")

	resolved, err := resolveAuditOptions(newAuditCmd(), auditOptions{output: "text"})
	if err != nil {
		t.Fatalf("resolveAuditOptions: %v", err)
	}

	kafkaCfg := buildKafkaConfig(resolved.connection())
	if !kafkaCfg.TLSEnabled {
		t.Error("tls: true did not reach kafka.Config")
	}
	if kafkaCfg.TLSCertFile != "/certs/client.pem" {
		t.Errorf("TLSCertFile = %q", kafkaCfg.TLSCertFile)
	}
	if kafkaCfg.TLSKeyFile != "/certs/client.key" {
		t.Errorf("TLSKeyFile = %q", kafkaCfg.TLSKeyFile)
	}
	if kafkaCfg.TLSCAFile != "/certs/ca.pem" {
		t.Errorf("TLSCAFile = %q", kafkaCfg.TLSCAFile)
	}
}

// WO-34: same wiring must hold for check, which is a separate command.
func TestTLSConfigReachesKafkaConfigForCheck(t *testing.T) {
	withConfig(t, "bootstrap_servers: kafka:9092\ntls: true\ntls_ca: /certs/ca.pem\n")

	resolved, err := resolveCheckOptions(newCheckCmd(), checkOptions{output: "text"})
	if err != nil {
		t.Fatalf("resolveCheckOptions: %v", err)
	}

	kafkaCfg := buildKafkaConfig(resolved.connection())
	if !kafkaCfg.TLSEnabled || kafkaCfg.TLSCAFile != "/certs/ca.pem" {
		t.Fatalf("TLS config did not reach check's kafka.Config: %+v", kafkaCfg)
	}
}

// WO-41: the operator escape hatch is worthless if no operator can reach it.
// SetExtraManagedPatterns had no production caller until managed_topics was
// wired — the same "parsed but never wired" defect class this review hunts.
func TestManagedTopicsConfigKeyReachesClassification(t *testing.T) {
	original := []string(nil)
	t.Cleanup(func() { kafka.SetExtraManagedPatterns(original) })

	withConfig(t, "bootstrap_servers: kafka:9092\nmanaged_topics:\n  - \"docker-connect-*\"\n  - \"acme-*-state\"\n")

	if _, err := resolveAuditOptions(newAuditCmd(), auditOptions{output: "text"}); err != nil {
		t.Fatalf("resolveAuditOptions: %v", err)
	}

	for _, declared := range []string{"docker-connect-configs", "acme-billing-state"} {
		if !kafka.IsManagedTopic(declared) {
			t.Errorf("operator-declared pattern did not reach classification for %q", declared)
		}
	}
	if kafka.IsManagedTopic("orders") {
		t.Error("operator patterns captured an unrelated topic")
	}
}

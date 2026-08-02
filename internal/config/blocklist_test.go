package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func parseConfig(t *testing.T, body string) (*Config, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return LoadFromPath(path)
}

// WO-33: YAML allows sequence items at the same indentation as their parent
// key. The old terminator ("line has no leading whitespace") treated the first
// such item as the next root key, so parsing failed with "unexpected list item"
// and every command was blocked until the user re-indented.
func TestBlockListIndentationStyles(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "zero-indent-items",
			body: "exclude_topics:\n- foo\n- bar\n",
			want: []string{"foo", "bar"},
		},
		{
			name: "indented-items",
			body: "exclude_topics:\n  - foo\n  - bar\n",
			want: []string{"foo", "bar"},
		},
		{
			name: "tab-indented-items",
			body: "exclude_topics:\n\t- foo\n\t- bar\n",
			want: []string{"foo", "bar"},
		},
		{
			name: "mixed-indent-items",
			body: "exclude_topics:\n- foo\n  - bar\n",
			want: []string{"foo", "bar"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseConfig(t, tc.body)
			if err != nil {
				t.Fatalf("parse error = %v", err)
			}
			if !reflect.DeepEqual(cfg.ExcludeTopics, tc.want) {
				t.Fatalf("exclude_topics = %v, want %v", cfg.ExcludeTopics, tc.want)
			}
		})
	}
}

// WO-33: a zero-indent list must still terminate at the next root key.
func TestZeroIndentBlockListTerminatesAtNextKey(t *testing.T) {
	cfg, err := parseConfig(t, "exclude_topics:\n- foo\n- bar\nformat: json\nbootstrap_servers: kafka:9092\n")
	if err != nil {
		t.Fatalf("parse error = %v", err)
	}

	if !reflect.DeepEqual(cfg.ExcludeTopics, []string{"foo", "bar"}) {
		t.Fatalf("exclude_topics = %v", cfg.ExcludeTopics)
	}
	if cfg.Format != "json" {
		t.Fatalf("format = %q, want json (key after the list was not parsed)", cfg.Format)
	}
	if cfg.BootstrapServers != "kafka:9092" {
		t.Fatalf("bootstrap_servers = %q", cfg.BootstrapServers)
	}
}

// WO-33: a stray list item with no preceding list key is still malformed.
func TestStrayListItemStillRejected(t *testing.T) {
	if _, err := parseConfig(t, "format: json\n- orphan\n"); err == nil {
		t.Fatal("stray list item was accepted")
	}
}

// WO-34: TLS material is configurable so a secured cluster can be expressed
// without repeating flags on every invocation.
func TestTLSConfigKeys(t *testing.T) {
	cfg, err := parseConfig(t, "tls: true\ntls_cert: /certs/client.pem\ntls_key: /certs/client.key\ntls_ca: /certs/ca.pem\n")
	if err != nil {
		t.Fatalf("parse error = %v", err)
	}

	if cfg.TLSEnabled == nil || !*cfg.TLSEnabled {
		t.Fatalf("tls = %v, want true", cfg.TLSEnabled)
	}
	if cfg.TLSCertFile != "/certs/client.pem" {
		t.Fatalf("tls_cert = %q", cfg.TLSCertFile)
	}
	if cfg.TLSKeyFile != "/certs/client.key" {
		t.Fatalf("tls_key = %q", cfg.TLSKeyFile)
	}
	if cfg.TLSCAFile != "/certs/ca.pem" {
		t.Fatalf("tls_ca = %q", cfg.TLSCAFile)
	}
}

// WO-34: credentials must never be read from a plaintext file on disk. Silently
// ignoring them would leave the user believing the config worked.
func TestCredentialKeysInConfigAreRejected(t *testing.T) {
	for _, key := range []string{"username", "password"} {
		t.Run(key, func(t *testing.T) {
			_, err := parseConfig(t, key+": someone\n")
			if err == nil {
				t.Fatalf("%q was accepted in the config file", key)
			}
			if !strings.Contains(err.Error(), UsernameEnvVar) {
				t.Fatalf("error should point at the environment variables, got %q", err)
			}
		})
	}
}

// WO-34: the environment is the credential source.
func TestCredentialsFromEnv(t *testing.T) {
	t.Setenv(UsernameEnvVar, "kafka-user")
	t.Setenv(PasswordEnvVar, "not-a-real-secret")

	username, password := CredentialsFromEnv()
	if username != "kafka-user" {
		t.Fatalf("username = %q", username)
	}
	if password != "not-a-real-secret" {
		t.Fatalf("password not read from %s", PasswordEnvVar)
	}
}

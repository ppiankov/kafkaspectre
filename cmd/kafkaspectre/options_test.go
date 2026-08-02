package main

import (
	"bytes"
	"reflect"
	"sort"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// connectionFlagNames is the shared connection/output surface both commands
// expose. WO-36: registered once, so this list is the contract.
var connectionFlagNames = []string{
	"auth-mechanism",
	"bootstrap-server",
	"exclude-internal",
	"exclude-topics",
	"include-managed",
	"output",
	"password",
	"timeout",
	"tls",
	"tls-ca",
	"tls-cert",
	"tls-key",
	"username",
}

// WO-36: extract flag names helper
func flagNames(set *pflag.FlagSet) []string {
	names := make([]string, 0)
	set.VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	sort.Strings(names)
	return names
}

// WO-36: the eleven-plus connection flags were declared twice, once per command,
// so a flag added to one silently went missing from the other. With a single
// registration the two sets must agree exactly.
// WO-36: shared flag set
func TestAuditAndCheckShareConnectionFlags(t *testing.T) {
	audit := flagNames(newAuditCmd().Flags())
	check := flagNames(newCheckCmd().Flags())

	if !reflect.DeepEqual(audit, connectionFlagNames) {
		t.Errorf("audit flags = %v, want %v", audit, connectionFlagNames)
	}

	// check has everything audit has, plus --repo.
	wantCheck := append(append([]string(nil), connectionFlagNames...), "repo")
	sort.Strings(wantCheck)
	if !reflect.DeepEqual(check, wantCheck) {
		t.Errorf("check flags = %v, want %v", check, wantCheck)
	}
}

// WO-36: defaults and help strings must also come from the single registration.
// WO-36: flag definitions match
func TestConnectionFlagDefinitionsMatchAcrossCommands(t *testing.T) {
	audit := newAuditCmd().Flags()
	check := newCheckCmd().Flags()

	for _, name := range connectionFlagNames {
		a := audit.Lookup(name)
		c := check.Lookup(name)
		if a == nil || c == nil {
			t.Fatalf("flag %q missing (audit=%v check=%v)", name, a != nil, c != nil)
		}
		if a.DefValue != c.DefValue {
			t.Errorf("flag %q default: audit=%q check=%q", name, a.DefValue, c.DefValue)
		}
		if a.Usage != c.Usage {
			t.Errorf("flag %q usage differs between commands:\n audit=%q\n check=%q", name, a.Usage, c.Usage)
		}
		if a.Value.Type() != c.Value.Type() {
			t.Errorf("flag %q type: audit=%q check=%q", name, a.Value.Type(), c.Value.Type())
		}
	}
}

// WO-36: validation error strings are user-visible contract. The refactor moved
// these checks into one function; the wording must not have drifted.
func TestValidationErrorStringsUnchanged(t *testing.T) {
	base := func() auditOptions {
		return auditOptions{bootstrapServer: "kafka:9092", output: "text", timeout: 10}
	}

	cases := []struct {
		name    string
		mutate  func(*auditOptions)
		wantErr string
	}{
		{
			name:    "missing-bootstrap-server",
			mutate:  func(o *auditOptions) { o.bootstrapServer = "" },
			wantErr: "bootstrap-server is required",
		},
		{
			name:    "auth-without-credentials",
			mutate:  func(o *auditOptions) { o.authMechanism = "PLAIN" },
			wantErr: "auth-mechanism requires both --username and --password",
		},
		{
			name:    "tls-cert-without-key",
			mutate:  func(o *auditOptions) { o.tlsCert = "/certs/client.pem" },
			wantErr: "--tls-cert and --tls-key must be provided together",
		},
		{
			name:    "non-positive-timeout",
			mutate:  func(o *auditOptions) { o.timeout = 0 },
			wantErr: "timeout must be greater than zero",
		},
		{
			name:    "invalid-output-format",
			mutate:  func(o *auditOptions) { o.output = "yaml" },
			wantErr: `invalid output format "yaml" (expected json, sarif, spectrehub, or text)`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := base()
			tc.mutate(&opts)

			// WO-36: exercise validateConnection where it can, so deleting a
			// check from the shared function actually fails this test. The
			// bootstrap-server check used to be written three times, and this
			// test hit a per-command copy rather than the shared one.
			if tc.name != "invalid-output-format" {
				if err := validateConnection(opts.connection()); err == nil || err.Error() != tc.wantErr {
					t.Fatalf("validateConnection error = %v, want %q", err, tc.wantErr)
				}
			}

			err := runAudit(newAuditCmd(), opts)
			if err == nil {
				t.Fatalf("expected error %q, got nil", tc.wantErr)
			}
			if err.Error() != tc.wantErr {
				t.Fatalf("error = %q, want %q", err, tc.wantErr)
			}
		})
	}
}

// WO-36: help output is the most visible CLI surface. Renders must stay stable
// and must not leak an empty or malformed flag section after the refactor.
func TestHelpOutputRendersEveryConnectionFlag(t *testing.T) {
	for _, newCmd := range []func() *cobra.Command{newAuditCmd, newCheckCmd} {
		cmd := newCmd()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		if err := cmd.Help(); err != nil {
			t.Fatalf("Help(): %v", err)
		}

		help := buf.String()
		for _, name := range connectionFlagNames {
			if !bytes.Contains([]byte(help), []byte("--"+name)) {
				t.Errorf("%s help output omits --%s", cmd.Name(), name)
			}
		}
	}
}

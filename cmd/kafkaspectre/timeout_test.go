package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppiankov/kafkaspectre/internal/config"
	"github.com/spf13/cobra"
)

// WO-37: `--timeout 0` was indistinguishable from "flag absent" because both
// produced a zero duration, so the sentinel substitution rewrote it to 10s
// before the "timeout must be greater than zero" guard could ever see it.
func TestTimeoutResolution(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		configBody string
		want       time.Duration
	}{
		{
			name: "flag-absent-uses-default",
			args: nil,
			want: defaultQueryTimeout,
		},
		{
			name: "explicit-zero-is-preserved-for-validation",
			args: []string{"--timeout", "0s"},
			want: 0,
		},
		{
			name:       "config-honoured-when-flag-absent",
			configBody: "timeout: 45s\n",
			want:       45 * time.Second,
		},
		{
			name:       "explicit-flag-overrides-config",
			args:       []string{"--timeout", "5s"},
			configBody: "timeout: 45s\n",
			want:       5 * time.Second,
		},
		{
			name:       "explicit-zero-overrides-config",
			args:       []string{"--timeout", "0s"},
			configBody: "timeout: 45s\n",
			want:       0,
		},
	}

	for _, tc := range cases {
		t.Run("audit/"+tc.name, func(t *testing.T) {
			workingDir := t.TempDir()
			withWorkingDir(t, workingDir)
			t.Setenv("HOME", t.TempDir())

			if tc.configBody != "" {
				path := filepath.Join(workingDir, config.DefaultFileName)
				if err := os.WriteFile(path, []byte(tc.configBody), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}

			cmd := newAuditCmd()
			if err := cmd.Flags().Parse(tc.args); err != nil {
				t.Fatalf("parse flags: %v", err)
			}

			resolved, err := resolveAuditOptions(cmd, auditOptions{output: "text", timeout: mustDuration(t, cmd)})
			if err != nil {
				t.Fatalf("resolveAuditOptions() error = %v", err)
			}
			if resolved.timeout != tc.want {
				t.Fatalf("timeout = %v, want %v", resolved.timeout, tc.want)
			}
		})
	}
}

// WO-37: an explicit zero must now reach the guard and be rejected.
func TestRunAuditRejectsExplicitZeroTimeout(t *testing.T) {
	err := runAudit(newAuditCmd(), auditOptions{
		bootstrapServer: "localhost:9092",
		output:          "text",
		timeout:         0,
	})
	if err == nil {
		t.Fatal("runAudit accepted a zero timeout")
	}
	if err.Error() != "timeout must be greater than zero" {
		t.Fatalf("error = %q, want %q", err, "timeout must be greater than zero")
	}
	if got := classifyError(err); got != ExitInvalidArg {
		t.Fatalf("exit code = %d, want %d", got, ExitInvalidArg)
	}
}

func TestRunCheckRejectsExplicitZeroTimeout(t *testing.T) {
	err := runCheck(newCheckCmd(), checkOptions{
		repo:            t.TempDir(),
		bootstrapServer: "localhost:9092",
		output:          "text",
		timeout:         0,
	})
	if err == nil {
		t.Fatal("runCheck accepted a zero timeout")
	}
	if err.Error() != "timeout must be greater than zero" {
		t.Fatalf("error = %q, want %q", err, "timeout must be greater than zero")
	}
}

// mustDuration reads the parsed --timeout value back off the command, mirroring
// how cobra populates the options struct in production.
func mustDuration(t *testing.T, cmd *cobra.Command) time.Duration {
	t.Helper()
	value, err := cmd.Flags().GetDuration("timeout")
	if err != nil {
		t.Fatalf("read timeout flag: %v", err)
	}
	return value
}

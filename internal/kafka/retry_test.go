package kafka

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestIsAuthError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "generic", err: errors.New("something"), want: false},
		{name: "sasl-auth-failed", err: kerr.SaslAuthenticationFailed, want: true},
		{name: "unsupported-sasl-mechanism", err: kerr.UnsupportedSaslMechanism, want: true},
		{name: "illegal-sasl-state", err: kerr.IllegalSaslState, want: true},
		{name: "topic-auth-failed", err: kerr.TopicAuthorizationFailed, want: true},
		{name: "cluster-auth-failed", err: kerr.ClusterAuthorizationFailed, want: true},
		{name: "group-auth-failed", err: kerr.GroupAuthorizationFailed, want: true},
		{name: "wrapped-sasl-auth", err: fmt.Errorf("connect: %w", kerr.SaslAuthenticationFailed), want: true},
		{name: "broker-not-available", err: kerr.BrokerNotAvailable, want: false},
		{name: "request-timed-out", err: kerr.RequestTimedOut, want: false},
		{name: "context-canceled", err: context.Canceled, want: false},
		{name: "io-eof", err: io.EOF, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAuthError(tc.err); got != tc.want {
				t.Fatalf("isAuthError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsAuthErrorFirstReadEOF(t *testing.T) {
	// ErrFirstReadEOF is a struct type; create one via the package's
	// exported constructor (it implements error).
	eof := &kgo.ErrFirstReadEOF{}
	if !isAuthError(eof) {
		t.Fatalf("isAuthError(ErrFirstReadEOF) = false, want true")
	}

	wrapped := fmt.Errorf("connection failed: %w", eof)
	if !isAuthError(wrapped) {
		t.Fatalf("isAuthError(wrapped ErrFirstReadEOF) = false, want true")
	}
}

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "auth-error", err: kerr.SaslAuthenticationFailed, want: false},
		{name: "context-canceled", err: context.Canceled, want: false},
		{name: "context-deadline", err: context.DeadlineExceeded, want: false},
		{name: "broker-not-available", err: kerr.BrokerNotAvailable, want: true},
		{name: "leader-not-available", err: kerr.LeaderNotAvailable, want: true},
		{name: "request-timed-out", err: kerr.RequestTimedOut, want: true},
		{name: "network-exception", err: kerr.NetworkException, want: true},
		{name: "net-closed", err: net.ErrClosed, want: true},
		{name: "wrapped-broker-unavail", err: fmt.Errorf("op: %w", kerr.BrokerNotAvailable), want: true},
		{name: "generic-error", err: errors.New("unknown"), want: false},
		{name: "non-retriable-kerr", err: kerr.InvalidTopicException, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryable(tc.err); got != tc.want {
				t.Fatalf("isRetryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsRetryableDialErrors(t *testing.T) {
	// Connection refused (non-timeout dial error) should NOT be retryable
	connRefused := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Addr: &net.TCPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 9092,
		},
		Err: &os.SyscallError{
			Syscall: "connect",
			Err:     syscall.ECONNREFUSED,
		},
	}
	if isRetryable(connRefused) {
		t.Fatalf("isRetryable(connection refused) = true, want false")
	}

	// Dial timeout should be retryable
	dialTimeout := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Addr: &net.TCPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 9092,
		},
		Err: &timeoutError{},
	}
	if !isRetryable(dialTimeout) {
		t.Fatalf("isRetryable(dial timeout) = false, want true")
	}
}

// timeoutError is a test helper that satisfies the net.Error interface
// with Timeout() returning true.
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

func TestWithRetrySuccess(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), "test", func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestWithRetryTransientThenSuccess(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), "test", func() error {
		calls++
		if calls <= 2 {
			return kerr.BrokerNotAvailable
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestWithRetryAuthFailsFast(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), "test", func() error {
		calls++
		return kerr.SaslAuthenticationFailed
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (fail fast), got %d", calls)
	}
	if !errors.Is(err, kerr.SaslAuthenticationFailed) {
		t.Fatalf("error = %v, want SaslAuthenticationFailed", err)
	}
}

func TestWithRetryExhaustsAttempts(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), "fetch", func() error {
		calls++
		return kerr.BrokerNotAvailable
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != maxRetries+1 {
		t.Fatalf("expected %d calls, got %d", maxRetries+1, calls)
	}
	if !strings.Contains(err.Error(), "attempts exhausted") {
		t.Fatalf("error = %q, want 'attempts exhausted'", err.Error())
	}
	if !errors.Is(err, kerr.BrokerNotAvailable) {
		t.Fatalf("underlying error should be BrokerNotAvailable")
	}
}

func TestWithRetryNonRetryableFailsFast(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), "test", func() error {
		calls++
		return errors.New("permanent failure")
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestWithRetryRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	err := withRetry(ctx, "test", func() error {
		calls++
		cancel() // cancel after first attempt
		return kerr.BrokerNotAvailable
	})

	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestWithRetryRespectsContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	calls := 0
	err := withRetry(ctx, "test", func() error {
		calls++
		return kerr.BrokerNotAvailable
	})

	if err == nil {
		t.Fatalf("expected error")
	}
	// Should have been cancelled before exhausting all attempts due to short timeout
	if calls > maxRetries+1 {
		t.Fatalf("too many calls: %d", calls)
	}
}

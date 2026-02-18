package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	maxRetries     = 3
	initialBackoff = 500 * time.Millisecond
	maxBackoff     = 4 * time.Second
)

// isAuthError returns true for errors that indicate SASL authentication or
// authorization failures. These are permanent — retrying will not help.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}

	var ke *kerr.Error
	if errors.As(err, &ke) {
		switch ke {
		case kerr.SaslAuthenticationFailed,
			kerr.UnsupportedSaslMechanism,
			kerr.IllegalSaslState,
			kerr.TopicAuthorizationFailed,
			kerr.ClusterAuthorizationFailed,
			kerr.GroupAuthorizationFailed,
			kerr.TransactionalIDAuthorizationFailed:
			return true
		}
	}

	var eof *kgo.ErrFirstReadEOF
	return errors.As(err, &eof)
}

// isRetryable returns true for transient broker errors where a retry might
// succeed: timeouts, broker restarts, temporary leader unavailability.
func isRetryable(err error) bool {
	if err == nil || isAuthError(err) {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Kafka protocol errors with retriable flag
	var ke *kerr.Error
	if errors.As(err, &ke) {
		return ke.Retriable
	}

	// Network-level: connection closed, EOF after established connection
	if errors.Is(err, net.ErrClosed) {
		return true
	}

	// Dial timeouts are retryable; connection-refused is not
	var ne *net.OpError
	if errors.As(err, &ne) {
		return ne.Timeout()
	}

	return false
}

// withRetry executes fn up to maxRetries times with exponential backoff.
// Auth errors fail immediately. Context cancellation stops retries.
func withRetry(ctx context.Context, desc string, fn func() error) error {
	backoff := initialBackoff

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if isAuthError(lastErr) {
			return lastErr
		}

		if !isRetryable(lastErr) {
			return lastErr
		}

		if attempt == maxRetries {
			break
		}

		slog.Warn("retrying after transient error",
			"operation", desc,
			"attempt", attempt+1,
			"max_attempts", maxRetries+1,
			"backoff", backoff,
			"error", lastErr,
		)

		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w (last error: %w)", desc, ctx.Err(), lastErr)
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	return fmt.Errorf("%s: %d attempts exhausted: %w", desc, maxRetries+1, lastErr)
}

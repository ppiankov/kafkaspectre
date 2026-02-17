package logging

import (
	"log/slog"
	"os"
)

// Init configures the process-wide default slog logger.
func Init(verbose bool) {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}

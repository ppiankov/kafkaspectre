package reporter

import (
	"context"
	"encoding/json"
	"io"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

// JSONReporter generates JSON reports
type JSONReporter struct {
	writer io.Writer
	pretty bool
}

// NewJSONReporter creates a new JSON reporter
func NewJSONReporter(w io.Writer, pretty bool) *JSONReporter {
	return &JSONReporter{
		writer: w,
		pretty: pretty,
	}
}

// Generate produces a JSON report
func (r *JSONReporter) Generate(ctx context.Context, metadata *kafka.ClusterMetadata) error {
	var output []byte
	var err error

	if r.pretty {
		output, err = json.MarshalIndent(metadata, "", "  ")
	} else {
		output, err = json.Marshal(metadata)
	}

	if err != nil {
		return err
	}

	_, err = r.writer.Write(output)
	if err != nil {
		return err
	}

	// Add newline at the end
	_, err = r.writer.Write([]byte("\n"))
	return err
}

// GenerateAudit is a stub to satisfy the Reporter interface
func (r *JSONReporter) GenerateAudit(ctx context.Context, result *AuditResult) error {
	// JSONReporter doesn't support audit mode directly
	// Use AuditJSONReporter instead
	return nil // Could also return an error like text reporter
}

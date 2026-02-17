package reporter

import (
	"context"
	"encoding/json"
	"io"
)

// CheckJSONReporter writes check results as JSON.
type CheckJSONReporter struct {
	writer io.Writer
	pretty bool
}

// NewCheckJSONReporter creates a JSON reporter for check results.
func NewCheckJSONReporter(w io.Writer, pretty bool) *CheckJSONReporter {
	return &CheckJSONReporter{writer: w, pretty: pretty}
}

// GenerateCheck emits the check result as JSON.
func (r *CheckJSONReporter) GenerateCheck(ctx context.Context, result *CheckResult) error {
	var (
		data []byte
		err  error
	)

	if r.pretty {
		data, err = json.MarshalIndent(result, "", "  ")
	} else {
		data, err = json.Marshal(result)
	}
	if err != nil {
		return err
	}
	if _, err := r.writer.Write(data); err != nil {
		return err
	}
	_, err = r.writer.Write([]byte("\n"))
	return err
}

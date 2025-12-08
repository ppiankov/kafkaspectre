package reporter

import (
	"context"
	"encoding/json"
	"io"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
)

// AuditJSONReporter generates JSON audit reports
type AuditJSONReporter struct {
	writer io.Writer
	pretty bool
}

// NewAuditJSONReporter creates a new audit JSON reporter
func NewAuditJSONReporter(w io.Writer, pretty bool) *AuditJSONReporter {
	return &AuditJSONReporter{
		writer: w,
		pretty: pretty,
	}
}

// AuditJSONOutput is the restructured JSON output format
type AuditJSONOutput struct {
	Summary          *AuditSummary    `json:"summary"`
	UnusedTopics     []*UnusedTopic   `json:"unused_topics"`
	ActiveTopics     []*ActiveTopic   `json:"active_topics,omitempty"`
	ClusterMetadata  *ClusterMetadata `json:"cluster_metadata"`
}

// ClusterMetadata simplified for JSON output
type ClusterMetadata struct {
	Brokers       []BrokerInfo `json:"brokers"`
	ConsumerCount int          `json:"consumer_groups_count"`
	FetchedAt     string       `json:"fetched_at"`
}

// BrokerInfo simplified for JSON output
type BrokerInfo struct {
	ID   int32  `json:"id"`
	Host string `json:"host"`
	Port int32  `json:"port"`
}

// GenerateAudit produces a JSON audit report with improved structure
func (r *AuditJSONReporter) GenerateAudit(ctx context.Context, result *AuditResult) error {
	// Build simplified output structure
	output := &AuditJSONOutput{
		Summary:      result.Summary,
		UnusedTopics: result.UnusedTopics,
		ClusterMetadata: &ClusterMetadata{
			Brokers:       convertBrokers(result.Metadata.Brokers),
			ConsumerCount: len(result.Metadata.ConsumerGroups),
			FetchedAt:     result.Metadata.FetchedAt.Format("2006-01-02 15:04:05 MST"),
		},
	}

	// Only include active topics if there are less than 50 to avoid noise
	if result.ActiveCount <= 50 {
		output.ActiveTopics = result.ActiveTopics
	}

	var jsonBytes []byte
	var err error

	if r.pretty {
		jsonBytes, err = json.MarshalIndent(output, "", "  ")
	} else {
		jsonBytes, err = json.Marshal(output)
	}

	if err != nil {
		return err
	}

	_, err = r.writer.Write(jsonBytes)
	if err != nil {
		return err
	}

	// Add newline at the end
	_, err = r.writer.Write([]byte("\n"))
	return err
}

// Generate is a stub to satisfy the Reporter interface
func (r *AuditJSONReporter) Generate(ctx context.Context, metadata *kafka.ClusterMetadata) error {
	// AuditJSONReporter doesn't support standard generate mode
	return nil
}

func convertBrokers(brokers []kafka.BrokerInfo) []BrokerInfo {
	result := make([]BrokerInfo, len(brokers))
	for i, b := range brokers {
		result[i] = BrokerInfo{
			ID:   b.ID,
			Host: b.Host,
			Port: b.Port,
		}
	}
	return result
}

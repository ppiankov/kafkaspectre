package reporter

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// SpectreHubEnvelope is the spectre/v1 cross-tool ingestion format.
type SpectreHubEnvelope struct {
	Schema    string              `json:"schema"`
	Tool      string              `json:"tool"`
	Version   string              `json:"version"`
	Timestamp string              `json:"timestamp"`
	Target    SpectreHubTarget    `json:"target"`
	Findings  []SpectreHubFinding `json:"findings"`
	Summary   SpectreHubSummary   `json:"summary"`
}

// SpectreHubTarget describes the audited system.
type SpectreHubTarget struct {
	Type    string `json:"type"`
	URIHash string `json:"uri_hash"`
	Cluster string `json:"cluster,omitempty"`
}

// SpectreHubFinding is a single finding in the spectre/v1 format.
type SpectreHubFinding struct {
	ID       string         `json:"id"`
	Severity string         `json:"severity"`
	Location string         `json:"location"`
	Message  string         `json:"message"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// SpectreHubSummary counts findings by severity.
type SpectreHubSummary struct {
	Total  int `json:"total"`
	High   int `json:"high"`
	Medium int `json:"medium"`
	Low    int `json:"low"`
	Info   int `json:"info"`
}

// HashBootstrap produces a sha256 hash of a Kafka bootstrap server address.
func HashBootstrap(bootstrap string) string {
	normalized := strings.TrimSpace(bootstrap)
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("sha256:%x", h)
}

// SpectreHubReporter writes output in the spectre/v1 envelope format.
type SpectreHubReporter struct {
	writer          io.Writer
	bootstrapServer string
}

// NewSpectreHubReporter creates a SpectreHub reporter.
func NewSpectreHubReporter(w io.Writer, bootstrapServer string) *SpectreHubReporter {
	return &SpectreHubReporter{writer: w, bootstrapServer: bootstrapServer}
}

// GenerateAudit emits audit findings as spectre/v1 JSON.
func (r *SpectreHubReporter) GenerateAudit(_ context.Context, result *AuditResult) error {
	envelope := SpectreHubEnvelope{
		Schema:    "spectre/v1",
		Tool:      "kafkaspectre",
		Version:   result.Version,
		Timestamp: result.Timestamp,
		Target: SpectreHubTarget{
			Type:    "kafka",
			URIHash: HashBootstrap(r.bootstrapServer),
		},
	}

	if result.Summary != nil {
		envelope.Target.Cluster = result.Summary.ClusterName
	}

	for _, topic := range result.UnusedTopics {
		if topic == nil {
			continue
		}
		severity := normalizeSeverity(topic.Risk)
		envelope.Findings = append(envelope.Findings, SpectreHubFinding{
			ID:       "UNUSED_TOPIC",
			Severity: severity,
			Location: topic.Name,
			Message:  topic.Reason,
			Metadata: map[string]any{
				"partitions":         topic.Partitions,
				"replication_factor": topic.ReplicationFactor,
				"retention":          topic.RetentionHuman,
				"recommendation":     topic.Recommendation,
			},
		})
		countSeverity(&envelope.Summary, severity)
	}

	envelope.Summary.Total = len(envelope.Findings)
	if envelope.Findings == nil {
		envelope.Findings = []SpectreHubFinding{}
	}

	enc := json.NewEncoder(r.writer)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope)
}

// GenerateCheck emits check findings as spectre/v1 JSON.
func (r *SpectreHubReporter) GenerateCheck(_ context.Context, result *CheckResult) error {
	envelope := SpectreHubEnvelope{
		Schema:    "spectre/v1",
		Tool:      "kafkaspectre",
		Version:   result.Version,
		Timestamp: result.Timestamp,
		Target: SpectreHubTarget{
			Type:    "kafka",
			URIHash: HashBootstrap(r.bootstrapServer),
		},
	}

	for _, f := range result.Findings {
		if f == nil || f.Status == CheckStatusOK {
			continue
		}
		id, severity := checkFindingMapping(f.Status)
		envelope.Findings = append(envelope.Findings, SpectreHubFinding{
			ID:       id,
			Severity: severity,
			Location: f.Topic,
			Message:  f.Reason,
		})
		countSeverity(&envelope.Summary, severity)
	}

	envelope.Summary.Total = len(envelope.Findings)
	if envelope.Findings == nil {
		envelope.Findings = []SpectreHubFinding{}
	}

	enc := json.NewEncoder(r.writer)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope)
}

func normalizeSeverity(risk string) string {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "high":
		return "high"
	case "medium":
		return "medium"
	case "low":
		return "low"
	default:
		return "info"
	}
}

func checkFindingMapping(status CheckStatus) (id string, severity string) {
	switch status {
	case CheckStatusMissingInCluster:
		return "MISSING_IN_CLUSTER", "high"
	case CheckStatusUnused:
		return "UNUSED", "medium"
	case CheckStatusUnreferencedInRepo:
		return "UNREFERENCED_IN_REPO", "low"
	default:
		return string(status), "info"
	}
}

func countSeverity(s *SpectreHubSummary, severity string) {
	switch severity {
	case "high":
		s.High++
	case "medium":
		s.Medium++
	case "low":
		s.Low++
	case "info":
		s.Info++
	}
}

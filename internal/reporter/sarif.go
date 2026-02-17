package reporter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
)

const (
	sarifSchema  = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion = "2.1.0"

	sarifToolName           = "KafkaSpectre"
	sarifToolInformationURI = "https://github.com/ppiankov/kafkaspectre"
	sarifSrcRootBaseID      = "%SRCROOT%"

	sarifRuleIDUnusedTopic        = "kafkaspectre/UNUSED_TOPIC"
	sarifRuleIDHighRiskTopic      = "kafkaspectre/HIGH_RISK_TOPIC"
	sarifRuleIDMediumRiskTopic    = "kafkaspectre/MEDIUM_RISK_TOPIC"
	sarifRuleIDLowRiskTopic       = "kafkaspectre/LOW_RISK_TOPIC"
	sarifRuleIDMissingInCluster   = "kafkaspectre/MISSING_IN_CLUSTER"
	sarifRuleIDUnreferencedInRepo = "kafkaspectre/UNREFERENCED_IN_REPO"
)

// SARIFReporter writes check/audit output in SARIF 2.1.0 format.
type SARIFReporter struct {
	writer io.Writer
	pretty bool
}

// NewSARIFReporter creates a SARIF reporter.
func NewSARIFReporter(w io.Writer, pretty bool) *SARIFReporter {
	return &SARIFReporter{writer: w, pretty: pretty}
}

// GenerateCheck emits check findings as SARIF.
func (r *SARIFReporter) GenerateCheck(ctx context.Context, result *CheckResult) error {
	run := buildCheckSARIFRun(result)
	return r.writeReport(sarifReport{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs:    []sarifRun{run},
	})
}

// GenerateAudit emits audit findings as SARIF.
func (r *SARIFReporter) GenerateAudit(ctx context.Context, result *AuditResult) error {
	run := buildAuditSARIFRun(result)
	return r.writeReport(sarifReport{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs:    []sarifRun{run},
	})
}

func (r *SARIFReporter) writeReport(report sarifReport) error {
	var (
		data []byte
		err  error
	)

	if r.pretty {
		data, err = json.MarshalIndent(report, "", "  ")
	} else {
		data, err = json.Marshal(report)
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

func buildCheckSARIFRun(result *CheckResult) sarifRun {
	if result == nil {
		result = &CheckResult{}
	}

	rules := []sarifRule{
		buildCheckMissingInClusterRule(),
		buildUnusedTopicRule("warning"),
		buildCheckUnreferencedInRepoRule(),
	}

	results := make([]sarifResult, 0, len(result.Findings))
	for _, finding := range result.Findings {
		if finding == nil {
			continue
		}

		ruleID, level, ok := checkRuleMapping(finding.Status)
		if !ok {
			continue
		}

		message := finding.Reason
		if strings.TrimSpace(message) == "" {
			message = fmt.Sprintf("topic %q has status %s", finding.Topic, finding.Status)
		}

		entry := sarifResult{
			RuleID: ruleID,
			Level:  level,
			Message: sarifMessage{
				Text: fmt.Sprintf("%s: %s", finding.Topic, message),
			},
			PartialFingerprints: map[string]string{
				"topicStatus": fmt.Sprintf("%s|%s", finding.Topic, finding.Status),
			},
			Properties: map[string]any{
				"topic":              finding.Topic,
				"status":             string(finding.Status),
				"referenced_in_repo": finding.ReferencedInRepo,
				"in_cluster":         finding.InCluster,
				"reference_count":    len(finding.References),
			},
		}
		if len(finding.ConsumerGroups) > 0 {
			entry.Properties["consumer_groups"] = finding.ConsumerGroups
		}

		locations := sarifLocationsFromReferences(finding.References)
		if len(locations) > 0 {
			entry.Locations = locations
		}

		results = append(results, entry)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].RuleID != results[j].RuleID {
			return results[i].RuleID < results[j].RuleID
		}
		if sarifResultTopic(results[i]) != sarifResultTopic(results[j]) {
			return sarifResultTopic(results[i]) < sarifResultTopic(results[j])
		}
		return results[i].Message.Text < results[j].Message.Text
	})

	run := sarifRun{
		Tool: sarifTool{
			Driver: sarifDriver{
				Name:           sarifToolName,
				InformationURI: sarifToolInformationURI,
				Rules:          rules,
			},
		},
		Results: results,
	}

	if result.Summary != nil && strings.TrimSpace(result.Summary.RepoPath) != "" {
		run.OriginalURIBaseIDs = map[string]sarifArtifactLocation{
			sarifSrcRootBaseID: {
				URI: pathToFileURI(result.Summary.RepoPath),
			},
		}
	}

	return run
}

func buildAuditSARIFRun(result *AuditResult) sarifRun {
	if result == nil {
		result = &AuditResult{}
	}

	rules := []sarifRule{
		buildHighRiskTopicRule(),
		buildLowRiskTopicRule(),
		buildMediumRiskTopicRule(),
	}
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].ID < rules[j].ID
	})

	results := make([]sarifResult, 0, len(result.UnusedTopics))
	for _, topic := range result.UnusedTopics {
		if topic == nil {
			continue
		}

		ruleID, level := auditRuleMapping(topic.Risk)
		message := topic.Reason
		if strings.TrimSpace(message) == "" {
			message = "topic has no active consumer groups"
		}

		entry := sarifResult{
			RuleID: ruleID,
			Level:  level,
			Message: sarifMessage{
				Text: fmt.Sprintf("%s: %s", topic.Name, message),
			},
			PartialFingerprints: map[string]string{
				"topicRisk": fmt.Sprintf("%s|%s", topic.Name, strings.ToLower(strings.TrimSpace(topic.Risk))),
			},
			Properties: map[string]any{
				"topic":              topic.Name,
				"risk":               strings.ToLower(strings.TrimSpace(topic.Risk)),
				"partitions":         topic.Partitions,
				"replication_factor": topic.ReplicationFactor,
				"retention_ms":       topic.RetentionMs,
				"cleanup_policy":     topic.CleanupPolicy,
				"recommendation":     topic.Recommendation,
				"cleanup_priority":   topic.CleanupPriority,
			},
		}
		results = append(results, entry)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].RuleID != results[j].RuleID {
			return results[i].RuleID < results[j].RuleID
		}
		if sarifResultTopic(results[i]) != sarifResultTopic(results[j]) {
			return sarifResultTopic(results[i]) < sarifResultTopic(results[j])
		}
		return results[i].Message.Text < results[j].Message.Text
	})

	return sarifRun{
		Tool: sarifTool{
			Driver: sarifDriver{
				Name:           sarifToolName,
				InformationURI: sarifToolInformationURI,
				Rules:          rules,
			},
		},
		Results: results,
	}
}

func checkRuleMapping(status CheckStatus) (ruleID string, level string, ok bool) {
	switch status {
	case CheckStatusMissingInCluster:
		return sarifRuleIDMissingInCluster, "error", true
	case CheckStatusUnused:
		return sarifRuleIDUnusedTopic, "warning", true
	case CheckStatusUnreferencedInRepo:
		return sarifRuleIDUnreferencedInRepo, "warning", true
	default:
		return "", "", false
	}
}

func auditRuleMapping(risk string) (ruleID string, level string) {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "high":
		return sarifRuleIDHighRiskTopic, "error"
	case "medium":
		return sarifRuleIDMediumRiskTopic, "warning"
	default:
		return sarifRuleIDLowRiskTopic, "note"
	}
}

func sarifResultTopic(result sarifResult) string {
	if result.Properties == nil {
		return ""
	}
	raw, ok := result.Properties["topic"]
	if !ok {
		return ""
	}
	topic, _ := raw.(string)
	return topic
}

func sarifLocationsFromReferences(refs []CheckReference) []sarifLocation {
	locations := make([]sarifLocation, 0, len(refs))
	for _, ref := range refs {
		normalized := strings.TrimSpace(ref.File)
		if normalized == "" {
			continue
		}
		normalized = filepath.ToSlash(filepath.Clean(normalized))

		location := sarifLocation{
			PhysicalLocation: sarifPhysicalLocation{
				ArtifactLocation: sarifArtifactLocation{
					URI:       normalized,
					URIBaseID: sarifSrcRootBaseID,
				},
			},
		}
		if ref.Line > 0 {
			location.PhysicalLocation.Region = &sarifRegion{StartLine: ref.Line}
		}

		locations = append(locations, location)
	}
	return locations
}

func pathToFileURI(path string) string {
	cleaned := filepath.Clean(path)
	slashPath := filepath.ToSlash(cleaned)
	if !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	if !strings.HasSuffix(slashPath, "/") {
		slashPath += "/"
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String()
}

func buildCheckMissingInClusterRule() sarifRule {
	return sarifRule{
		ID:   sarifRuleIDMissingInCluster,
		Name: "Missing topic in cluster",
		ShortDescription: &sarifMessage{
			Text: "Topic is referenced in repository but missing in Kafka cluster",
		},
		FullDescription: &sarifMessage{
			Text: "The repository references a topic that was not found in the target cluster metadata.",
		},
		DefaultConfiguration: &sarifReportingConfiguration{
			Level: "error",
		},
		Properties: map[string]any{
			"tags": []string{"kafka", "reliability", "configuration"},
		},
	}
}

func buildUnusedTopicRule(level string) sarifRule {
	return sarifRule{
		ID:   sarifRuleIDUnusedTopic,
		Name: "Unused Kafka topic",
		ShortDescription: &sarifMessage{
			Text: "Topic has no active consumer groups",
		},
		FullDescription: &sarifMessage{
			Text: "The topic exists in Kafka but no active consumer groups are currently attached.",
		},
		DefaultConfiguration: &sarifReportingConfiguration{
			Level: level,
		},
		Properties: map[string]any{
			"tags": []string{"kafka", "cleanup", "cost"},
		},
	}
}

func buildCheckUnreferencedInRepoRule() sarifRule {
	return sarifRule{
		ID:   sarifRuleIDUnreferencedInRepo,
		Name: "Unreferenced topic in repository",
		ShortDescription: &sarifMessage{
			Text: "Topic exists in Kafka but was not found in repository scan",
		},
		FullDescription: &sarifMessage{
			Text: "The topic appears to be active in Kafka but has no references in scanned files.",
		},
		DefaultConfiguration: &sarifReportingConfiguration{
			Level: "warning",
		},
		Properties: map[string]any{
			"tags": []string{"kafka", "drift", "inventory"},
		},
	}
}

func buildHighRiskTopicRule() sarifRule {
	return sarifRule{
		ID:   sarifRuleIDHighRiskTopic,
		Name: "High-risk unused topic",
		ShortDescription: &sarifMessage{
			Text: "Unused topic classified as high risk",
		},
		FullDescription: &sarifMessage{
			Text: "Unused topic has high cleanup risk based on partition count or replication settings.",
		},
		DefaultConfiguration: &sarifReportingConfiguration{
			Level: "error",
		},
		Properties: map[string]any{
			"tags": []string{"kafka", "cleanup", "high-risk"},
		},
	}
}

func buildMediumRiskTopicRule() sarifRule {
	return sarifRule{
		ID:   sarifRuleIDMediumRiskTopic,
		Name: "Medium-risk unused topic",
		ShortDescription: &sarifMessage{
			Text: "Unused topic classified as medium risk",
		},
		FullDescription: &sarifMessage{
			Text: "Unused topic has medium cleanup risk and should be reviewed before deletion.",
		},
		DefaultConfiguration: &sarifReportingConfiguration{
			Level: "warning",
		},
		Properties: map[string]any{
			"tags": []string{"kafka", "cleanup", "medium-risk"},
		},
	}
}

func buildLowRiskTopicRule() sarifRule {
	return sarifRule{
		ID:   sarifRuleIDLowRiskTopic,
		Name: "Low-risk unused topic",
		ShortDescription: &sarifMessage{
			Text: "Unused topic classified as low risk",
		},
		FullDescription: &sarifMessage{
			Text: "Unused topic has low cleanup risk and can usually be removed after confirmation.",
		},
		DefaultConfiguration: &sarifReportingConfiguration{
			Level: "note",
		},
		Properties: map[string]any{
			"tags": []string{"kafka", "cleanup", "low-risk"},
		},
	}
}

type sarifReport struct {
	Schema  string     `json:"$schema,omitempty"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool               sarifTool                        `json:"tool"`
	Results            []sarifResult                    `json:"results,omitempty"`
	OriginalURIBaseIDs map[string]sarifArtifactLocation `json:"originalUriBaseIds,omitempty"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []sarifRule `json:"rules,omitempty"`
}

type sarifRule struct {
	ID                   string                       `json:"id"`
	Name                 string                       `json:"name,omitempty"`
	ShortDescription     *sarifMessage                `json:"shortDescription,omitempty"`
	FullDescription      *sarifMessage                `json:"fullDescription,omitempty"`
	DefaultConfiguration *sarifReportingConfiguration `json:"defaultConfiguration,omitempty"`
	Properties           map[string]any               `json:"properties,omitempty"`
}

type sarifReportingConfiguration struct {
	Level string `json:"level,omitempty"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	Level               string            `json:"level,omitempty"`
	Message             sarifMessage      `json:"message"`
	Locations           []sarifLocation   `json:"locations,omitempty"`
	PartialFingerprints map[string]string `json:"partialFingerprints,omitempty"`
	Properties          map[string]any    `json:"properties,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId,omitempty"`
}

type sarifRegion struct {
	StartLine int `json:"startLine,omitempty"`
}

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ppiankov/kafkaspectre/internal/kafka"
	"github.com/ppiankov/kafkaspectre/internal/reporter"
)

// WO-49: a topic with RF=1 on a multi-broker cluster is flagged.
func TestConfigRiskUnderReplicated(t *testing.T) {
	topic := &kafka.TopicInfo{
		Name:              "risky-topic",
		Partitions:        3,
		ReplicationFactor: 1,
		Config:            map[string]string{},
	}

	risks := reporter.AssessConfigRisk(topic, 3)
	if len(risks) != 1 {
		t.Fatalf("expected 1 risk, got %d", len(risks))
	}
	if risks[0].RiskType != "under_replicated" {
		t.Errorf("RiskType = %q, want under_replicated", risks[0].RiskType)
	}
	if risks[0].Severity != "high" {
		t.Errorf("Severity = %q, want high", risks[0].Severity)
	}
}

// WO-49: a topic with RF=1 on a single-broker cluster is fine.
func TestConfigRiskRFOneSingleBroker(t *testing.T) {
	topic := &kafka.TopicInfo{
		Name:              "dev-topic",
		Partitions:        1,
		ReplicationFactor: 1,
		Config:            map[string]string{},
	}

	risks := reporter.AssessConfigRisk(topic, 1)
	if len(risks) != 0 {
		t.Fatalf("expected 0 risks on single-broker cluster, got %d", len(risks))
	}
}

// WO-49: infinite retention is flagged.
func TestConfigRiskInfiniteRetention(t *testing.T) {
	topic := &kafka.TopicInfo{
		Name:              "growing-topic",
		Partitions:        1,
		ReplicationFactor: 3,
		Config:            map[string]string{"retention.ms": "-1"},
	}

	risks := reporter.AssessConfigRisk(topic, 3)
	found := false
	for _, r := range risks {
		if r.RiskType == "infinite_retention" {
			found = true
		}
	}
	if !found {
		t.Fatal("infinite retention not flagged")
	}
}

// WO-49: min.insync.replicas=1 with RF>=3 is flagged.
func TestConfigRiskWeakDurability(t *testing.T) {
	topic := &kafka.TopicInfo{
		Name:              "weak-topic",
		Partitions:        3,
		ReplicationFactor: 3,
		Config:            map[string]string{"min.insync.replicas": "1"},
	}

	risks := reporter.AssessConfigRisk(topic, 3)
	found := false
	for _, r := range risks {
		if r.RiskType == "weak_durability" {
			found = true
		}
	}
	if !found {
		t.Fatal("weak durability (min.insync.replicas=1 RF=3) not flagged")
	}
}

// WO-49: a well-configured topic produces no risks.
func TestConfigRiskHealthyTopic(t *testing.T) {
	topic := &kafka.TopicInfo{
		Name:              "healthy-topic",
		Partitions:        6,
		ReplicationFactor: 3,
		Config: map[string]string{
			"retention.ms":        "604800000",
			"min.insync.replicas": "2",
			"cleanup.policy":      "delete",
		},
	}

	risks := reporter.AssessConfigRisk(topic, 3)
	if len(risks) != 0 {
		t.Fatalf("healthy topic produced %d risks: %+v", len(risks), risks)
	}
}

// WO-49: config risks appear in the audit result and summary.
func TestConfigRisksInAuditResult(t *testing.T) {
	metadata := &kafka.ClusterMetadata{
		Brokers: []kafka.BrokerInfo{
			{ID: 1, Host: "b1", Port: 9092},
			{ID: 2, Host: "b2", Port: 9092},
			{ID: 3, Host: "b3", Port: 9092},
		},
		Topics: map[string]*kafka.TopicInfo{
			"risky": topic("risky", 3, 1),
			"safe":  topic("safe", 3, 3),
		},
		ConsumerGroups: map[string]*kafka.ConsumerGroupInfo{
			"cg": {GroupID: "cg", State: "Stable", Topics: []string{"risky", "safe"}},
		},
	}

	result := buildAuditResult(metadata, false, nil)

	if len(result.ConfigRisks) == 0 {
		t.Fatal("expected config risks, got 0")
	}
	if result.Summary.ConfigRisks == 0 {
		t.Fatal("summary.config_risks = 0, expected > 0")
	}

	foundRisky := false
	for _, cr := range result.ConfigRisks {
		if cr.Topic == "risky" && cr.RiskType == "under_replicated" {
			foundRisky = true
		}
	}
	if !foundRisky {
		t.Error("risky topic (RF=1 on 3-broker cluster) not flagged")
	}
}

// WO-50: SARIF output includes managed_by when set.
func TestSARIFIncludesManagedBy(t *testing.T) {
	result := &reporter.AuditResult{
		UnusedTopics: []*reporter.UnusedTopic{{
			Name:      "_schemas",
			Risk:      "low",
			Reason:    "managed",
			ManagedBy: "Confluent Schema Registry",
		}},
	}
	result.UnusedCount = 1

	var buf bytes.Buffer
	err := reporter.NewSARIFReporter(&buf, false).GenerateAudit(context.Background(), result)
	if err != nil {
		t.Fatalf("GenerateAudit: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"managed_by"`) {
		t.Error("SARIF output missing managed_by")
	}
	if !strings.Contains(out, "Confluent Schema Registry") {
		t.Error("SARIF output missing managed_by value")
	}
}

// WO-50: SpectreHub output includes managed_by when set.
func TestSpectreHubIncludesManagedBy(t *testing.T) {
	result := &reporter.AuditResult{
		UnusedTopics: []*reporter.UnusedTopic{{
			Name:      "_schemas",
			Risk:      "low",
			Reason:    "managed",
			ManagedBy: "Confluent Schema Registry",
		}},
	}
	result.UnusedCount = 1

	var buf bytes.Buffer
	err := reporter.NewSpectreHubReporter(&buf, "kafka:9092").GenerateAudit(context.Background(), result)
	if err != nil {
		t.Fatalf("GenerateAudit: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"managed_by"`) {
		t.Error("SpectreHub output missing managed_by")
	}
}

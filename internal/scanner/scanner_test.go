package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRepoScannerScan(t *testing.T) {
	repoDir := t.TempDir()

	mustWriteFile(t, filepath.Join(repoDir, "config", "app.yaml"), `kafka:
  topic: orders.events
  topics:
    - payments.completed
    - refunds.processed # inline comment
`)

	mustWriteFile(t, filepath.Join(repoDir, "config", "app.json"), `{
  "kafkaTopic": "inventory.updates",
  "topics": ["billing.v1", "shipping.v2"]
}`)

	mustWriteFile(t, filepath.Join(repoDir, ".env"), `KAFKA_TOPIC=deadletter.events
KAFKA_TOPICS=bulk.one,bulk.two
KAFKA_TOPIC_TEMPLATE=${KAFKA_TOPIC_NAME}
OTHER_KEY=ignore
`)

	mustWriteFile(t, filepath.Join(repoDir, "src", "main.go"), `package main

func main() {
	kafkaTopic := "source.events"
	_ = kafkaTopic
}
`)

	s := NewRepoScanner()
	result, err := s.Scan(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	if result.FilesScanned != 4 {
		t.Fatalf("FilesScanned = %d, want 4", result.FilesScanned)
	}

	wantTopics := []string{
		"orders.events",
		"payments.completed",
		"refunds.processed",
		"inventory.updates",
		"billing.v1",
		"shipping.v2",
		"deadletter.events",
		"bulk.one",
		"bulk.two",
		"source.events",
	}
	for _, topic := range wantTopics {
		if _, ok := result.Topics[topic]; !ok {
			t.Fatalf("expected topic %q to be detected", topic)
		}
	}

	if _, ok := result.Topics["KAFKA_TOPIC_NAME"]; ok {
		t.Fatalf("unexpected env variable placeholder detection")
	}

	if !hasSource(result, "orders.events", SourceYAMLJSON) {
		t.Fatalf("expected orders.events to include %q source", SourceYAMLJSON)
	}
	if !hasSource(result, "deadletter.events", SourceEnv) {
		t.Fatalf("expected deadletter.events to include %q source", SourceEnv)
	}
	if !hasSource(result, "source.events", SourceRegex) {
		t.Fatalf("expected source.events to include %q source", SourceRegex)
	}

	if !hasFile(result, "orders.events", "config/app.yaml") {
		t.Fatalf("expected orders.events reference in config/app.yaml")
	}
}

func TestRepoScannerScanMissingPath(t *testing.T) {
	s := NewRepoScanner()
	_, err := s.Scan(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatalf("expected error for missing repo path")
	}
}

func hasSource(result *Result, topic, source string) bool {
	ref, ok := result.Topics[topic]
	if !ok {
		return false
	}
	for _, occ := range ref.Occurrences {
		if occ.Source == source {
			return true
		}
	}
	return false
}

func hasFile(result *Result, topic, file string) bool {
	ref, ok := result.Topics[topic]
	if !ok {
		return false
	}
	for _, occ := range ref.Occurrences {
		if occ.File == file {
			return true
		}
	}
	return false
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

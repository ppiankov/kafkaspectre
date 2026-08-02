package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func scanDir(t *testing.T, files map[string]string) *Result {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	result, err := NewRepoScanner().Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	return result
}

func hasTopic(result *Result, topic string) bool {
	_, ok := result.Topics[topic]
	return ok
}

// WO-35: .properties is Kafka's native config format. Topics referenced only
// there were invisible to `check` and fed the unused-topic findings.
func TestScanPropertiesFile(t *testing.T) {
	result := scanDir(t, map[string]string{
		"kafka.properties": "topic=orders\n" +
			"# comment=ignored\n" +
			"! bang-comment=ignored\n" +
			"bootstrap.servers=kafka:9092\n",
	})

	if !hasTopic(result, "orders") {
		t.Fatalf("orders not found; topics = %v", result.Topics)
	}
	if hasTopic(result, "kafka:9092") {
		t.Fatal("bootstrap.servers value was misread as a topic")
	}
}

// WO-35: Spring Boot puts topics under namespaced keys.
func TestScanSpringApplicationProperties(t *testing.T) {
	result := scanDir(t, map[string]string{
		"src/main/resources/application.properties": "spring.kafka.template.default-topic=user-events\n" +
			"app.kafka.consumer.topics=billing-events,audit-events\n" +
			"spring.kafka.bootstrap-servers=kafka:9092\n",
	})

	for _, want := range []string{"user-events", "billing-events", "audit-events"} {
		if !hasTopic(result, want) {
			t.Fatalf("%q not found; topics = %v", want, result.Topics)
		}
	}
}

// WO-35: properties references must be attributed to their own source kind.
func TestPropertiesReferencesAreLabelled(t *testing.T) {
	result := scanDir(t, map[string]string{"kafka.properties": "topic=orders\n"})

	ref, ok := result.Topics["orders"]
	if !ok || len(ref.Occurrences) == 0 {
		t.Fatalf("no occurrence recorded for orders")
	}
	if got := ref.Occurrences[0].Source; got != SourceProperties {
		t.Fatalf("source = %q, want %q", got, SourceProperties)
	}
	if got := ref.Occurrences[0].Line; got != 1 {
		t.Fatalf("line = %d, want 1", got)
	}
}

// WO-35: JVM and Node source extensions are part of the Kafka ecosystem.
func TestScanAddedSourceExtensions(t *testing.T) {
	cases := map[string]string{
		"Consumer.kt":    "val topic = \"kotlin-events\"\n",
		"Consumer.scala": "val topic = \"scala-events\"\n",
		"consumer.ts":    "const topic = \"ts-events\";\n",
		"consumer.js":    "const topic = \"js-events\";\n",
	}

	for file, body := range cases {
		t.Run(file, func(t *testing.T) {
			result := scanDir(t, map[string]string{file: body})
			if len(result.Topics) == 0 {
				t.Fatalf("%s produced no topic references", file)
			}
		})
	}
}

// WO-35: a zero-topic result must be distinguishable from an all-unsupported
// repository.
func TestFilesSkippedCounted(t *testing.T) {
	result := scanDir(t, map[string]string{
		"README.md":  "# no topics here\n",
		"image.png":  "not really a png\n",
		"notes.txt":  "nothing\n",
		"app.config": "nothing\n",
	})

	if result.FilesScanned != 0 {
		t.Fatalf("files scanned = %d, want 0", result.FilesScanned)
	}
	if result.FilesSkipped != 4 {
		t.Fatalf("files skipped = %d, want 4", result.FilesSkipped)
	}
}

// WO-35: supported files must not be counted as skipped.
func TestSupportedFilesNotCountedAsSkipped(t *testing.T) {
	result := scanDir(t, map[string]string{"kafka.properties": "topic=orders\n"})

	if result.FilesScanned != 1 {
		t.Fatalf("files scanned = %d, want 1", result.FilesScanned)
	}
	if result.FilesSkipped != 0 {
		t.Fatalf("files skipped = %d, want 0", result.FilesSkipped)
	}
}

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// WO-35: scan directory fixture
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
// WO-35: properties file scan
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

// WO-35: the key-name filter is the whole feature. Without it, any properties
// key with a topic-shaped value becomes a phantom topic reference, and phantom
// references suppress genuine UNREFERENCED_IN_REPO findings in `check`.
// These values are all valid topic names, so only the key filter rejects them.
// WO-35: non-topic keys filtered
func TestPropertiesNonTopicKeysAreIgnored(t *testing.T) {
	result := scanDir(t, map[string]string{
		"app.properties": "spring.application.name=my-service\n" +
			"client.id=audit-client\n" +
			"group.id=orders-consumer\n" +
			"topic=real-topic\n",
	})

	for _, phantom := range []string{"my-service", "audit-client", "orders-consumer"} {
		if hasTopic(result, phantom) {
			t.Errorf("non-topic key produced phantom topic reference %q", phantom)
		}
	}
	if !hasTopic(result, "real-topic") {
		t.Fatalf("genuine topic key was not extracted; topics = %v", result.Topics)
	}
}

// WO-35: .properties comments use both # and !.
// WO-35: comments ignored
func TestPropertiesCommentsIgnored(t *testing.T) {
	result := scanDir(t, map[string]string{
		"c.properties": "# topic=hash-ghost\n! topic=bang-ghost\ntopic=real\n",
	})

	for _, ghost := range []string{"hash-ghost", "bang-ghost"} {
		if hasTopic(result, ghost) {
			t.Errorf("commented-out line produced topic reference %q", ghost)
		}
	}
	if !hasTopic(result, "real") {
		t.Fatal("uncommented topic was not extracted")
	}
}

// WO-35: Spring Boot puts topics under namespaced keys.
// WO-35: Spring properties scan
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
// WO-35: added source extensions
func TestScanAddedSourceExtensions(t *testing.T) {
	cases := map[string]string{
		"Consumer.kt":    "val topic = \"kotlin-events\"\n",
		"Consumer.scala": "val topic = \"scala-events\"\n",
		"consumer.ts":    "const topic = \"ts-events\";\n",
		"consumer.js":    "const topic = \"js-events\";\n",
	}

	want := map[string]string{
		"Consumer.kt":    "kotlin-events",
		"Consumer.scala": "scala-events",
		"consumer.ts":    "ts-events",
		"consumer.js":    "js-events",
	}

	for file, body := range cases {
		t.Run(file, func(t *testing.T) {
			result := scanDir(t, map[string]string{file: body})
			if !hasTopic(result, want[file]) {
				t.Fatalf("%s: %q not extracted; topics = %v", file, want[file], result.Topics)
			}
		})
	}
}

// WO-35: a zero-topic result must be distinguishable from an all-unsupported
// repository.
// WO-35: skipped files counted
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
// WO-35: supported not skipped
func TestSupportedFilesNotCountedAsSkipped(t *testing.T) {
	result := scanDir(t, map[string]string{"kafka.properties": "topic=orders\n"})

	if result.FilesScanned != 1 {
		t.Fatalf("files scanned = %d, want 1", result.FilesScanned)
	}
	if result.FilesSkipped != 0 {
		t.Fatalf("files skipped = %d, want 0", result.FilesSkipped)
	}
}

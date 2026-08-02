package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// publicDocs are the shipped documents whose command lines users copy-paste.
//
// WO-25/WO-40: docs/SKILL.md had a fail-closed test and was caught; README.md
// and docs/cli-reference.md did not, and both shipped commands that do not run
// (`--brokers`, `--format`, credentials-in-config). The two documents that
// drifted were exactly the two with no test. This closes that asymmetry.
var publicDocs = []string{
	"../../README.md",
	"../../docs/cli-reference.md",
	"../../docs/SKILL.md",
	"../../docs/cleanup-guide.md",
}

// commandLinePattern matches a shell line that INVOKES kafkaspectre. Prose such
// as "kafkaspectre operates in read-only mode" must not match, so the line has
// to begin with the binary name (after an optional prompt) and be inside a
// fenced code block.
var commandLinePattern = regexp.MustCompile(`^\s*(?:\$\s*)?(?:[./][\w./-]*/)?kafkaspectre\s+([a-z][a-z0-9-]*)(.*)$`)

// codeFencedCommandLines returns the kafkaspectre invocations in fenced blocks.
// WO-25: extract fenced command lines
func codeFencedCommandLines(doc string) [][]string {
	var out [][]string
	inFence := false

	lines := strings.Split(doc, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			continue
		}

		// Join shell line continuations so flags on wrapped lines are seen.
		joined := line
		for strings.HasSuffix(strings.TrimSpace(joined), `\\`) && i+1 < len(lines) {
			i++
			joined = strings.TrimSuffix(strings.TrimSpace(joined), `\\`) + " " + lines[i]
		}

		if match := commandLinePattern.FindStringSubmatch(joined); match != nil {
			out = append(out, match)
		}
	}

	return out
}

// WO-25: guard public docs against drift
func TestPublicDocsInvokeRealCommandsAndFlags(t *testing.T) {
	root := newRootCmd()

	commands := map[string]bool{}
	for _, sub := range root.Commands() {
		commands[sub.Name()] = true
	}

	flagDefined := func(name string) bool {
		if root.PersistentFlags().Lookup(name) != nil {
			return true
		}
		for _, sub := range root.Commands() {
			if sub.Flags().Lookup(name) != nil {
				return true
			}
		}
		return false
	}

	flagPattern := regexp.MustCompile(`--([a-z][a-z0-9-]*)`)

	for _, docPath := range publicDocs {
		data, err := os.ReadFile(filepath.Clean(docPath))
		if err != nil {
			t.Fatalf("read %s: %v", docPath, err)
		}

		found := 0
		for _, match := range codeFencedCommandLines(string(data)) {
			command, rest := match[1], match[2]
			if !commands[command] {
				t.Errorf("%s invokes `kafkaspectre %s`, which is not a command", filepath.Base(docPath), command)
				continue
			}
			found++

			for _, flagMatch := range flagPattern.FindAllStringSubmatch(rest, -1) {
				if !flagDefined(flagMatch[1]) {
					t.Errorf("%s invokes `kafkaspectre %s` with --%s, which no command defines",
						filepath.Base(docPath), command, flagMatch[1])
				}
			}
		}

		if found == 0 {
			t.Errorf("%s contains no parsed kafkaspectre invocation; the parser or doc structure changed", filepath.Base(docPath))
		}
	}
}

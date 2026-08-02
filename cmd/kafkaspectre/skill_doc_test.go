package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const skillDocPath = "../../docs/SKILL.md"

// commandHeadingPattern matches "### kafkaspectre <command>" headings.
var commandHeadingPattern = regexp.MustCompile(`(?m)^#+\s+kafkaspectre\s+([a-z][a-z0-9-]*)\b.*$`)

// commandInvocationPattern matches `kafkaspectre <command>` inside examples.
var commandInvocationPattern = regexp.MustCompile(`(?m)\bkafkaspectre\s+([a-z][a-z0-9-]*)\b`)

// docFlagPattern matches a long flag mentioned anywhere in the document.
var docFlagPattern = regexp.MustCompile(`--([a-z][a-z0-9-]*)`)

// WO-25: read SKILL.md helper
func readSkillDoc(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(skillDocPath))
	if err != nil {
		t.Fatalf("read %s: %v", skillDocPath, err)
	}
	return string(data)
}

func knownCommands(t *testing.T) map[string]*cobra.Command {
	t.Helper()
	root := newRootCmd()
	out := make(map[string]*cobra.Command)
	for _, sub := range root.Commands() {
		out[sub.Name()] = sub
	}
	return out
}

// WO-25: docs/SKILL.md is the ANCC machine-consumption contract — it exists so
// AGENTS can drive this CLI. It previously documented a `scan` command and an
// `init` command, neither of which exists, so every documented invocation
// failed. This test fails the build if the doc drifts from the binary again.
// WO-25: guard SKILL.md commands
func TestSkillDocCommandsExist(t *testing.T) {
	doc := readSkillDoc(t)
	commands := knownCommands(t)

	matches := commandHeadingPattern.FindAllStringSubmatch(doc, -1)
	if len(matches) == 0 {
		t.Fatal("no command headings found in SKILL.md; the parser or the doc structure changed")
	}

	// Commands invoked in examples must resolve too — the original bad doc put
	// `kafkaspectre scan --output json` in a parsing example, not only a heading.
	matches = append(matches, commandInvocationPattern.FindAllStringSubmatch(doc, -1)...)

	for _, match := range matches {
		name := match[1]
		if _, ok := commands[name]; !ok {
			available := make([]string, 0, len(commands))
			for cmd := range commands {
				available = append(available, cmd)
			}
			sort.Strings(available)
			t.Errorf("SKILL.md documents command %q which does not exist; available: %v", name, available)
		}
	}
}

// WO-25: `--format json` and `--baseline path` were documented but never
// implemented, so an agent following the doc got "unknown flag".
// WO-25: guard SKILL.md flags
func TestSkillDocFlagsExist(t *testing.T) {
	doc := readSkillDoc(t)
	root := newRootCmd()

	// A flag is valid if the root command or any subcommand defines it.
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

	// Flags belonging to tools other than kafkaspectre appear in piped
	// examples; only check flags on a kafkaspectre invocation or in a bullet.
	ignored := map[string]bool{
		"arg": true, // jq -e style placeholders, defensive
	}

	seen := make(map[string]bool)
	for _, line := range strings.Split(doc, "\n") {
		if strings.Contains(line, "| jq") {
			line = strings.SplitN(line, "| jq", 2)[0]
		}
		for _, match := range docFlagPattern.FindAllStringSubmatch(line, -1) {
			name := match[1]
			if ignored[name] || seen[name] {
				continue
			}
			seen[name] = true
			if !flagDefined(name) {
				t.Errorf("SKILL.md documents flag --%s which no command defines", name)
			}
		}
	}

	if len(seen) == 0 {
		t.Fatal("no flags found in SKILL.md; the parser or the doc structure changed")
	}
}

// WO-25: the doc claimed exit codes 0/1/2 while the binary uses 0/1/2/3/5/6.
// "findings detected" in particular was documented as 1 but is really 6, so a
// CI gate keying on the documented value would misread every run.
// WO-25: guard exit codes
func TestSkillDocExitCodesMatchConstants(t *testing.T) {
	doc := readSkillDoc(t)

	for _, want := range []struct {
		code int
		hint string
	}{
		{ExitSuccess, "no findings"},
		{ExitInternal, "internal error"},
		{ExitInvalidArg, "invalid arguments"},
		{ExitNotFound, "not found"},
		{ExitNetwork, "network error"},
		{ExitFindings, "findings detected"},
	} {
		line := regexp.MustCompile(`(?m)^-\s*` + strconv.Itoa(want.code) + `:\s*(.+)$`)
		match := line.FindStringSubmatch(doc)
		if match == nil {
			t.Errorf("SKILL.md does not document exit code %d (%s)", want.code, want.hint)
			continue
		}
		if !strings.Contains(strings.ToLower(match[1]), want.hint) {
			t.Errorf("exit code %d documented as %q, want it to mention %q", want.code, match[1], want.hint)
		}
	}
}

// WO-25: the doc described the tool as an ACL auditor. No ACL logic exists.
// WO-25: guard description
func TestSkillDocDescribesTheRightTool(t *testing.T) {
	doc := strings.ToLower(readSkillDoc(t))

	// Word-boundary match: a substring test also fires on "oracle" and
	// "obstacle" — the project's own tagline is "Mirrors, not oracles", so a
	// substring test would fail the build on a legitimate edit.
	if regexp.MustCompile(`(?i)\bacls?\b`).MatchString(doc) {
		t.Error("SKILL.md claims ACL auditing; no ACL logic exists in this tool")
	}
	if !strings.Contains(doc, "unused") {
		t.Error("SKILL.md does not mention unused-topic auditing, which is what this tool does")
	}
}

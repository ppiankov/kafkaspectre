package scanner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Scanner scans a repository for Kafka topic references.
type Scanner interface {
	Scan(ctx context.Context, repoPath string) (*Result, error)
}

// Result contains discovered topic references and scan metadata.
type Result struct {
	RepoPath     string                     `json:"repo_path"`
	FilesScanned int                        `json:"files_scanned"`
	Topics       map[string]*TopicReference `json:"topics"`
}

// TopicReference aggregates all occurrences for a topic.
type TopicReference struct {
	Topic       string      `json:"topic"`
	Occurrences []Reference `json:"occurrences"`
}

// Reference is a single occurrence of a topic reference.
type Reference struct {
	Topic  string `json:"topic"`
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Source string `json:"source"`
}

const (
	SourceYAMLJSON = "yaml_json"
	SourceEnv      = "env"
	SourceRegex    = "source_regex"
)

type scanMode int

const (
	scanNone scanMode = iota
	scanConfig
	scanEnv
	scanSource
)

var (
	topicConfigLinePattern = regexp.MustCompile(`(?i)^\s*(?:-\s*)?["']?([A-Za-z0-9_.-]*topic[s]?[A-Za-z0-9_.-]*)["']?\s*[:=]\s*(.*?)\s*,?\s*$`)
	envLinePattern         = regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$`)
	quotedTokenPattern     = regexp.MustCompile("[\"'`]([A-Za-z0-9._-]{3,249})[\"'`]")
	plainTokenPattern      = regexp.MustCompile(`[A-Za-z0-9._-]{3,249}`)
)

// RepoScanner scans source repositories for topic references.
type RepoScanner struct {
	maxFileSize int64
	skipDirs    map[string]struct{}
}

// NewRepoScanner returns the default repository scanner.
func NewRepoScanner() *RepoScanner {
	return &RepoScanner{
		maxFileSize: 2 * 1024 * 1024,
		skipDirs: map[string]struct{}{
			".git":         {},
			".idea":        {},
			".vscode":      {},
			".venv":        {},
			"node_modules": {},
			"vendor":       {},
			"dist":         {},
			"build":        {},
			"target":       {},
			"bin":          {},
		},
	}
}

// Scan walks the repository and extracts topic references from supported files.
func (s *RepoScanner) Scan(ctx context.Context, repoPath string) (*Result, error) {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return nil, errors.New("repo path is required")
	}

	absRepoPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("resolve repo path: %w", err)
	}

	info, err := os.Stat(absRepoPath)
	if err != nil {
		return nil, fmt.Errorf("repo path %q: %w", repoPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repo path %q is not a directory", repoPath)
	}

	result := &Result{
		RepoPath: absRepoPath,
		Topics:   make(map[string]*TopicReference),
	}
	dedupe := make(map[string]map[string]struct{})

	err = filepath.WalkDir(absRepoPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		if d.IsDir() {
			if s.shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}

		mode := detectScanMode(path)
		if mode == scanNone {
			return nil
		}

		fileInfo, err := d.Info()
		if err != nil {
			return err
		}
		if fileInfo.Size() > s.maxFileSize {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result.FilesScanned++

		relPath, err := filepath.Rel(absRepoPath, path)
		if err != nil {
			relPath = path
		}
		relPath = filepath.ToSlash(relPath)

		var refs []Reference
		switch mode {
		case scanConfig:
			refs, err = scanConfigFile(content)
		case scanEnv:
			refs, err = scanEnvFile(content)
		case scanSource:
			refs, err = scanSourceFile(content)
		default:
			return nil
		}
		if err != nil {
			return fmt.Errorf("scan %s: %w", relPath, err)
		}

		for _, ref := range refs {
			ref.File = relPath
			addReference(result, dedupe, ref)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, topicRef := range result.Topics {
		sort.Slice(topicRef.Occurrences, func(i, j int) bool {
			left := topicRef.Occurrences[i]
			right := topicRef.Occurrences[j]
			if left.File != right.File {
				return left.File < right.File
			}
			if left.Line != right.Line {
				return left.Line < right.Line
			}
			return left.Source < right.Source
		})
	}

	return result, nil
}

func (s *RepoScanner) shouldSkipDir(name string) bool {
	_, skip := s.skipDirs[strings.ToLower(name)]
	return skip
}

func detectScanMode(path string) scanMode {
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))

	switch {
	case base == ".env" || strings.HasPrefix(base, ".env."):
		return scanEnv
	case ext == ".yaml" || ext == ".yml" || ext == ".json":
		return scanConfig
	case ext == ".go" || ext == ".py" || ext == ".java":
		return scanSource
	default:
		return scanNone
	}
}

func scanConfigFile(content []byte) ([]Reference, error) {
	lines := bufio.NewScanner(bytes.NewReader(content))
	lines.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	refs := make([]Reference, 0)
	pendingTopicListIndent := -1

	for lineNo := 1; lines.Scan(); lineNo++ {
		line := lines.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue
		}

		if pendingTopicListIndent >= 0 {
			indent := leadingIndent(line)
			if indent > pendingTopicListIndent && strings.HasPrefix(strings.TrimSpace(line), "-") {
				value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
				for _, topic := range extractTopicCandidates(value) {
					refs = append(refs, Reference{Topic: topic, Line: lineNo, Source: SourceYAMLJSON})
				}
				continue
			}
			if indent <= pendingTopicListIndent {
				pendingTopicListIndent = -1
			}
		}

		match := topicConfigLinePattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}

		value := strings.TrimSpace(match[2])
		if value == "" || value == "|" || value == ">" {
			pendingTopicListIndent = leadingIndent(line)
			continue
		}

		for _, topic := range extractTopicCandidates(value) {
			refs = append(refs, Reference{Topic: topic, Line: lineNo, Source: SourceYAMLJSON})
		}
	}

	if err := lines.Err(); err != nil {
		return nil, err
	}

	return refs, nil
}

func scanEnvFile(content []byte) ([]Reference, error) {
	lines := bufio.NewScanner(bytes.NewReader(content))
	lines.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	refs := make([]Reference, 0)

	for lineNo := 1; lines.Scan(); lineNo++ {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		match := envLinePattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}

		key := strings.ToUpper(match[1])
		if !strings.Contains(key, "TOPIC") {
			continue
		}

		value := strings.TrimSpace(stripInlineComment(match[2]))
		for _, topic := range extractTopicCandidates(value) {
			refs = append(refs, Reference{Topic: topic, Line: lineNo, Source: SourceEnv})
		}
	}

	if err := lines.Err(); err != nil {
		return nil, err
	}

	return refs, nil
}

func scanSourceFile(content []byte) ([]Reference, error) {
	lines := bufio.NewScanner(bytes.NewReader(content))
	lines.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	refs := make([]Reference, 0)

	for lineNo := 1; lines.Scan(); lineNo++ {
		line := lines.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "*") {
			continue
		}

		lower := strings.ToLower(line)
		if !strings.Contains(lower, "topic") && !strings.Contains(lower, "kafka") {
			continue
		}

		matches := quotedTokenPattern.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			if len(m) != 2 {
				continue
			}
			topic := m[1]
			if !isLikelyTopic(topic, line) {
				continue
			}
			refs = append(refs, Reference{Topic: topic, Line: lineNo, Source: SourceRegex})
		}
	}

	if err := lines.Err(); err != nil {
		return nil, err
	}

	return refs, nil
}

func addReference(result *Result, dedupe map[string]map[string]struct{}, ref Reference) {
	if ref.Topic == "" {
		return
	}

	topicRef, exists := result.Topics[ref.Topic]
	if !exists {
		topicRef = &TopicReference{Topic: ref.Topic}
		result.Topics[ref.Topic] = topicRef
	}

	if _, ok := dedupe[ref.Topic]; !ok {
		dedupe[ref.Topic] = make(map[string]struct{})
	}

	key := fmt.Sprintf("%s:%d:%s", ref.File, ref.Line, ref.Source)
	if _, seen := dedupe[ref.Topic][key]; seen {
		return
	}

	dedupe[ref.Topic][key] = struct{}{}
	topicRef.Occurrences = append(topicRef.Occurrences, ref)
}

func extractTopicCandidates(value string) []string {
	value = strings.TrimSpace(stripInlineComment(value))
	if value == "" {
		return nil
	}

	// Skip simple env interpolation placeholders like ${KAFKA_TOPIC}.
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		return nil
	}

	matches := plainTokenPattern.FindAllString(value, -1)
	if len(matches) == 0 {
		return nil
	}

	out := make([]string, 0, len(matches))
	seen := make(map[string]struct{})
	for _, match := range matches {
		if !isLikelyTopic(match, value) {
			continue
		}
		if _, ok := seen[match]; ok {
			continue
		}
		seen[match] = struct{}{}
		out = append(out, match)
	}

	return out
}

func isLikelyTopic(candidate, source string) bool {
	if len(candidate) < 3 {
		return false
	}
	if _, err := strconv.Atoi(candidate); err == nil {
		return false
	}
	if strings.HasPrefix(strings.ToLower(candidate), "http") {
		return false
	}
	if strings.ContainsAny(candidate, "{}[]()$") {
		return false
	}
	if strings.Contains(source, "${"+candidate+"}") {
		return false
	}
	if isAllUpperUnderscore(candidate) && !strings.Contains(candidate, ".") && !strings.Contains(candidate, "-") {
		return false
	}

	switch strings.ToLower(candidate) {
	case "topic", "topics", "kafka", "true", "false", "null", "nil", "none", "default", "latest", "earliest", "name", "value", "string":
		return false
	}

	return true
}

func isAllUpperUnderscore(s string) bool {
	hasLetter := false
	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			hasLetter = true
		case unicode.IsDigit(r), r == '_':
			continue
		default:
			return false
		}
	}
	return hasLetter
}

func stripInlineComment(value string) string {
	inSingle := false
	inDouble := false
	for i, r := range value {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return strings.TrimSpace(value[:i])
			}
		}
	}
	return strings.TrimSpace(value)
}

func leadingIndent(line string) int {
	indent := 0
	for _, r := range line {
		switch r {
		case ' ':
			indent++
		case '\t':
			indent += 2
		default:
			return indent
		}
	}
	return indent
}

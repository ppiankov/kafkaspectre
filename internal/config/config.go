package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultFileName is the primary config file name that is auto-discovered.
	DefaultFileName = ".kafkaspectre.yaml"
	alternateName   = ".kafkaspectre.yml"
)

const (
	// UsernameEnvVar and PasswordEnvVar name the environment variables that
	// supply SASL credentials.
	//
	// WO-34: auth_mechanism was configurable but its mandatory companions were
	// not, so a config file naming a mechanism made EVERY invocation fail until
	// the user also passed --username and --password. Credentials are read from
	// the environment rather than the config file so a secured cluster can be
	// expressed without writing a password to disk.
	UsernameEnvVar = "KAFKASPECTRE_USERNAME"
	PasswordEnvVar = "KAFKASPECTRE_PASSWORD"
)

// Config holds defaults loaded from .kafkaspectre.yaml.
//
// WO-34: this struct deliberately has no username or password field. Adding one
// would invite credentials into a plaintext file on disk; use the environment
// variables above instead.
type Config struct {
	BootstrapServers string
	AuthMechanism    string
	ExcludeTopics    []string
	ExcludeInternal  *bool
	Format           string
	Timeout          time.Duration
	HasTimeout       bool

	TLSEnabled  *bool
	TLSCertFile string
	TLSKeyFile  string
	TLSCAFile   string

	// ManagedTopics are operator-declared glob patterns for backing topics this
	// tool cannot recognise by name — renamed Connect topics, custom Streams
	// application IDs. WO-41: name-based recognition is best-effort, so a
	// deployment that renames its backing topics must be able to declare them.
	ManagedTopics []string
}

// CredentialsFromEnv returns SASL credentials supplied via the environment.
//
// WO-34: the returned values are secrets. Callers must never log them.
func CredentialsFromEnv() (username, password string) {
	return os.Getenv(UsernameEnvVar), os.Getenv(PasswordEnvVar)
}

// Load auto-discovers and loads a config file.
// Search order:
// 1) current working directory
// 2) user home directory
func Load() (*Config, string, error) {
	paths, err := defaultPaths()
	if err != nil {
		return nil, "", err
	}

	for _, path := range paths {
		cfg, found, err := loadOptionalPath(path)
		if err != nil {
			return nil, "", err
		}
		if found {
			return cfg, path, nil
		}
	}

	return nil, "", nil
}

// LoadFromPath loads and parses a config file from an explicit path.
func LoadFromPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	return cfg, nil
}

func defaultPaths() ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve current directory: %w", err)
	}

	paths := []string{
		filepath.Join(cwd, DefaultFileName),
		filepath.Join(cwd, alternateName),
	}

	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		homeDefault := filepath.Join(home, DefaultFileName)
		homeAlt := filepath.Join(home, alternateName)
		if !containsPath(paths, homeDefault) {
			paths = append(paths, homeDefault)
		}
		if !containsPath(paths, homeAlt) {
			paths = append(paths, homeAlt)
		}
	}

	return paths, nil
}

func containsPath(paths []string, target string) bool {
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}

func loadOptionalPath(path string) (*Config, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg, err := parse(data)
	if err != nil {
		return nil, false, fmt.Errorf("parse config %q: %w", path, err)
	}

	return cfg, true, nil
}

func parse(data []byte) (*Config, error) {
	cfg := &Config{}
	text := strings.TrimPrefix(string(data), "\uFEFF")
	lines := strings.Split(text, "\n")

	for i := 0; i < len(lines); i++ {
		lineNum := i + 1
		line := strings.TrimRight(lines[i], "\r")
		line = stripInlineComment(line)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "-") {
			return nil, fmt.Errorf("line %d: unexpected list item", lineNum)
		}

		keyPart, valuePart, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key: value", lineNum)
		}

		key := strings.TrimSpace(keyPart)
		value := strings.TrimSpace(valuePart)

		switch key {
		case "bootstrap_servers":
			scalar, err := parseScalar(value)
			if err != nil {
				return nil, fmt.Errorf("line %d: parse bootstrap_servers: %w", lineNum, err)
			}
			cfg.BootstrapServers = strings.TrimSpace(scalar)
		case "auth_mechanism":
			scalar, err := parseScalar(value)
			if err != nil {
				return nil, fmt.Errorf("line %d: parse auth_mechanism: %w", lineNum, err)
			}
			cfg.AuthMechanism = strings.TrimSpace(scalar)
		case "exclude_topics":
			if value == "" {
				items, next, err := parseBlockList(lines, i+1)
				if err != nil {
					return nil, err
				}
				cfg.ExcludeTopics = append(cfg.ExcludeTopics, items...)
				i = next - 1
				continue
			}

			items, err := parseInlineList(value)
			if err != nil {
				return nil, fmt.Errorf("line %d: parse exclude_topics: %w", lineNum, err)
			}
			cfg.ExcludeTopics = append(cfg.ExcludeTopics, items...)
		case "managed_topics":
			if value == "" {
				items, next, err := parseBlockList(lines, i+1)
				if err != nil {
					return nil, err
				}
				cfg.ManagedTopics = append(cfg.ManagedTopics, items...)
				i = next - 1
				continue
			}

			items, err := parseInlineList(value)
			if err != nil {
				return nil, fmt.Errorf("line %d: parse managed_topics: %w", lineNum, err)
			}
			cfg.ManagedTopics = append(cfg.ManagedTopics, items...)
		case "exclude_internal":
			scalar, err := parseScalar(value)
			if err != nil {
				return nil, fmt.Errorf("line %d: parse exclude_internal: %w", lineNum, err)
			}
			boolValue, err := strconv.ParseBool(strings.TrimSpace(scalar))
			if err != nil {
				return nil, fmt.Errorf("line %d: parse exclude_internal as bool: %w", lineNum, err)
			}
			cfg.ExcludeInternal = &boolValue
		case "tls":
			scalar, err := parseScalar(value)
			if err != nil {
				return nil, fmt.Errorf("line %d: parse tls: %w", lineNum, err)
			}
			boolValue, err := strconv.ParseBool(strings.TrimSpace(scalar))
			if err != nil {
				return nil, fmt.Errorf("line %d: parse tls as bool: %w", lineNum, err)
			}
			cfg.TLSEnabled = &boolValue
		case "tls_cert":
			scalar, err := parseScalar(value)
			if err != nil {
				return nil, fmt.Errorf("line %d: parse tls_cert: %w", lineNum, err)
			}
			cfg.TLSCertFile = strings.TrimSpace(scalar)
		case "tls_key":
			scalar, err := parseScalar(value)
			if err != nil {
				return nil, fmt.Errorf("line %d: parse tls_key: %w", lineNum, err)
			}
			cfg.TLSKeyFile = strings.TrimSpace(scalar)
		case "tls_ca":
			scalar, err := parseScalar(value)
			if err != nil {
				return nil, fmt.Errorf("line %d: parse tls_ca: %w", lineNum, err)
			}
			cfg.TLSCAFile = strings.TrimSpace(scalar)
		case "username", "password":
			// WO-34: refuse credentials in the config file rather than silently
			// ignoring them — a user who writes a password here must be told it
			// will not be read, not left believing it works.
			return nil, fmt.Errorf("line %d: %q must not be set in the config file; use the %s and %s environment variables",
				lineNum, key, UsernameEnvVar, PasswordEnvVar)
		case "format":
			scalar, err := parseScalar(value)
			if err != nil {
				return nil, fmt.Errorf("line %d: parse format: %w", lineNum, err)
			}
			cfg.Format = strings.ToLower(strings.TrimSpace(scalar))
		case "timeout":
			scalar, err := parseScalar(value)
			if err != nil {
				return nil, fmt.Errorf("line %d: parse timeout: %w", lineNum, err)
			}
			duration, err := time.ParseDuration(strings.TrimSpace(scalar))
			if err != nil {
				return nil, fmt.Errorf("line %d: parse timeout as duration: %w", lineNum, err)
			}
			cfg.Timeout = duration
			cfg.HasTimeout = true
		default:
			return nil, fmt.Errorf("line %d: unknown key %q", lineNum, key)
		}
	}

	cfg.ExcludeTopics = normalizeList(cfg.ExcludeTopics)
	cfg.ManagedTopics = normalizeList(cfg.ManagedTopics)

	return cfg, nil
}

// parseBlockList reads a YAML block sequence following a list-valued key.
//
// WO-33: the list terminator used to be "line has no leading whitespace", which
// also rejected the equally valid YAML form where sequence items sit at the
// SAME indentation as their parent key. That made a legal config file fail with
// "unexpected list item" and blocked every command until the user re-indented.
// The terminator is now "line is not a sequence item", so both forms parse.
func parseBlockList(lines []string, start int) ([]string, int, error) {
	items := make([]string, 0)

	for i := start; i < len(lines); i++ {
		lineNum := i + 1
		line := strings.TrimRight(lines[i], "\r")
		line = stripInlineComment(line)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// End of list, start of the next key. A sequence item at any
		// indentation still belongs to this list.
		if !strings.HasPrefix(trimmed, "-") {
			return items, i, nil
		}

		item := trimmed

		item = strings.TrimSpace(strings.TrimPrefix(item, "-"))
		if item == "" {
			return nil, 0, fmt.Errorf("line %d: empty list item for exclude_topics", lineNum)
		}

		scalar, err := parseScalar(item)
		if err != nil {
			return nil, 0, fmt.Errorf("line %d: parse exclude_topics item: %w", lineNum, err)
		}
		items = append(items, scalar)
	}

	return items, len(lines), nil
}

func parseInlineList(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	// Allow scalar as shorthand for a single pattern.
	if !strings.HasPrefix(value, "[") {
		scalar, err := parseScalar(value)
		if err != nil {
			return nil, err
		}
		return []string{scalar}, nil
	}

	if !strings.HasSuffix(value, "]") {
		return nil, errors.New("inline list must end with ]")
	}

	inner := strings.TrimSpace(value[1 : len(value)-1])
	if inner == "" {
		return nil, nil
	}

	parts, err := splitCSV(inner)
	if err != nil {
		return nil, err
	}

	items := make([]string, 0, len(parts))
	for _, part := range parts {
		scalar, err := parseScalar(part)
		if err != nil {
			return nil, err
		}
		items = append(items, scalar)
	}

	return items, nil
}

func splitCSV(input string) ([]string, error) {
	parts := make([]string, 0)
	var current strings.Builder

	inSingle := false
	inDouble := false
	escaped := false

	flush := func() {
		parts = append(parts, current.String())
		current.Reset()
	}

	for _, r := range input {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && inDouble:
			current.WriteRune(r)
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			current.WriteRune(r)
		case r == '"' && !inSingle:
			inDouble = !inDouble
			current.WriteRune(r)
		case r == ',' && !inSingle && !inDouble:
			flush()
		default:
			current.WriteRune(r)
		}
	}

	if inSingle || inDouble {
		return nil, errors.New("unterminated quoted string in inline list")
	}

	flush()
	return parts, nil
}

func parseScalar(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", err
		}
		return parsed, nil
	}

	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1], nil
	}

	if strings.HasPrefix(value, "'") || strings.HasPrefix(value, "\"") ||
		strings.HasSuffix(value, "'") || strings.HasSuffix(value, "\"") {
		return "", errors.New("unterminated quoted string")
	}

	return value, nil
}

func stripInlineComment(line string) string {
	inSingle := false
	inDouble := false
	escaped := false

	for i, r := range line {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inDouble:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case r == '#' && !inSingle && !inDouble:
			return line[:i]
		}
	}

	return line
}

func normalizeList(items []string) []string {
	if len(items) == 0 {
		return nil
	}

	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

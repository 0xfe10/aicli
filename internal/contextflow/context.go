// Package contextflow selects isolated account configuration for service CLIs.
package contextflow

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	DefaultName   = "default"
	SourceDefault = "default"
	SourceCurrent = "current"
	SourceEnv     = "environment"
	SourceFlag    = "flag"

	dirPerm  = 0o700
	filePerm = 0o600
)

// Manager owns context selection for one service CLI.
type Manager struct {
	Service string
	EnvVar  string
}

// Selection is the context snapshot used for one CLI process.
type Selection struct {
	Name      string
	Source    string
	ConfigDir string
	CacheDir  string
}

// New creates a service context manager.
func New(service, envVar string) Manager {
	return Manager{Service: service, EnvVar: envVar}
}

// DefaultSelection returns the legacy-compatible default context.
func (m Manager) DefaultSelection() Selection {
	return m.selection(DefaultName, SourceDefault)
}

// ResolveArgs removes a leading --context flag and resolves the active context.
func (m Manager) ResolveArgs(args []string) (Selection, []string, error) {
	name, filtered, found, err := extractContextFlag(args)
	if err != nil {
		return Selection{}, nil, err
	}
	if found {
		if err := ValidateName(name); err != nil {
			return Selection{}, nil, err
		}
		return m.selection(name, SourceFlag), filtered, nil
	}
	if name = strings.TrimSpace(os.Getenv(m.EnvVar)); name != "" {
		if err := ValidateName(name); err != nil {
			return Selection{}, nil, fmt.Errorf("%s: %w", m.EnvVar, err)
		}
		return m.selection(name, SourceEnv), filtered, nil
	}
	name, found, err = m.readCurrent()
	if err != nil {
		return Selection{}, nil, err
	}
	if found {
		return m.selection(name, SourceCurrent), filtered, nil
	}
	return m.DefaultSelection(), filtered, nil
}

// ValidateName rejects names that could escape the contexts directory.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("context name is required")
	}
	if len(name) > 64 {
		return fmt.Errorf("context name must not exceed 64 characters")
	}
	for i, ch := range name {
		valid := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9'
		if i > 0 && (ch == '.' || ch == '_' || ch == '-') {
			valid = true
		}
		if !valid {
			return fmt.Errorf("invalid context name %q: use letters, numbers, dots, underscores, or hyphens", name)
		}
	}
	return nil
}

// Use persists name as the current context and creates its secure state directory.
func (m Manager) Use(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	selection := m.selection(name, SourceCurrent)
	if selection.ConfigDir == "" {
		return fmt.Errorf("%s context path is unavailable", m.Service)
	}
	if err := m.Prepare(selection); err != nil {
		return err
	}
	root := m.configRoot()
	currentPath := filepath.Join(root, "current-context")
	if info, err := os.Lstat(currentPath); err == nil {
		if err := validateSecureFile(currentPath, info); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s current context: %w", m.Service, err)
	}
	return writeCurrentAtomic(currentPath, name)
}

// Prepare securely creates the directory chain for a named context.
func (m Manager) Prepare(selection Selection) error {
	if err := ValidateName(selection.Name); err != nil {
		return err
	}
	if selection.ConfigDir == "" {
		return nil
	}
	if selection.Name == DefaultName {
		if err := prepareLegacyState(selection.ConfigDir); err != nil {
			return err
		}
		if selection.CacheDir != "" {
			return prepareLegacyState(selection.CacheDir)
		}
		return nil
	}
	if err := prepareNamedState(selection.ConfigDir); err != nil {
		return err
	}
	if selection.CacheDir != "" {
		if err := prepareNamedState(selection.CacheDir); err != nil {
			return err
		}
	}
	return nil
}

// List returns known context names, always including default and current.
func (m Manager) List(current string) ([]string, error) {
	seen := map[string]bool{DefaultName: true}
	if current != "" {
		seen[current] = true
	}
	root := m.configRoot()
	if root == "" {
		return sortedNames(seen), nil
	}
	if err := validateProjectRoot(root); err != nil {
		return nil, err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return sortedNames(seen), nil
		}
		return nil, fmt.Errorf("stat %s context directory: %w", m.Service, err)
	}
	if err := validateSecureDir(root, rootInfo); err != nil {
		return nil, err
	}
	contextsDir := filepath.Join(root, "contexts")
	info, err := os.Lstat(contextsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return sortedNames(seen), nil
		}
		return nil, fmt.Errorf("stat %s contexts: %w", m.Service, err)
	}
	if err := validateSecureDir(contextsDir, info); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(contextsDir)
	if err != nil {
		return nil, fmt.Errorf("read %s contexts: %w", m.Service, err)
	}
	for _, entry := range entries {
		if err := ValidateName(entry.Name()); err != nil || entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		seen[entry.Name()] = true
	}
	return sortedNames(seen), nil
}

// MaybeRun handles the local context command before Restish sees argv.
func MaybeRun(args []string, manager Manager, selection Selection, stdout, stderr io.Writer) (bool, error) {
	commandArgs, handled := localCommandArgs(args, "context")
	if !handled {
		return false, nil
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if len(commandArgs) == 0 || isHelp(commandArgs[0]) {
		fmt.Fprint(stdout, helpText(manager.Service))
		return true, nil
	}
	switch commandArgs[0] {
	case "current":
		if len(commandArgs) != 1 {
			return true, fmt.Errorf("context current does not accept arguments")
		}
		fmt.Fprintln(stdout, selection.Name)
	case "list":
		if len(commandArgs) != 1 {
			return true, fmt.Errorf("context list does not accept arguments")
		}
		names, err := manager.List(selection.Name)
		if err != nil {
			return true, err
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(struct {
			Current  string   `json:"current"`
			Source   string   `json:"source"`
			Contexts []string `json:"contexts"`
		}{selection.Name, selection.Source, names}); err != nil {
			return true, err
		}
	case "use":
		if len(commandArgs) == 2 && isHelp(commandArgs[1]) {
			fmt.Fprintf(stdout, "Usage: %s context use NAME\n", manager.Service)
			return true, nil
		}
		if len(commandArgs) != 2 {
			return true, fmt.Errorf("usage: %s context use NAME", manager.Service)
		}
		if err := manager.Use(commandArgs[1]); err != nil {
			return true, err
		}
		fmt.Fprintf(stdout, "Switched to context %q.\n", commandArgs[1])
		if strings.TrimSpace(os.Getenv(manager.EnvVar)) != "" {
			fmt.Fprintf(stderr, "warning: %s is set; the environment context remains active\n", manager.EnvVar)
		}
	default:
		return true, fmt.Errorf("unknown context command %q\n\n%s", commandArgs[0], helpText(manager.Service))
	}
	return true, nil
}

func (m Manager) selection(name, source string) Selection {
	configRoot := m.configRoot()
	cacheRoot := m.cacheRoot()
	if name == DefaultName {
		return Selection{Name: name, Source: source, ConfigDir: configRoot, CacheDir: cacheRoot}
	}
	configDir, cacheDir := "", ""
	if configRoot != "" {
		configDir = filepath.Join(configRoot, "contexts", name)
	}
	if cacheRoot != "" {
		cacheDir = filepath.Join(cacheRoot, "contexts", name)
	}
	return Selection{
		Name:      name,
		Source:    source,
		ConfigDir: configDir,
		CacheDir:  cacheDir,
	}
}

func (m Manager) configRoot() string {
	return serviceRoot("XDG_CONFIG_HOME", ".config", m.Service)
}

func (m Manager) cacheRoot() string {
	return serviceRoot("XDG_CACHE_HOME", ".cache", m.Service)
}

func (m Manager) readCurrent() (string, bool, error) {
	root := m.configRoot()
	if root == "" {
		return "", false, nil
	}
	if err := validateProjectRoot(root); err != nil {
		return "", false, err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("stat %s context directory: %w", m.Service, err)
	}
	if err := validateSecureDir(root, rootInfo); err != nil {
		return "", false, err
	}
	path := filepath.Join(root, "current-context")
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("stat %s current context: %w", m.Service, err)
	}
	if err := validateSecureFile(path, info); err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read %s current context: %w", m.Service, err)
	}
	name := strings.TrimSpace(string(data))
	if err := ValidateName(name); err != nil {
		return "", false, fmt.Errorf("invalid %s current context: %w", m.Service, err)
	}
	return name, true, nil
}

func extractContextFlag(args []string) (string, []string, bool, error) {
	filtered := append([]string(nil), args...)
	if len(args) < 2 {
		return "", filtered, false, nil
	}
	var name string
	consumed := 0
	switch {
	case args[1] == "--context":
		if len(args) < 3 {
			return "", nil, false, fmt.Errorf("--context requires a value")
		}
		name, consumed = args[2], 2
	case strings.HasPrefix(args[1], "--context="):
		name, consumed = strings.TrimPrefix(args[1], "--context="), 1
	}
	start := 1
	if consumed != 0 {
		start += consumed
		filtered = append([]string{args[0]}, args[start:]...)
	}
	for _, arg := range args[start:] {
		if arg == "--" {
			break
		}
		if arg == "--context" || strings.HasPrefix(arg, "--context=") {
			if consumed != 0 {
				return "", nil, false, fmt.Errorf("--context may only be specified once")
			}
			return "", nil, false, fmt.Errorf("--context must appear before other command arguments")
		}
	}
	return strings.TrimSpace(name), filtered, consumed != 0, nil
}

func serviceRoot(envName, fallback, service string) string {
	base := strings.TrimSpace(os.Getenv(envName))
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		base = filepath.Join(home, fallback)
	}
	return filepath.Join(base, "aicli", service)
}

func writeCurrentAtomic(path, name string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".current-context.*.tmp")
	if err != nil {
		return fmt.Errorf("create current context temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := fmt.Fprintln(tmp, name); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write current context: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync current context: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close current context: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace current context: %w", err)
	}
	return nil
}

func sortedNames(seen map[string]bool) []string {
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func localCommandArgs(args []string, name string) ([]string, bool) {
	if len(args) < 2 {
		return nil, false
	}
	if args[1] == "help" && len(args) >= 3 && args[2] == name {
		if len(args) >= 4 {
			return []string{args[3], "--help"}, true
		}
		return []string{"--help"}, true
	}
	for i := 1; i < len(args); i++ {
		if args[i] == name {
			return args[i+1:], true
		}
		if args[i] == "-S" || args[i] == "--rsh-silent" || strings.HasPrefix(args[i], "--rsh-silent=") ||
			args[i] == "--rsh-verbose" || strings.HasPrefix(args[i], "--rsh-verbose=") || allVerbose(args[i]) {
			continue
		}
		return nil, false
	}
	return nil, false
}

func allVerbose(arg string) bool {
	if len(arg) < 2 || arg[0] != '-' {
		return false
	}
	for _, ch := range arg[1:] {
		if ch != 'v' {
			return false
		}
	}
	return true
}

func isHelp(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func helpText(command string) string {
	return fmt.Sprintf(`Usage:
  %s context current
  %s context list
  %s context use NAME

Select an isolated account context. Use --context NAME before a command for a one-off override.
`, command, command, command)
}

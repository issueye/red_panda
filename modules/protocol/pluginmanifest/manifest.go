package pluginmanifest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"redpanda/protocol/pluginmeta"
)

const Filename = "redpanda-plugin.json"

const maxManifestBytes = 1 << 20

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)

type Manifest struct {
	ID             string       `json:"id"`
	Version        string       `json:"version"`
	Description    string       `json:"description,omitempty"`
	Type           string       `json:"type"`
	Entry          Entry        `json:"entry"`
	Capabilities   Capabilities `json:"capabilities"`
	DefaultEnabled bool         `json:"default_enabled"`
	RiskDefault    string       `json:"risk_default,omitempty"`
}

type Entry struct {
	Command    string   `json:"command,omitempty"`
	CommandEnv string   `json:"command_env,omitempty"`
	Args       []string `json:"args,omitempty"`
	Script     string   `json:"script,omitempty"`
}

type Capabilities struct {
	Tools    []string `json:"tools,omitempty"`
	Hooks    []string `json:"hooks,omitempty"`
	Commands []string `json:"commands,omitempty"`
}

type LocatedManifest struct {
	Path     string
	Manifest Manifest
}

type Diagnostic struct {
	Path  string
	Error string
}

func Load(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil {
		return Manifest{}, err
	} else if info.Size() > maxManifestBytes {
		return Manifest{}, fmt.Errorf("plugin manifest exceeds %d bytes", maxManifestBytes)
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxManifestBytes+1))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode plugin manifest: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	if err := pluginmeta.ValidateSource(m.Type + ":" + m.ID); err != nil {
		return fmt.Errorf("manifest id/type: %w", err)
	}
	if m.Type != "mcp" && m.Type != "js" {
		return fmt.Errorf("manifest type %q is not supported", m.Type)
	}
	if !versionPattern.MatchString(m.Version) {
		return fmt.Errorf("manifest version %q is not semantic versioning", m.Version)
	}
	if err := m.validateEntry(); err != nil {
		return err
	}
	if err := validateUniqueNames("tool", m.Capabilities.Tools, pluginmeta.ValidateToolName); err != nil {
		return err
	}
	if err := validateUniqueNames("hook", m.Capabilities.Hooks, pluginmeta.ValidateHookName); err != nil {
		return err
	}
	if err := validateUniqueNames("command", m.Capabilities.Commands, pluginmeta.ValidateMethod); err != nil {
		return err
	}
	switch m.RiskDefault {
	case "", "low", "medium", "high":
	default:
		return fmt.Errorf("manifest risk_default %q is invalid", m.RiskDefault)
	}
	return nil
}

func (m Manifest) validateEntry() error {
	command := strings.TrimSpace(m.Entry.Command)
	commandEnv := strings.TrimSpace(m.Entry.CommandEnv)
	script := strings.TrimSpace(m.Entry.Script)
	switch m.Type {
	case "mcp":
		if script != "" || (command == "") == (commandEnv == "") {
			return fmt.Errorf("mcp manifest entry requires exactly one of command or command_env")
		}
		if commandEnv != "" && !validEnvironmentName(commandEnv) {
			return fmt.Errorf("manifest command_env %q is invalid", commandEnv)
		}
	case "js":
		if script == "" || command != "" || commandEnv != "" || len(m.Entry.Args) != 0 {
			return fmt.Errorf("js manifest entry requires only script")
		}
		if filepath.IsAbs(script) || filepath.Clean(script) != script || strings.HasPrefix(script, "..") {
			return fmt.Errorf("manifest script %q must be a normalized relative path", script)
		}
	}
	return nil
}

func validateUniqueNames(kind string, names []string, validate func(string) error) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, exists := seen[name]; exists {
			return fmt.Errorf("manifest %s %q is duplicated", kind, name)
		}
		seen[name] = struct{}{}
		if err := validate(name); err != nil {
			return fmt.Errorf("manifest %s: %w", kind, err)
		}
	}
	return nil
}

func Discover(root string) ([]LocatedManifest, []Diagnostic) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, []Diagnostic{{Path: root, Error: err.Error()}}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var manifests []LocatedManifest
	var diagnostics []Diagnostic
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), Filename)
		if _, err := os.Stat(path); err != nil {
			if !os.IsNotExist(err) {
				diagnostics = append(diagnostics, Diagnostic{Path: path, Error: err.Error()})
			}
			continue
		}
		manifest, err := Load(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Error: err.Error()})
			continue
		}
		manifests = append(manifests, LocatedManifest{Path: path, Manifest: manifest})
	}
	return manifests, diagnostics
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode plugin manifest trailing data: %w", err)
	}
	return fmt.Errorf("plugin manifest contains multiple JSON values")
}

func validEnvironmentName(value string) bool {
	for index, r := range value {
		if (r >= 'A' && r <= 'Z') || r == '_' || (index > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return value != ""
}

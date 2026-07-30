package pluginmanifest

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestManifestValidate(t *testing.T) {
	valid := Manifest{
		ID: "fetch", Version: "1.0.0", Type: "mcp",
		Entry:        Entry{Command: "uvx", Args: []string{"mcp-server-fetch"}},
		Capabilities: Capabilities{Tools: []string{"fetch"}},
		RiskDefault:  "medium",
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Entry.CommandEnv = "SECRET_COMMAND"
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected mutually exclusive entry error")
	}
	invalid = valid
	invalid.ID = "has space"
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected invalid id error")
	}
}

func TestJSManifestValidate(t *testing.T) {
	valid := Manifest{
		ID: "greeter", Version: "1.0.0", Type: "js",
		Entry:        Entry{Script: "main.js"},
		Capabilities: Capabilities{Tools: []string{"greeter.hello"}, Hooks: []string{"context.compose"}},
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Entry.Command = "node"
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected mixed JS entry error")
	}
	invalid = valid
	invalid.Entry.Script = "../main.js"
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected escaping JS entry error")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), Filename)
	raw := `{"id":"test","version":"1.0.0","type":"mcp","entry":{"command":"test"},"capabilities":{},"permissions":["filesystem"]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown permissions field error")
	}
}

func TestManifestRejectsDuplicateCapabilities(t *testing.T) {
	manifest := Manifest{
		ID: "test", Version: "1.0.0", Type: "mcp", Entry: Entry{Command: "test"},
		Capabilities: Capabilities{Tools: []string{"read", "read"}},
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected duplicate tool error")
	}
}

func TestLoadRejectsOversizedManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), Filename)
	if err := os.WriteFile(path, make([]byte, maxManifestBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected manifest size error")
	}
}

func TestRepositoryMCPManifests(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "mcps"))
	manifests, diagnostics := Discover(root)
	if len(diagnostics) != 0 {
		t.Fatalf("manifest diagnostics = %#v", diagnostics)
	}
	ids := make([]string, 0, len(manifests))
	for _, manifest := range manifests {
		ids = append(ids, manifest.Manifest.ID)
	}
	want := []string{"fetch", "robotgo-flow", "sequential-thinking", "tasks"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("manifest ids = %#v, want %#v", ids, want)
	}
}

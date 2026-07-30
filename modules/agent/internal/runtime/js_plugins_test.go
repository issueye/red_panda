package runtime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeLoadsGlobalJSPluginsAndIsolatesFailures(t *testing.T) {
	home := t.TempDir()
	t.Setenv("RED_PANDA_HOME", home)
	plugins := filepath.Join(home, "plugins")
	if err := os.MkdirAll(plugins, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugins, "good.js"), []byte(`rp.registerTool({name:"demo.runtime", risk:"low"}, () => "loaded");`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugins, "bad.js"), []byte(`throw new Error("broken");`), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	rt := New(bytes.NewReader(nil), &bytes.Buffer{}, &logs, "test")
	defer rt.workerPool.Close(context.Background())
	if len(rt.jsPlugins) != 1 || rt.jsPlugins[0].ID != "good" {
		t.Fatalf("plugins = %#v", rt.jsPlugins)
	}
	if len(rt.jsDiagnostics) != 1 || rt.jsDiagnostics[0].Path != filepath.Join(plugins, "bad.js") {
		t.Fatalf("diagnostics = %#v", rt.jsDiagnostics)
	}
	if _, ok := rt.registry.Lookup("demo.runtime"); !ok {
		t.Fatal("global javascript tool was not registered")
	}
}

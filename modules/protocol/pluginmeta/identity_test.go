package pluginmeta

import (
	"errors"
	"testing"
)

func TestValidateIdentifiers(t *testing.T) {
	for _, value := range []string{"workspace.read_file", "run.execute", "initialize", "worker-send.v2"} {
		if err := ValidateToolName(value); err != nil {
			t.Errorf("ValidateToolName(%q): %v", value, err)
		}
	}
	for _, source := range []string{"builtin:workspace", "js:my-plugin", "mcp:fetch.v2"} {
		if err := ValidateSource(source); err != nil {
			t.Errorf("ValidateSource(%q): %v", source, err)
		}
	}
}

func TestValidateIdentifiersRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "has space", ".leading"} {
		err := ValidateMethod(value)
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Errorf("ValidateMethod(%q) error = %v", value, err)
		}
	}
	for _, source := range []string{"", "plugin:test", "js:", "js:has space"} {
		err := ValidateSource(source)
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Errorf("ValidateSource(%q) error = %v", source, err)
		}
	}
}

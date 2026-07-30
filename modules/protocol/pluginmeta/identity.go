package pluginmeta

import (
	"fmt"
	"regexp"
)

var (
	qualifiedNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)
	sourcePattern        = regexp.MustCompile(`^(builtin|js|mcp):[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

type ValidationError struct {
	Kind  string
	Value string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid plugin %s %q", e.Kind, e.Value)
}

func ValidateToolName(name string) error {
	return validateQualified("tool name", name)
}

func ValidateProviderName(name string) error {
	return validateQualified("provider name", name)
}

func ValidateMethod(method string) error {
	return validateQualified("method", method)
}

func ValidateHookName(name string) error {
	return validateQualified("hook name", name)
}

func ValidateSource(source string) error {
	if !sourcePattern.MatchString(source) {
		return &ValidationError{Kind: "source", Value: source}
	}
	return nil
}

func validateQualified(kind, value string) error {
	if !qualifiedNamePattern.MatchString(value) {
		return &ValidationError{Kind: kind, Value: value}
	}
	return nil
}

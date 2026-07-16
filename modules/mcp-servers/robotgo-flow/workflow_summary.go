package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Lightweight structural summary without pulling a YAML library (keeps the MCP
// binary dependency-free). Good enough for agent planning; robotgo-flow does
// authoritative validation on run.

var (
	reNameLine  = regexp.MustCompile(`(?m)^name:\s*["']?([^"'\n#]+)["']?`)
	reDescLine  = regexp.MustCompile(`(?m)^description:\s*["']?([^"'\n#]+)["']?`)
	reStepName  = regexp.MustCompile(`(?m)^\s+-\s+name:\s*["']?([^"'\n#]+)["']?`)
	reInputName = regexp.MustCompile(`(?m)^\s+-\s+name:\s*([a-zA-Z0-9_]+)`)
	reRequired  = regexp.MustCompile(`(?m)^\s+required:\s*(true|false)`)
	reActions   = regexp.MustCompile(`(?m)^\s+-\s+(click|double_click|right_click|drag|type|press|combo|wait|wait_gone|scroll|open_url|refresh|back|forward|switch_tab|sleep|prompt|confirm|notify):`)
)

func summarizeWorkflowYAML(raw []byte) (map[string]any, error) {
	text := string(raw)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("empty file")
	}
	name := firstSub(reNameLine, text)
	desc := firstSub(reDescLine, text)

	// Scope name matches to their sections so inputs[] names are not counted as steps.
	stepsSection := extractSection(text, "steps:")
	inputsSection := extractSection(text, "inputs:")

	steps := reStepName.FindAllStringSubmatch(stepsSection, -1)
	stepNames := make([]string, 0, len(steps))
	for _, m := range steps {
		if len(m) > 1 {
			stepNames = append(stepNames, strings.TrimSpace(m[1]))
		}
	}

	inputNames := reInputName.FindAllStringSubmatch(inputsSection, -1)
	// Pair each input block's required flag by scanning input entries.
	inputs := make([]map[string]any, 0, len(inputNames))
	required := make([]string, 0)
	for _, block := range splitYAMLListItems(inputsSection) {
		n := firstSub(reInputName, block)
		if n == "" {
			continue
		}
		req := strings.Contains(block, "required: true")
		inputs = append(inputs, map[string]any{"name": n, "required": req})
		if req {
			required = append(required, n)
		}
	}

	actionMatches := reActions.FindAllStringSubmatch(stepsSection, -1)
	actionCounts := map[string]int{}
	for _, m := range actionMatches {
		if len(m) > 1 {
			actionCounts[m[1]]++
		}
	}
	if name == "" && len(stepNames) == 0 {
		return nil, fmt.Errorf("no workflow name or steps detected; is this robotgo-flow YAML?")
	}
	return map[string]any{
		"name":            name,
		"description":     desc,
		"step_count":      len(stepNames),
		"steps":           stepNames,
		"inputs":          inputs,
		"required_inputs": required,
		"action_counts":   actionCounts,
	}, nil
}

// splitYAMLListItems splits a section body into top-level list item chunks starting with "  - ".
func splitYAMLListItems(section string) []string {
	lines := strings.Split(section, "\n")
	var items []string
	var cur strings.Builder
	flush := func() {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			items = append(items, cur.String())
		}
		cur.Reset()
	}
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "- ") {
			flush()
			cur.WriteString(line)
			cur.WriteByte('\n')
			continue
		}
		if cur.Len() > 0 {
			cur.WriteString(line)
			cur.WriteByte('\n')
		}
	}
	flush()
	return items
}

func firstSub(re *regexp.Regexp, text string) string {
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func extractSection(text, header string) string {
	idx := strings.Index(text, header)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(header):]
	// Stop at next top-level key (line starting without indent and ending with :)
	lines := strings.Split(rest, "\n")
	var b strings.Builder
	for _, line := range lines {
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && strings.Contains(line, ":") {
			// next top-level key
			if !strings.HasPrefix(strings.TrimSpace(line), "-") {
				break
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

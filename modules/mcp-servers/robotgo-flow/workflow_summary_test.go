package main

import "testing"

func TestSummarizeWorkflowYAML(t *testing.T) {
	raw := []byte(`
name: "demo login"
description: "example"
inputs:
  - name: username
    label: "用户名"
    required: true
  - name: password
    label: "密码"
    required: true
    mask: true
settings:
  element_timeout: 10
steps:
  - name: "open"
    actions:
      - open_url: "https://example.com"
  - name: "login"
    actions:
      - wait: "templates/btn.png"
      - click: "templates/btn.png"
      - type:
          into: "templates/user.png"
          text: "$input.username"
`)
	sum, err := summarizeWorkflowYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if sum["name"] != "demo login" {
		t.Fatalf("name=%v", sum["name"])
	}
	if sum["step_count"].(int) != 2 {
		t.Fatalf("step_count=%v", sum["step_count"])
	}
	req := sum["required_inputs"].([]string)
	if len(req) != 2 {
		t.Fatalf("required=%v", req)
	}
	counts := sum["action_counts"].(map[string]int)
	if counts["open_url"] != 1 || counts["click"] != 1 || counts["type"] != 1 {
		t.Fatalf("action_counts=%v", counts)
	}
}

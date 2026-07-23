package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"redpanda/protocol/tools"
)

const (
	defaultListDepth   = 3
	defaultGrepMatches = 50
)

type EchoProvider struct {
	providerStream   bool
	streamConfigured bool
}

func newEchoProvider(stream bool) EchoProvider {
	return EchoProvider{providerStream: stream, streamConfigured: true}
}

func (p EchoProvider) streamByDefault() bool {
	if p.streamConfigured {
		return p.providerStream
	}
	return true
}

func (EchoProvider) Name() string {
	return "echo"
}

func (p EchoProvider) Complete(ctx context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	if override, ok := Resolve(req.Options, p.streamByDefault()); ok {
		return completeResolvedProvider(ctx, override, req, emit)
	}
	if len(req.ToolHistory) > 0 {
		last := req.ToolHistory[len(req.ToolHistory)-1]
		text := fmt.Sprintf("Tool %s completed with status %s.\n%s", last.Result.Name, last.Result.Status, last.Result.Output)
		if last.Result.Error != "" {
			text = fmt.Sprintf("Tool %s failed: %s", last.Result.Name, last.Result.Error)
		}
		if err := emit(ProviderChunk{Delta: text}); err != nil {
			return err
		}
		time.Sleep(30 * time.Millisecond)
		return emit(ProviderChunk{Final: true})
	}
	if calls := echoToolCalls(req); len(calls) > 0 {
		return emit(ProviderChunk{ToolCalls: calls})
	}
	text := req.Input
	if text == "" {
		text = "ok"
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := emit(ProviderChunk{Delta: text}); err != nil {
		return err
	}
	time.Sleep(30 * time.Millisecond)
	return emit(ProviderChunk{Final: true})
}

func echoToolCalls(req ProviderRequest) []tools.Call {
	text := strings.TrimSpace(req.Input)
	lower := strings.ToLower(text)
	switch {
	case strings.HasPrefix(lower, "read file "):
		path := strings.TrimSpace(text[len("read file "):])
		return []tools.Call{{
			ID:          "tool_" + req.RunID + "_model_read",
			Name:        "workspace.read_file",
			DisplayName: "Read file",
			Risk:        tools.RiskLow,
			Arguments:   map[string]any{"path": path},
		}}
	case lower == "list files" || strings.HasPrefix(lower, "list files "):
		path := strings.TrimSpace(text[len("list files"):])
		if path == "" {
			path = "."
		}
		return []tools.Call{{
			ID:          "tool_" + req.RunID + "_model_list",
			Name:        "workspace.list",
			DisplayName: "List files",
			Risk:        tools.RiskLow,
			Arguments:   map[string]any{"path": path, "max_depth": defaultListDepth},
		}}
	case strings.HasPrefix(lower, "grep "):
		return []tools.Call{grepModelCall(req.RunID, strings.TrimSpace(text[len("grep "):]))}
	case strings.HasPrefix(lower, "search "):
		return []tools.Call{grepModelCall(req.RunID, strings.TrimSpace(text[len("search "):]))}
	case strings.HasPrefix(lower, "run shell "):
		command := strings.TrimSpace(text[len("run shell "):])
		return []tools.Call{{
			ID:          "tool_" + req.RunID + "_model_shell",
			Name:        "shell.exec",
			DisplayName: "Shell",
			Risk:        tools.RiskHigh,
			Arguments:   map[string]any{"command": command},
		}}
	case strings.HasPrefix(lower, "write file "):
		rest := strings.TrimSpace(text[len("write file "):])
		path, content, ok := strings.Cut(rest, " ")
		if !ok {
			path = rest
			content = ""
		}
		return []tools.Call{{
			ID:          "tool_" + req.RunID + "_model_write",
			Name:        "workspace.write_file",
			DisplayName: "Write file",
			Risk:        tools.RiskHigh,
			Arguments:   map[string]any{"path": path, "content": content},
		}}
	case strings.HasPrefix(lower, "edit file "):
		rest := strings.TrimSpace(text[len("edit file "):])
		return []tools.Call{editModelCall(req.RunID, rest)}
	case strings.HasPrefix(lower, "diff file "):
		rest := strings.TrimSpace(text[len("diff file "):])
		return []tools.Call{diffModelCall(req.RunID, rest)}
	case strings.HasPrefix(lower, "apply patch "):
		patch := strings.TrimSpace(text[len("apply patch "):])
		return []tools.Call{{
			ID:          "tool_" + req.RunID + "_model_patch",
			Name:        "workspace.apply_patch",
			DisplayName: "Apply patch",
			Risk:        tools.RiskHigh,
			Arguments:   map[string]any{"patch": patch},
		}}
	case lower == "list memory" || strings.HasPrefix(lower, "list memory "):
		scope := strings.TrimSpace(text[len("list memory"):])
		args := map[string]any{"limit": 20}
		if scope != "" {
			args["scope"] = scope
		}
		return []tools.Call{{
			ID:          "tool_" + req.RunID + "_model_memory_list",
			Name:        "memory.list",
			DisplayName: "List memory",
			Risk:        tools.RiskMedium,
			Arguments:   args,
		}}
	case strings.HasPrefix(lower, "remember session "):
		content := strings.TrimSpace(text[len("remember session "):])
		return []tools.Call{memoryCreateModelCall(req.RunID, "session", content)}
	case strings.HasPrefix(lower, "remember project "):
		content := strings.TrimSpace(text[len("remember project "):])
		return []tools.Call{memoryCreateModelCall(req.RunID, "project", content)}
	case strings.HasPrefix(lower, "delete memory "):
		id := strings.TrimSpace(text[len("delete memory "):])
		return []tools.Call{{
			ID:          "tool_" + req.RunID + "_model_memory_delete",
			Name:        "memory.delete",
			DisplayName: "Delete memory",
			Risk:        tools.RiskHigh,
			Arguments:   map[string]any{"id": id},
		}}
	case lower == "list todos" || lower == "list todo" || lower == "/todo list":
		return []tools.Call{{
			ID:          "tool_" + req.RunID + "_model_todo_list",
			Name:        "todo.list",
			DisplayName: "List todos",
			Risk:        tools.RiskLow,
			Arguments:   map[string]any{"status": "all"},
		}}
	case strings.HasPrefix(lower, "todo ") || lower == "/todo":
		// 简单冒烟测试辅助："todo plan a; plan b"。
		rest := strings.TrimSpace(text)
		if strings.HasPrefix(lower, "todo ") {
			rest = strings.TrimSpace(text[len("todo "):])
		} else if lower == "/todo" {
			rest = "task"
		}
		parts := strings.Split(rest, ";")
		todos := make([]any, 0, len(parts))
		for i, part := range parts {
			content := strings.TrimSpace(part)
			if content == "" {
				continue
			}
			status := "pending"
			if i == 0 {
				status = "in_progress"
			}
			todos = append(todos, map[string]any{
				"id":      fmt.Sprintf("%d", i+1),
				"content": content,
				"status":  status,
			})
		}
		if len(todos) == 0 {
			return nil
		}
		return []tools.Call{{
			ID:          "tool_" + req.RunID + "_model_todo_write",
			Name:        "todo.write",
			DisplayName: "Update todos",
			Risk:        tools.RiskLow,
			Arguments:   map[string]any{"todos": todos, "merge": false},
		}}
	default:
		return nil
	}
}

func memoryCreateModelCall(runID string, scope string, content string) tools.Call {
	return tools.Call{
		ID:          "tool_" + runID + "_model_memory_create",
		Name:        "memory.create",
		DisplayName: "Create memory",
		Risk:        tools.RiskHigh,
		Arguments: map[string]any{
			"scope":   scope,
			"content": content,
		},
	}
}

func grepModelCall(runID string, rest string) tools.Call {
	pattern, path, ok := strings.Cut(rest, " ")
	if !ok {
		path = "."
	}
	return tools.Call{
		ID:          "tool_" + runID + "_model_grep",
		Name:        "workspace.grep",
		DisplayName: "Search files",
		Risk:        tools.RiskLow,
		Arguments:   map[string]any{"pattern": pattern, "path": strings.TrimSpace(path), "max_matches": defaultGrepMatches},
	}
}

func editModelCall(runID string, rest string) tools.Call {
	path, replacement, ok := strings.Cut(rest, " ")
	oldText, newText := "", ""
	if ok {
		oldText, newText, _ = strings.Cut(replacement, "=>")
	}
	return tools.Call{
		ID:          "tool_" + runID + "_model_edit",
		Name:        "workspace.edit_file",
		DisplayName: "Edit file",
		Risk:        tools.RiskHigh,
		Arguments: map[string]any{
			"path":        strings.TrimSpace(path),
			"old_text":    strings.TrimSpace(oldText),
			"new_text":    strings.TrimSpace(newText),
			"replace_all": false,
		},
	}
}

func diffModelCall(runID string, rest string) tools.Call {
	path, replacement, ok := strings.Cut(rest, " ")
	oldText, newText := "", ""
	if ok {
		oldText, newText, _ = strings.Cut(replacement, "=>")
	}
	return tools.Call{
		ID:          "tool_" + runID + "_model_diff",
		Name:        "workspace.diff_file",
		DisplayName: "Preview diff",
		Risk:        tools.RiskLow,
		Arguments: map[string]any{
			"path":        strings.TrimSpace(path),
			"old_text":    strings.TrimSpace(oldText),
			"new_text":    strings.TrimSpace(newText),
			"replace_all": false,
		},
	}
}

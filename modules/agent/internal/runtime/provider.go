package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type Provider interface {
	Name() string
	Complete(ctx context.Context, req ProviderRequest, emit func(ProviderChunk) error) error
}

type ProviderRequest struct {
	RunID   string
	Session methods.ReplySession
	Input   methods.ReplyInput
	Options methods.ReplyOptions
	Tools   []tools.Definition
	// ToolHistory is a flat list (used by EchoProvider and tests).
	ToolHistory []ToolExchange
	// ToolRounds groups tools that ran in the same model turn (OpenAI multi-tool format).
	// When empty, each ToolHistory item is treated as its own round.
	ToolRounds [][]ToolExchange
}

type ToolExchange struct {
	Call   tools.Call
	Result tools.Result
}

type ProviderChunk struct {
	Delta     string
	Final     bool
	ToolCalls []tools.Call
}

func newProviderFromEnv(log io.Writer) Provider {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER")))
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_BASE_URL")), "/")
	if provider == "openai_compatible" || provider == "http_compatible" || baseURL != "" {
		model := strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_MODEL"))
		apiKey := strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_API_KEY"))
		if model == "" {
			model = "default"
		}
		if baseURL == "" {
			fmt.Fprintln(log, "provider base url is empty; falling back to echo provider")
			return EchoProvider{}
		}
		return HTTPCompatibleProvider{
			baseURL: baseURL,
			apiKey:  apiKey,
			model:   model,
			stream:  boolEnv("RED_PANDA_PROVIDER_STREAM"),
			client:  &http.Client{Timeout: 90 * time.Second},
		}
	}
	return EchoProvider{}
}

type EchoProvider struct{}

func (EchoProvider) Name() string {
	return "echo"
}

func (EchoProvider) Complete(ctx context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	if override, ok := providerFromOptions(req.Options, false); ok {
		return override.complete(ctx, req, emit)
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
	text := req.Input.Text
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
	text := strings.TrimSpace(req.Input.Text)
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
		// Simple smoke helper: "todo plan a; plan b"
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

type HTTPCompatibleProvider struct {
	baseURL string
	apiKey  string
	model   string
	stream  bool
	client  *http.Client
}

func (p HTTPCompatibleProvider) Name() string {
	return "openai_compatible"
}

func (p HTTPCompatibleProvider) Complete(ctx context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	if override, ok := providerFromOptions(req.Options, p.stream); ok {
		return override.complete(ctx, req, emit)
	}
	return p.complete(ctx, req, emit)
}

func (p HTTPCompatibleProvider) complete(ctx context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	model := req.Options.Model
	if model == "" {
		model = p.model
	}
	body := map[string]any{
		"model":    model,
		"messages": openAICompatibleMessages(req),
		"stream":   p.stream,
	}
	if len(req.Tools) > 0 {
		body["tools"] = openAICompatibleTools(req.Tools)
		body["tool_choice"] = "auto"
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := openAICompatibleChatCompletionsURL(p.baseURL)
	if path, logErr := logLLMRequest(req.Options.LogLLMRequests, req.RunID, req.Session.ID, model, endpoint, rawBody); logErr != nil {
		// Never fail the user-facing request because diagnostics failed.
		fmt.Fprintf(os.Stderr, "red-panda-agent: llm request log failed: %v\n", logErr)
	} else if path != "" {
		fmt.Fprintf(os.Stderr, "red-panda-agent: llm request logged to %s\n", path)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		rawResp, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return fmt.Errorf("provider returned HTTP %d: %s", resp.StatusCode, string(rawResp))
	}
	if p.stream {
		return p.completeStream(resp.Body, emit)
	}
	rawResp, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return completeHTTPResponse(rawResp, emit)
}

func openAICompatibleChatCompletionsURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/chat/completions"
	}
	return baseURL + "/v1/chat/completions"
}

func providerFromOptions(options methods.ReplyOptions, fallbackStream bool) (HTTPCompatibleProvider, bool) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.ProviderBaseURL), "/")
	if baseURL == "" {
		return HTTPCompatibleProvider{}, false
	}
	provider := strings.ToLower(strings.TrimSpace(options.ProviderName))
	if provider == "" {
		provider = "openai_compatible"
	}
	if provider != "openai_compatible" && provider != "http_compatible" {
		return HTTPCompatibleProvider{}, false
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = "default"
	}
	return HTTPCompatibleProvider{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(options.ProviderAPIKey),
		model:   model,
		stream:  fallbackStream,
		client:  &http.Client{Timeout: 90 * time.Second},
	}, true
}

func completeHTTPResponse(rawResp []byte, emit func(ProviderChunk) error) error {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rawResp, &parsed); err != nil {
		return err
	}
	if len(parsed.Choices) == 0 {
		return fmt.Errorf("provider returned no choices")
	}
	message := parsed.Choices[0].Message
	if len(message.ToolCalls) > 0 {
		calls := make([]tools.Call, 0, len(message.ToolCalls))
		for _, toolCall := range message.ToolCalls {
			var args map[string]any
			if toolCall.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(toolCall.Function.Arguments), &args)
			}
			calls = append(calls, tools.Call{
				ID:        toolCall.ID,
				Name:      internalToolName(toolCall.Function.Name),
				Arguments: args,
			})
		}
		return emit(ProviderChunk{ToolCalls: calls})
	}
	if err := emit(ProviderChunk{Delta: message.Content}); err != nil {
		return err
	}
	return emit(ProviderChunk{Final: true})
}

type streamToolCall struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

func (p HTTPCompatibleProvider) completeStream(reader io.Reader, emit func(ProviderChunk) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	toolCalls := map[int]*streamToolCall{}
	var emittedContent bool
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return err
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				emittedContent = true
				if err := emit(ProviderChunk{Delta: choice.Delta.Content}); err != nil {
					return err
				}
			}
			for _, deltaCall := range choice.Delta.ToolCalls {
				call := toolCalls[deltaCall.Index]
				if call == nil {
					call = &streamToolCall{}
					toolCalls[deltaCall.Index] = call
				}
				if deltaCall.ID != "" {
					call.ID = deltaCall.ID
				}
				if deltaCall.Function.Name != "" {
					call.Name = internalToolName(deltaCall.Function.Name)
				}
				if deltaCall.Function.Arguments != "" {
					call.Arguments.WriteString(deltaCall.Function.Arguments)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(toolCalls) > 0 {
		calls := make([]tools.Call, 0, len(toolCalls))
		for index := 0; index < len(toolCalls); index++ {
			call := toolCalls[index]
			if call == nil || call.Name == "" {
				continue
			}
			var args map[string]any
			if text := call.Arguments.String(); text != "" {
				_ = json.Unmarshal([]byte(text), &args)
			}
			calls = append(calls, tools.Call{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: args,
			})
		}
		if len(calls) > 0 {
			return emit(ProviderChunk{ToolCalls: calls})
		}
	}
	if emittedContent {
		return emit(ProviderChunk{Final: true})
	}
	return emit(ProviderChunk{Final: true})
}

// rootAgentOrchestrationPolicy is injected only for root runs that can call subagent tools.
// Child specialists intentionally do not receive this (they cannot nest subagents).
const rootAgentOrchestrationPolicy = `You are the red_panda root orchestrator.

Survey-then-split policy (mandatory for analysis when tools include subagent.run):
1. Before multi-file / multi-module analysis, call workspace.stats on the target roots to measure structure and file counts.
2. Do NOT use a fixed small max_turns budget. After stats, set each specialist budget as:
   max_turns = file_count + summary_turns
   Use suggested_max_turns for a whole scope, or top_level[].recommended_max_turns / top_level[].files for each split.
   Pass path and file_count into subagent.run (or max_turns = that formula). There is no artificial maximum.
3. Use suggested_splits / top_level:
   - small_tree: root or one subagent.run
   - medium/large: split by major directories and spawn multiple subagent.run calls IN ONE TURN (parallel process pool). Resize pool first if needed.
4. Make coverage explicit and non-overlapping before dispatch:
   - Assign root-level entrypoints, build manifests, dependency files, and project configuration (for example main.go, go.mod, package.json, wails.json) to one foundation specialist, or include them explicitly in one module specialist's task.
   - Give every specialist a path boundary, concrete questions, and an expected evidence-based final report.
   - Do not leave shared/root files unowned and then read them serially on the root agent after specialists finish.
5. Never dump a large tree analysis onto yourself with serial greps or workspace.read_file calls when specialists are available. The root agent coordinates, resolves conflicts, and synthesizes.
6. After specialists finish, treat successful reports as the evidence for their assigned scopes. Do not re-read files already covered merely to reconstruct their work. If a report has a specific missing fact, launch one narrow follow-up subagent for that gap; do not restart a broad scan.
7. Manage workers with subagent.list / cancel / reset / pool_status / pool_resize / pool_reset. A failed or unusable specialist must be reset/reassigned or explicitly reported; it must never block unrelated completed work.
8. After all required coverage is complete, synthesize the reports into the user-facing answer. Never claim subagents are unavailable when subagent.run is in your tool list.
9. Trivial single-file Q&A or tiny edits may stay on the root agent without subagents.
10. For multi-step work, maintain a session checklist with todo.write (see the dedicated todo policy when available). Do not track plans only in free-text.`

// rootAgentPostDelegationPolicy is added once a specialist has returned a usable
// result. It keeps the next model turn focused on synthesis instead of silently
// repeating the specialist's file reads on the root agent.
const rootAgentPostDelegationPolicy = `Delegation results are now available.

Post-delegation rule:
1. Use successful subagent.run reports as the authoritative evidence for their assigned scopes.
2. Synthesize completed reports before requesting any more workspace tools.
3. Do not call workspace.read_file, workspace.list, or broad search tools just to repeat or verify work already covered by a successful specialist.
4. If an exact fact is missing, identify that gap and issue one narrowly scoped follow-up subagent.run. Direct root-agent file reads are reserved for genuinely unassigned trivial scope or an explicit user request.
5. Failed specialists do not invalidate successful reports from other specialists; reassign only the failed/missing scope and continue.`

// rootAgentGoalPipelinePolicy guides long-horizon Goal work (docs/32 §2 / §2.10).
const rootAgentGoalPipelinePolicy = `Goal pipeline (goal.* + todo.*) — mandatory for multi-step user goals.

For multi-step work you MUST use phase specialists via subagent.run with these exact names
(unless the request is trivial one-shot Q&A with no Goal):

| Phase | subagent.run name | Who writes goal/todo |
| analyze | goal-analyst | root only |
| plan | goal-planner (draft) then root goal.update + todo.write | root only |
| execute | goal-implementer | root updates todos after |
| verify | goal-verifier | root on pass/fail |
| evaluate | goal-evaluator (report draft) | root goal.complete |

Mandatory order:
1. analyze — subagent.run name="goal-analyst". If trivial=true, answer without Goal.
2. plan — optional goal-planner draft; then goal.update(success_criteria, pipeline_phase="execute") + todo.write (one in_progress).
3. For EACH todo step:
   a. execute — subagent.run name="goal-implementer" with current step in task
   b. verify — subagent.run name="goal-verifier"; require evidence
   c. on pass: todo.write complete step + next in_progress + goal.checkpoint
   d. on fail: retry implementer or replan
4. evaluate — subagent.run name="goal-evaluator"
5. Publish completion report to the user, then goal.complete(status, summary, report_markdown).

Hard rules:
- Do NOT skip analyze/plan before large edits.
- Do NOT put analyze+implement+verify into one generic worker.
- Call exactly ONE specialist for the current pipeline_phase. Never dispatch later-phase specialists in the same tool batch.
- After a specialist returns, update pipeline_phase before calling the next specialist; out-of-phase specialists are rejected by Runtime.
- Do NOT goal.complete without a completion report summary.
- Specialists cannot call goal.* / todo.* / nested subagent.*; root owns session state.
- Prefer specialist names exactly: goal-analyst, goal-planner, goal-implementer, goal-verifier, goal-evaluator.`

// rootAgentTodoPolicy is injected for root runs that expose todo.write.
// Guides models to use the structured checklist instead of free-text plans or memory.kind=task.
const rootAgentTodoPolicy = `Session task list (todo.write / todo.list) — mandatory for multi-step work:

When to use:
- Use todo.write whenever the user request needs 2+ sequential steps (investigate then fix, multi-file change, implement feature, debug then verify, plan then execute).
- Skip todos only for trivial one-shot Q&A, pure greeting, or a single read/lookup with no follow-up work.
- Never use memory.create with kind=task as a work queue; memory is durable knowledge, todos are the live checklist.

How to use:
1. At the start of multi-step work, call todo.write once with the full plan (merge=false or a complete list). Mark the first active item in_progress; others pending.
2. Keep at most ONE item in_progress. Before switching work, mark the current item completed (or cancelled) and set the next to in_progress.
3. Prefer sending the full active list each write. merge defaults to true: omitted items are KEPT (not deleted). To drop items, set status=cancelled or use merge=false.
4. todos[].id may be your short key ("1","2") or the Gateway id returned earlier. Prefer reusing the same short keys across turns so items update in place.
5. Update the list when steps finish, fail, or the plan changes — do not only describe progress in prose.
6. Users see the list above the chat input; keep content short, concrete, and action-oriented (Chinese or English matching the user).
7. Call todo.list only if you need to re-read the list; the current checklist is also injected as system context when present.

Example first write:
  todos: [
    {id:"1", content:"定位失败用例", status:"in_progress"},
    {id:"2", content:"修复实现", status:"pending"},
    {id:"3", content:"跑测试确认", status:"pending"}
  ]`

func openAICompatibleMessages(req ProviderRequest) []map[string]any {
	messages := make([]map[string]any, 0, 4+len(req.Session.Conversation)+1+len(req.ToolHistory)*2)
	// Always inject fresh local time so the model does not rely on training-data dates.
	messages = append(messages, map[string]any{
		"role":    "system",
		"content": currentTimeContextMessage(time.Now()),
	})
	goalPipeline := hasToolNamed(req.Tools, "goal.write")
	if hasToolNamed(req.Tools, "subagent.run") && !goalPipeline {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": rootAgentOrchestrationPolicy,
		})
	}
	if hasSuccessfulSubagentResult(req.ToolHistory) && !goalPipeline {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": rootAgentPostDelegationPolicy,
		})
	}
	if goalPipeline {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": rootAgentGoalPipelinePolicy,
		})
	}
	if hasToolNamed(req.Tools, "todo.write") {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": rootAgentTodoPolicy,
		})
	}
	// Specialist/worker/skill role briefs are separate from long-term memory.
	if req.Options.SpecialistContext != nil && strings.TrimSpace(req.Options.SpecialistContext.Context) != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": strings.TrimSpace(req.Options.SpecialistContext.Context),
		})
	}
	if req.Options.MemoryContext != nil && strings.TrimSpace(req.Options.MemoryContext.Context) != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": strings.TrimSpace(req.Options.MemoryContext.Context),
		})
	}
	if req.Options.GoalContext != nil && strings.TrimSpace(req.Options.GoalContext.Context) != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": strings.TrimSpace(req.Options.GoalContext.Context),
		})
		// User/command may have already created+bound the Goal; prefer update over write.
		if id := strings.TrimSpace(req.Options.GoalContext.GoalID); id != "" {
			messages = append(messages, map[string]any{
				"role": "system",
				"content": "A session Goal is already bound to this run (id=" + id + "). " +
					"Do NOT call goal.write to create another. Use goal.update / goal.checkpoint / goal.complete and todo.write. " +
					"Start from the current pipeline_phase (usually analyze) with the phase specialists.",
			})
		}
	}
	if req.Options.TodoContext != nil && strings.TrimSpace(req.Options.TodoContext.Context) != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": strings.TrimSpace(req.Options.TodoContext.Context),
		})
	}
	if req.Options.SkillsContext != nil && strings.TrimSpace(req.Options.SkillsContext.Context) != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": strings.TrimSpace(req.Options.SkillsContext.Context),
		})
	}
	for _, message := range req.Session.Conversation {
		content := conversationMessageText(message)
		if content == "" {
			continue
		}
		role, ok := openAICompatibleConversationRole(message.Role)
		if !ok {
			continue
		}
		messages = append(messages, map[string]any{
			"role":    role,
			"content": content,
		})
	}
	messages = append(messages, map[string]any{"role": "user", "content": req.Input.Text})
	for _, round := range toolRoundsForRequest(req) {
		if len(round) == 0 {
			continue
		}
		toolCalls := make([]map[string]any, 0, len(round))
		for _, exchange := range round {
			arguments, _ := json.Marshal(exchange.Call.Arguments)
			toolCalls = append(toolCalls, map[string]any{
				"id":   exchange.Call.ID,
				"type": "function",
				"function": map[string]any{
					"name":      publicToolName(exchange.Call.Name),
					"arguments": string(arguments),
				},
			})
		}
		messages = append(messages, map[string]any{
			"role":       "assistant",
			"tool_calls": toolCalls,
		})
		for _, exchange := range round {
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": exchange.Call.ID,
				"name":         publicToolName(exchange.Call.Name),
				"content":      toolExchangeContent(exchange.Result),
			})
		}
	}
	return messages
}

func hasSuccessfulSubagentResult(history []ToolExchange) bool {
	for _, exchange := range history {
		if exchange.Call.Name == "subagent.run" && exchange.Result.Status == tools.CallStatusCompleted {
			return true
		}
	}
	return false
}

func toolRoundsForRequest(req ProviderRequest) [][]ToolExchange {
	if len(req.ToolRounds) > 0 {
		return req.ToolRounds
	}
	if len(req.ToolHistory) == 0 {
		return nil
	}
	rounds := make([][]ToolExchange, 0, len(req.ToolHistory))
	for _, exchange := range req.ToolHistory {
		rounds = append(rounds, []ToolExchange{exchange})
	}
	return rounds
}

// currentTimeContextMessage builds an authoritative local-time system note.
// Injected on every provider turn so multi-step tool loops also stay accurate.
func currentTimeContextMessage(now time.Time) string {
	zoneName, offsetSec := now.Zone()
	if zoneName == "" {
		zoneName = "Local"
	}
	weekdayCN := [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	weekday := weekdayCN[int(now.Weekday())]
	return fmt.Sprintf(
		"当前权威时间（本地时区）：%s %s（%s，UTC%s）。\n"+
			"Current local time: %s (%s, UTC%s).\n"+
			"请以此时间为准回答日期/时间相关问题，不要使用训练数据中的过时日期。",
		now.Format("2006-01-02 15:04:05"),
		weekday,
		zoneName,
		formatUTCOffset(offsetSec),
		now.Format(time.RFC3339),
		zoneName,
		formatUTCOffset(offsetSec),
	)
}

func formatUTCOffset(offsetSec int) string {
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	hours := offsetSec / 3600
	mins := (offsetSec % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, mins)
}

func hasToolNamed(definitions []tools.Definition, name string) bool {
	for _, definition := range definitions {
		if definition.Name == name {
			return true
		}
	}
	return false
}

// maxToolResultForModel caps each tool result fed back into the next LLM turn.
// Full standardized output remains available on the tool event / UI card.
const maxToolResultForModel = 16 * 1024

// toolExchangeContent formats a tool result for the next provider turn.
// Always returns a standardized JSON envelope (never silently drops fields).
func toolExchangeContent(result tools.Result) string {
	return modelFacingToolContent(result)
}

func openAICompatibleConversationRole(role string) (string, bool) {
	if role == "user" || role == "system" {
		return role, true
	}
	if role == "assistant" {
		return role, true
	}
	return "", false
}

func conversationMessageText(message methods.Message) string {
	parts := make([]string, 0, len(message.Content))
	for _, block := range message.Content {
		if text := strings.TrimSpace(block.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func openAICompatibleTools(definitions []tools.Definition) []map[string]any {
	items := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		items = append(items, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        publicToolName(definition.Name),
				"description": definition.Description,
				"parameters":  definition.Parameters,
			},
		})
	}
	return items
}

func publicToolName(name string) string {
	return strings.ReplaceAll(name, ".", "__")
}

func internalToolName(name string) string {
	return strings.ReplaceAll(name, "__", ".")
}

func boolEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

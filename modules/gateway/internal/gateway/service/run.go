package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/infra/runtimeclient"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	protows "redpanda/protocol/ws"
)

const (
	defaultMaxConcurrentRuns = 3
	maxConcurrentRunsCap     = 16
	// defaultRuntimeMode isolates multi-session concurrent runs in dedicated agent processes.
	defaultRuntimeMode = "per_run_process"
)

type RunService struct {
	repos   repository.Set
	hub     *eventhub.Hub
	runtime *runtimeclient.Client
	// startMu serializes admission so concurrent budget checks and slot reservation are atomic.
	startMu *sync.Mutex
}

type StartRunResult struct {
	RunID        string `json:"run_id"`
	SessionID    string `json:"session_id"`
	Accepted     bool   `json:"accepted"`
	Subscribed   bool   `json:"subscribed"`
	RuntimeMode  string `json:"runtime_mode"`
	RootSequence uint64 `json:"root_seq"`
}

type RunRecordDTO struct {
	ID            string     `json:"id"`
	SessionID     string     `json:"session_id"`
	WorkspaceRoot string     `json:"workspace_root,omitempty"`
	RuntimeMode   string     `json:"runtime_mode"`
	Status        string     `json:"status"`
	Input         string     `json:"input,omitempty"`
	LastEventType string     `json:"last_event_type,omitempty"`
	LastRootSeq   uint64     `json:"last_root_seq"`
	MessageCount  int        `json:"message_count"`
	ToolCount     int        `json:"tool_count"`
	Error         string     `json:"error,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type RunEventDTO struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	RootRunID   string         `json:"root_run_id"`
	RunID       string         `json:"run_id"`
	ParentRunID string         `json:"parent_run_id,omitempty"`
	SessionID   string         `json:"session_id"`
	RootSeq     uint64         `json:"root_seq"`
	AgentSeq    uint64         `json:"agent_seq"`
	AgentID     string         `json:"agent_id"`
	AgentRole   string         `json:"agent_role"`
	AgentName   string         `json:"agent_name,omitempty"`
	StreamKind  string         `json:"stream_kind,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

func NewRunService(repos repository.Set, hub *eventhub.Hub, runtime *runtimeclient.Client) RunService {
	return RunService{repos: repos, hub: hub, runtime: runtime, startMu: &sync.Mutex{}}
}

func (r RunService) RuntimeStatus() map[string]any {
	status := map[string]any{
		"available":            false,
		"mode":                 defaultRuntimeMode,
		"default_runtime_mode": defaultRuntimeMode,
		"max_concurrent_runs":  defaultMaxConcurrentRuns,
		"active_runs":          int64(0),
		"isolation":            "per_run_process",
	}
	if active, err := r.repos.Runs.CountActive(); err == nil {
		status["active_runs"] = active
	}
	if r.runtime == nil {
		status["reason"] = "runtime client not configured"
		return status
	}
	for key, value := range r.runtime.Status() {
		status[key] = value
	}
	// Prefer explicit default for clients that only look at mode when idle.
	if _, ok := status["default_runtime_mode"]; !ok {
		status["default_runtime_mode"] = defaultRuntimeMode
	}
	return status
}

// resolveMaxConcurrentRuns prefers per-run option, then env, then default.
func resolveMaxConcurrentRuns(options map[string]any) int {
	if n := intOption(options, "max_concurrent_runs"); n > 0 {
		if n > maxConcurrentRunsCap {
			return maxConcurrentRunsCap
		}
		return n
	}
	if env := strings.TrimSpace(os.Getenv("RED_PANDA_MAX_CONCURRENT_RUNS")); env != "" {
		if parsed, err := strconv.Atoi(env); err == nil && parsed > 0 {
			if parsed > maxConcurrentRunsCap {
				return maxConcurrentRunsCap
			}
			return parsed
		}
	}
	return defaultMaxConcurrentRuns
}

func (r RunService) Get(runID string) (RunRecordDTO, error) {
	row, err := r.repos.Runs.Get(runID)
	if err != nil {
		return RunRecordDTO{}, err
	}
	return runRecordDTO(row), nil
}

func (r RunService) ListBySession(sessionID string, limit int) ([]RunRecordDTO, error) {
	rows, err := r.repos.Runs.ListBySession(sessionID, limit)
	if err != nil {
		return nil, err
	}
	items := make([]RunRecordDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, runRecordDTO(row))
	}
	return items, nil
}

func (r RunService) Events(rootRunID string, afterSeq uint64, limit int) ([]RunEventDTO, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	items, err := r.repos.RunEvents.ListAfter(rootRunID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	dtos := make([]RunEventDTO, 0, len(items))
	for _, event := range items {
		dtos = append(dtos, runEventDTO(event))
	}
	return dtos, nil
}

func (r RunService) Start(ctx context.Context, payload protows.RunStartPayload) (StartRunResult, error) {
	if r.runtime == nil {
		return StartRunResult{}, fmt.Errorf("runtime client not configured")
	}

	runID := fmt.Sprintf("run_%d", time.Now().UnixNano())
	sessionID := payload.SessionID
	if sessionID == "" {
		sessionID = "session_" + runID
	}
	workspaceRoot := stringOption(payload.Options, "working_dir")
	session, err := r.repos.Sessions.Ensure(sessionID, sessionID, workspaceRoot)
	if err != nil {
		return StartRunResult{}, err
	}

	inputText := stringInput(payload.Input, "text")
	runtimeMode := normalizedRuntimeMode(stringOption(payload.Options, "runtime_mode"))

	// Atomic admission: session serial + global concurrent budget + reserve active slot.
	if r.startMu != nil {
		r.startMu.Lock()
	}
	sessionActive, err := r.repos.Runs.CountActiveBySession(session.ID)
	if err != nil {
		if r.startMu != nil {
			r.startMu.Unlock()
		}
		return StartRunResult{}, err
	}
	if sessionActive > 0 {
		if r.startMu != nil {
			r.startMu.Unlock()
		}
		return StartRunResult{}, fmt.Errorf("会话已有任务在运行中，请等待结束后再发送")
	}

	maxConcurrent := resolveMaxConcurrentRuns(payload.Options)
	active, err := r.repos.Runs.CountActive()
	if err != nil {
		if r.startMu != nil {
			r.startMu.Unlock()
		}
		return StartRunResult{}, err
	}
	if int(active) >= maxConcurrent {
		if r.startMu != nil {
			r.startMu.Unlock()
		}
		return StartRunResult{}, fmt.Errorf(
			"已达到最大并发运行数 %d（当前活跃 %d），请等待其它会话完成或在设置中提高上限",
			maxConcurrent, active,
		)
	}

	if err := r.repos.Runs.Start(model.RunRecord{
		ID:            runID,
		SessionID:     session.ID,
		WorkspaceRoot: session.WorkspaceRoot,
		RuntimeMode:   runtimeMode,
		Status:        "running",
		Input:         inputText,
		StartedAt:     time.Now().UTC(),
	}); err != nil {
		if r.startMu != nil {
			r.startMu.Unlock()
		}
		return StartRunResult{}, err
	}
	if r.startMu != nil {
		r.startMu.Unlock()
	}

	conversation, err := r.buildRunConversation(session.ID)
	if err != nil {
		_ = r.repos.Runs.Finish(runID, "failed", err.Error())
		return StartRunResult{}, err
	}
	if _, err := r.repos.Messages.Add(session.ID, "user", inputText, runID); err != nil {
		_ = r.repos.Runs.Finish(runID, "failed", err.Error())
		return StartRunResult{}, err
	}

	params := methods.ReplyParams{
		RunID: runID,
		Session: methods.ReplySession{
			ID:           session.ID,
			Name:         session.Name,
			WorkingDir:   session.WorkspaceRoot,
			Conversation: conversation,
		},
		Input: methods.ReplyInput{
			Text: inputText,
		},
		Options: methods.ReplyOptions{
			ProviderProfileID:   stringOption(payload.Options, "provider_profile_id"),
			Model:               stringOption(payload.Options, "model"),
			PermissionMode:      stringOption(payload.Options, "permission_mode"),
			ToolPolicy:          stringOption(payload.Options, "tool_policy"),
			ToolAllowlist:       stringSliceOption(payload.Options, "tool_allowlist"),
			ToolDenylist:        stringSliceOption(payload.Options, "tool_denylist"),
			EmitToolEvents:      true,
			RequirePermission:   boolOption(payload.Options, "require_permission"),
			SpawnSubAgents:      boolOption(payload.Options, "spawn_subagents"),
			SubAgentBackend:     stringOption(payload.Options, "subagent_backend"),
			WebSearchMaxResults: intOption(payload.Options, "web_search_max_results"),
			WebFetchMaxBytes:    intOption(payload.Options, "web_fetch_max_bytes"),
			WebSearchProvider:   stringOption(payload.Options, "web_search_provider"),
			WebTavilyAPIKey:     stringOption(payload.Options, "web_tavily_api_key"),
			WebHTTPProxy:        stringOption(payload.Options, "web_http_proxy"),
			MaxToolTurns:        intOption(payload.Options, "max_tool_turns"),
			LogLLMRequests:      boolOption(payload.Options, "log_llm_requests"),
		},
	}
	// Goal execution is opt-in. A missing option represents a regular
	// conversation and must not expose Goal tools or pipeline instructions.
	goalsEnabled := boolOption(payload.Options, "goals_enabled")
	params.Options.GoalsEnabled = &goalsEnabled
	if err := r.applyProviderProfile(&params); err != nil {
		_ = r.repos.Runs.Finish(runID, "failed", err.Error())
		return StartRunResult{}, err
	}
	if err := r.applyMemoryContext(&params); err != nil {
		_ = r.repos.Runs.Finish(runID, "failed", err.Error())
		return StartRunResult{}, err
	}
	if err := r.applyTodoContext(&params); err != nil {
		_ = r.repos.Runs.Finish(runID, "failed", err.Error())
		return StartRunResult{}, err
	}
	if err := r.applyGoalBindingAndContext(&params, payload.Options); err != nil {
		_ = r.repos.Runs.Finish(runID, "failed", err.Error())
		return StartRunResult{}, err
	}
	if err := r.applyAgentDefinitions(&params); err != nil {
		_ = r.repos.Runs.Finish(runID, "failed", err.Error())
		return StartRunResult{}, err
	}

	accepted, err := r.runtime.ReplyWithMode(ctx, runtimeMode, params)
	if err != nil {
		_ = NewGoalService(r.repos).PauseByRun(runID, "run_failed")
		_ = r.repos.Runs.Finish(runID, "failed", err.Error())
		return StartRunResult{}, err
	}
	_ = r.repos.Sessions.Touch(session.ID)
	return StartRunResult{
		RunID:       firstNonEmpty(accepted.RunID, runID),
		SessionID:   session.ID,
		Accepted:    accepted.Accepted,
		Subscribed:  payload.Subscribe,
		RuntimeMode: runtimeMode,
	}, nil
}

func (r RunService) buildRunConversation(sessionID string) ([]methods.Message, error) {
	history, err := r.repos.Messages.ListLatestConversation(sessionID, 200)
	if err != nil {
		return nil, err
	}
	conversation := make([]methods.Message, 0, len(history)+1)

	compaction, compactErr := r.repos.Compactions.LatestAppliedInPlace(sessionID)
	if compactErr == nil {
		var summary CompactSummary
		if err := json.Unmarshal([]byte(compaction.SummaryJSON), &summary); err != nil {
			return nil, fmt.Errorf("decode active compaction summary: %w", err)
		}
		tail, err := r.repos.Messages.ListConversationAfterSeq(sessionID, compaction.SourceEndSeq, 200)
		if err != nil {
			return nil, err
		}
		history = tail
		conversation = append(conversation, methods.Message{
			ID:   compaction.ID,
			Role: "system",
			Content: []methods.ContentBlock{{
				Type: "text",
				Text: fmt.Sprintf(
					"Conversation summary covering original messages %d-%d. Use it as prior context; the full original history remains stored in the session.\n\n%s",
					compaction.SourceStartSeq,
					compaction.SourceEndSeq,
					formatCompactSummaryMessage(summary),
				),
			}},
			CreatedAt: compaction.UpdatedAt.Format(time.RFC3339Nano),
		})
	} else if compactErr != gorm.ErrRecordNotFound {
		return nil, compactErr
	}

	for _, row := range history {
		message, err := messageDTO(row)
		if err != nil {
			return nil, err
		}
		conversation = append(conversation, methods.Message{
			ID:        message.ID,
			Role:      message.Role,
			Content:   message.Content,
			CreatedAt: message.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	return conversation, nil
}

func (r RunService) applyMemoryContext(params *methods.ReplyParams) error {
	preview, err := NewMemoryService(r.repos).PreviewRun(MemoryPreviewRunRequest{
		SessionID:     params.Session.ID,
		WorkspaceRoot: params.Session.WorkingDir,
		Input:         params.Input.Text,
	})
	if err != nil {
		return err
	}
	if len(preview.Items) == 0 || preview.Context == "" {
		return nil
	}
	items := make([]methods.MemoryItem, 0, len(preview.Items))
	for _, item := range preview.Items {
		items = append(items, methods.MemoryItem{
			ID:      item.ID,
			Scope:   item.Scope,
			Kind:    item.Kind,
			Title:   item.Title,
			Content: item.Content,
		})
	}
	params.Options.MemoryContext = &methods.MemoryContext{
		Items:   items,
		Context: preview.Context,
	}
	return nil
}

func (r RunService) applyTodoContext(params *methods.ReplyParams) error {
	ctx, err := NewTodoService(r.repos).FormatTodoContext(params.Session.ID)
	if err != nil {
		return err
	}
	if ctx == nil || strings.TrimSpace(ctx.Context) == "" {
		return nil
	}
	params.Options.TodoContext = ctx
	return nil
}

// applyAgentDefinitions attaches enabled Gateway agent profiles so Runtime can
// treat managed system prompts / default turns as the execution source of truth
// for goal specialists (builtin hardcode remains fallback only).
func (r RunService) applyAgentDefinitions(params *methods.ReplyParams) error {
	if params == nil {
		return nil
	}
	defs, err := NewAgentDefinitionService(r.repos).ListEnabled()
	if err != nil {
		return err
	}
	if len(defs) == 0 {
		return nil
	}
	out := make([]methods.AgentDefinitionRef, 0, len(defs))
	for _, d := range defs {
		out = append(out, methods.AgentDefinitionRef{
			Key:             d.Key,
			Name:            d.Name,
			NameZH:          d.NameZH,
			Phase:           d.Phase,
			SystemPrompt:    d.SystemPrompt,
			DefaultMaxTurns: d.DefaultMaxTurns,
			Enabled:         d.Enabled,
		})
	}
	params.Options.AgentDefinitions = out
	return nil
}

func (r RunService) applyProviderProfile(params *methods.ReplyParams) error {
	profileID := params.Options.ProviderProfileID
	if profileID == "" {
		return nil
	}
	profile, err := r.repos.Providers.Get(profileID)
	if err != nil {
		return fmt.Errorf("provider profile %s not found: %w", profileID, err)
	}
	if !profile.Active {
		return fmt.Errorf("provider profile %s is inactive", profileID)
	}
	params.Options.ProviderName = profile.Provider
	params.Options.ProviderBaseURL = profile.BaseURL
	params.Options.ProviderAPIKey = profile.APIKeySecret
	if params.Options.Model == "" {
		params.Options.Model = profile.Model
	}
	return nil
}

func (r RunService) Cancel(ctx context.Context, runID string, reason string) error {
	if r.runtime == nil {
		return fmt.Errorf("runtime client not configured")
	}
	// Pause bound Goal before killing the process so force-finish still leaves a clean pause.
	pauseReason := "user_cancel"
	if strings.Contains(strings.ToLower(reason), "compact") {
		pauseReason = "session_compact"
	}
	_ = NewGoalService(r.repos).PauseByRun(runID, pauseReason)
	return r.runtime.Cancel(ctx, methods.CancelParams{RunID: runID, Reason: reason})
}

// StartGoal creates a user-initiated Goal and starts a bound run (slash /goal …).
func (r RunService) StartGoal(ctx context.Context, sessionID, objective, title, successCriteria string, options map[string]any) (StartRunResult, error) {
	if r.runtime == nil {
		return StartRunResult{}, fmt.Errorf("runtime client not configured")
	}
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return StartRunResult{}, fmt.Errorf("objective is required")
	}
	goalSvc := NewGoalService(r.repos)
	goal, err := goalSvc.CreateUserInitiated(sessionID, objective, title, successCriteria)
	if err != nil {
		return StartRunResult{}, err
	}

	displayTitle := goal.Title
	if displayTitle == "" {
		displayTitle = truncateRunes(goal.Objective, 40)
	}
	criteria := goal.SuccessCriteria
	if criteria == "" {
		criteria = "完成用户所述目标，并通过合理验证（测试/检查/可演示结果）。"
	}
	var b strings.Builder
	b.WriteString("[启动目标] ")
	b.WriteString(displayTitle)
	b.WriteString("\n目标：")
	b.WriteString(goal.Objective)
	b.WriteString("\n成功标准：")
	b.WriteString(criteria)
	b.WriteString("\n阶段：analyze\n\n")
	b.WriteString("本 Goal 已由用户指令创建并绑定到本次 run。请按 Goal 流水线执行：\n")
	b.WriteString("1. 用 goal-analyst 做范围分析（若 trivial 可直接说明后 goal.complete）\n")
	b.WriteString("2. 用 goal.update 补充 analysis_summary / success_criteria / pipeline_phase，并用 todo.write 写出步骤\n")
	b.WriteString("3. 逐步 execute → verify，goal.checkpoint 记录进度\n")
	b.WriteString("4. evaluate 后 goal.complete，并给用户完整报告\n")
	b.WriteString("不要重新 goal.write 新建目标；当前会话已绑定本 Goal。\n")

	opts := map[string]any{}
	for k, v := range options {
		opts[k] = v
	}
	opts["goal_id"] = goal.ID
	opts["goals_enabled"] = true
	// Avoid double-create if caller also set create_goal.
	delete(opts, "create_goal")

	return r.Start(ctx, protows.RunStartPayload{
		SessionID: sessionID,
		Input:     map[string]any{"text": b.String()},
		Options:   opts,
		Subscribe: true,
	})
}

// ContinueGoal starts a new run bound to an existing paused/pending goal.
func (r RunService) ContinueGoal(ctx context.Context, sessionID, goalID, extraText string, options map[string]any) (StartRunResult, error) {
	if r.runtime == nil {
		return StartRunResult{}, fmt.Errorf("runtime client not configured")
	}
	goalSvc := NewGoalService(r.repos)
	if err := goalSvc.RepairStaleActive(sessionID); err != nil {
		return StartRunResult{}, err
	}
	goal, err := goalSvc.Get(sessionID, goalID)
	if err != nil {
		return StartRunResult{}, err
	}
	switch goal.Status {
	case "paused", "pending":
		// ok
	case "active":
		return StartRunResult{}, fmt.Errorf("goal is already active; wait for the current run or cancel it")
	default:
		return StartRunResult{}, fmt.Errorf("goal status %q cannot continue", goal.Status)
	}

	title := goal.Title
	if title == "" {
		title = truncateRunes(goal.Objective, 40)
	}
	var b strings.Builder
	b.WriteString("[继续目标] ")
	b.WriteString(title)
	b.WriteString("\n")
	if goal.CheckpointSummary != "" {
		b.WriteString("检查点：")
		b.WriteString(goal.CheckpointSummary)
		b.WriteString("\n")
	}
	if goal.SuccessCriteria != "" {
		b.WriteString("成功标准：")
		b.WriteString(goal.SuccessCriteria)
		b.WriteString("\n")
	}
	if goal.PipelinePhase != "" {
		b.WriteString("阶段：")
		b.WriteString(goal.PipelinePhase)
		b.WriteString("\n")
	}
	b.WriteString("请从检查点继续，不要无故整单重做分析，除非范围已变化。\n")
	if strings.TrimSpace(extraText) != "" {
		b.WriteString("用户补充：")
		b.WriteString(strings.TrimSpace(extraText))
		b.WriteString("\n")
	}

	opts := map[string]any{}
	for k, v := range options {
		opts[k] = v
	}
	opts["goal_id"] = goal.ID
	opts["continue_goal"] = true
	opts["goals_enabled"] = true

	return r.Start(ctx, protows.RunStartPayload{
		SessionID: sessionID,
		Input:     map[string]any{"text": b.String()},
		Options:   opts,
		Subscribe: true,
	})
}

func (r RunService) applyGoalBindingAndContext(params *methods.ReplyParams, options map[string]any) error {
	goalSvc := NewGoalService(r.repos)
	_ = goalSvc.RepairStaleActive(params.Session.ID)

	goalID := stringOption(options, "goal_id")
	continueGoal := boolOption(options, "continue_goal")
	createGoal := boolOption(options, "create_goal")
	if params.Options.GoalsEnabled != nil && !*params.Options.GoalsEnabled {
		if goalID != "" || continueGoal || createGoal {
			return fmt.Errorf("goals are disabled")
		}
		return nil
	}

	// User-initiated Goal from slash command / run.start create_goal.
	if createGoal && goalID == "" {
		objective := strings.TrimSpace(stringOption(options, "goal_objective"))
		if objective == "" {
			objective = strings.TrimSpace(params.Input.Text)
		}
		if objective == "" {
			return fmt.Errorf("create_goal requires goal_objective or input text")
		}
		created, err := goalSvc.CreateUserInitiated(
			params.Session.ID,
			objective,
			stringOption(options, "goal_title"),
			stringOption(options, "goal_success_criteria"),
		)
		if err != nil {
			return err
		}
		goalID = created.ID
	}

	if goalID != "" || continueGoal {
		bound, err := goalSvc.BindToRun(params.Session.ID, params.RunID, goalID, continueGoal)
		if err != nil {
			return err
		}
		params.Options.GoalID = bound.ID
		ctx, err := goalSvc.FormatGoalContext(params.Session.ID, bound.ID)
		if err != nil {
			return err
		}
		params.Options.GoalContext = ctx
		if bound.MaxToolTurnsSeg > 0 && (params.Options.MaxToolTurns <= 0 || params.Options.MaxToolTurns > bound.MaxToolTurnsSeg) {
			params.Options.MaxToolTurns = bound.MaxToolTurnsSeg
		}
		return nil
	}
	// No default bind: only inject context if options already carried a goal_id from client.
	return nil
}

func (r RunService) SubAgents(ctx context.Context, params methods.SubAgentsParams) (methods.SubAgentsResult, error) {
	if r.runtime == nil {
		return methods.SubAgentsResult{}, fmt.Errorf("runtime client not configured")
	}
	return r.runtime.SubAgents(ctx, params)
}

func (r RunService) CancelSubAgent(ctx context.Context, params methods.SubAgentCancelParams) (methods.SubAgentCancelResult, error) {
	if r.runtime == nil {
		return methods.SubAgentCancelResult{}, fmt.Errorf("runtime client not configured")
	}
	return r.runtime.CancelSubAgent(ctx, params)
}

func (r RunService) ResolvePermission(ctx context.Context, params permission.ResolveParams) (permission.ResolveResult, error) {
	if r.runtime == nil {
		return permission.ResolveResult{}, fmt.Errorf("runtime client not configured")
	}
	result, err := r.runtime.ResolvePermission(ctx, params)
	if err != nil {
		return permission.ResolveResult{}, err
	}
	_ = r.repos.Permissions.Resolve(params)
	return result, nil
}

func (r RunService) Subscribe(rootRunID string) (<-chan events.Envelope, func()) {
	return r.hub.Subscribe(rootRunID)
}

func (r RunService) Replay(rootRunID string, afterSeq uint64) ([]events.Envelope, error) {
	return r.repos.RunEvents.ListAfter(rootRunID, afterSeq, 500)
}

func (r RunService) HandleRuntimeEvent(event events.Envelope) {
	if event.EventID == "" || event.RootRunID == "" {
		return
	}
	_ = r.repos.Runs.ProjectEvent(event)
	if event.Type == events.EventPermissionRequest {
		_ = r.repos.Permissions.ProjectRequired(event)
	}
	if isToolEvent(event.Type) {
		_ = r.repos.ToolCalls.Project(event)
		_ = r.repos.Runs.RefreshToolCount(event.RootRunID)
	}
	if event.Type == events.EventFinish || event.Type == events.EventError {
		_ = r.repos.Permissions.ClosePendingByRun(event.RootRunID, "closed", "run finished")
		status := "completed"
		if event.Type == events.EventError {
			status = "failed"
		} else if s, ok := event.Payload["status"].(string); ok && s != "" {
			status = s
		}
		if reason, _ := event.Payload["loop_end_reason"].(string); reason == "budget_exhausted" {
			status = "budget_exhausted"
		}
		_ = NewGoalService(r.repos).OnRootRunTerminal(event.RootRunID, event.SessionID, status)
	}
	_ = r.repos.RunEvents.Save(event)
	if event.Type == events.EventMessageDelta || event.Type == events.EventReasoningDelta {
		if delta, ok := event.Payload["delta"].(string); ok && delta != "" {
			role := "assistant"
			if event.Agent.Role == events.AgentRoleSubAgent {
				role = "subagent"
			}
			_, _ = r.repos.Messages.AddOrAppend(event.SessionID, role, delta, event.RootRunID)
			_ = r.repos.Sessions.Touch(event.SessionID)
		}
	}
	r.hub.Publish(event)
}

func isToolEvent(typ events.EventType) bool {
	return typ == events.EventToolStarted ||
		typ == events.EventToolOutput ||
		typ == events.EventToolFinished ||
		typ == events.EventToolFailed
}

func stringInput(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	value, _ := input[key].(string)
	return value
}

func stringOption(options map[string]any, key string) string {
	if options == nil {
		return ""
	}
	switch value := options[key].(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

func boolOption(options map[string]any, key string) bool {
	if options == nil {
		return false
	}
	value, _ := options[key].(bool)
	return value
}

func intOption(options map[string]any, key string) int {
	if options == nil {
		return 0
	}
	switch value := options[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	}
	return 0
}

func stringSliceOption(options map[string]any, key string) []string {
	if options == nil {
		return nil
	}
	raw, ok := options[key]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case []string:
		return value
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && text != "" {
				items = append(items, text)
			}
		}
		return items
	default:
		return nil
	}
}

func normalizedRuntimeMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "", "default", "auto":
		return defaultRuntimeMode
	case "per_run_process":
		return "per_run_process"
	case "single_core":
		return "single_core"
	default:
		return defaultRuntimeMode
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func runRecordDTO(row model.RunRecord) RunRecordDTO {
	return RunRecordDTO{
		ID:            row.ID,
		SessionID:     row.SessionID,
		WorkspaceRoot: row.WorkspaceRoot,
		RuntimeMode:   row.RuntimeMode,
		Status:        row.Status,
		Input:         row.Input,
		LastEventType: row.LastEventType,
		LastRootSeq:   row.LastRootSeq,
		MessageCount:  row.MessageCount,
		ToolCount:     row.ToolCount,
		Error:         row.Error,
		StartedAt:     row.StartedAt,
		FinishedAt:    row.FinishedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func runEventDTO(event events.Envelope) RunEventDTO {
	streamKind := ""
	if event.Stream != nil {
		streamKind = string(event.Stream.Kind)
	}
	return RunEventDTO{
		ID:          event.EventID,
		Type:        string(event.Type),
		RootRunID:   event.RootRunID,
		RunID:       event.RunID,
		ParentRunID: event.ParentRunID,
		SessionID:   event.SessionID,
		RootSeq:     event.RootSeq,
		AgentSeq:    event.AgentSeq,
		AgentID:     event.Agent.AgentID,
		AgentRole:   string(event.Agent.Role),
		AgentName:   event.Agent.Name,
		StreamKind:  streamKind,
		Payload:     event.Payload,
		CreatedAt:   event.CreatedAt,
	}
}

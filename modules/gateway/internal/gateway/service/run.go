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
	protocolmcp "redpanda/protocol/mcp"
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
	RunID       string `json:"run_id"`
	SessionID   string `json:"session_id"`
	Accepted    bool   `json:"accepted"`
	Subscribed  bool   `json:"subscribed"`
	RuntimeMode string `json:"runtime_mode"`
	RunSequence uint64 `json:"run_seq"`
}

type RunRecordDTO struct {
	ID            string     `json:"id"`
	SessionID     string     `json:"session_id"`
	WorkspaceRoot string     `json:"workspace_root,omitempty"`
	RuntimeMode   string     `json:"runtime_mode"`
	Status        string     `json:"status"`
	Input         string     `json:"input,omitempty"`
	LastEventType string     `json:"last_event_type,omitempty"`
	LastRunSeq    uint64     `json:"last_run_seq"`
	MessageCount  int        `json:"message_count"`
	ToolCount     int        `json:"tool_count"`
	Error         string     `json:"error,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type RunEventDTO struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	RunID        string         `json:"run_id"`
	SessionID    string         `json:"session_id"`
	AssignmentID string         `json:"assignment_id"`
	RunSeq       uint64         `json:"run_seq"`
	WorkerSeq    uint64         `json:"worker_seq"`
	WorkerID     string         `json:"worker_id"`
	ProfileKey   string         `json:"profile_key,omitempty"`
	StreamKind   string         `json:"stream_kind,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

func NewRunService(repos repository.Set, hub *eventhub.Hub, runtime *runtimeclient.Client) RunService {
	service := RunService{repos: repos, hub: hub, runtime: runtime, startMu: &sync.Mutex{}}
	if runtime != nil {
		runtime.SetRunExitHandler(service.HandleRuntimeExit)
	}
	return service
}

// RecoverStaleRunsOnStartup marks any leftover "running"/"waiting_permission" runs as failed.
// Call this exactly once during gateway boot to release the concurrency budget after crashes/restarts.
func (r RunService) RecoverStaleRunsOnStartup() (int64, error) {
	return r.repos.Runs.RecoverStaleRuns()
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

	params := methods.RunExecuteParams{
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
		Options: methods.RunExecuteOptions{
			ProviderProfileID:   stringOption(payload.Options, "provider_profile_id"),
			Model:               stringOption(payload.Options, "model"),
			PermissionMode:      stringOption(payload.Options, "permission_mode"),
			ToolPolicy:          stringOption(payload.Options, "tool_policy"),
			ToolAllowlist:       stringSliceOption(payload.Options, "tool_allowlist"),
			ToolDenylist:        stringSliceOption(payload.Options, "tool_denylist"),
			EmitToolEvents:      true,
			RequirePermission:   boolOption(payload.Options, "require_permission"),
			WebSearchMaxResults: intOption(payload.Options, "web_search_max_results"),
			WebFetchMaxBytes:    intOption(payload.Options, "web_fetch_max_bytes"),
			WebSearchProvider:   stringOption(payload.Options, "web_search_provider"),
			WebTavilyAPIKey:     stringOption(payload.Options, "web_tavily_api_key"),
			WebHTTPProxy:        stringOption(payload.Options, "web_http_proxy"),
			MaxToolTurns:        intOption(payload.Options, "max_tool_turns"),
			LogLLMRequests:      boolOption(payload.Options, "log_llm_requests"),
			WorkerPoolSize:      intOption(payload.Options, "worker_pool_size"),
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
	// From this point the Goal may be active and bound to runID. Any later
	// admission failure must pause the Goal so it is not left stuck active.
	if err := r.applyWorkerProfiles(&params); err != nil {
		_ = NewGoalService(r.repos).PauseByRun(runID, "run_failed")
		_ = r.repos.Runs.Finish(runID, "failed", err.Error())
		return StartRunResult{}, err
	}
	if err := r.applyMCPServers(&params); err != nil {
		_ = NewGoalService(r.repos).PauseByRun(runID, "run_failed")
		_ = r.repos.Runs.Finish(runID, "failed", err.Error())
		return StartRunResult{}, err
	}

	accepted, err := r.runtime.ExecuteWithMode(ctx, runtimeMode, params)
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

func (r RunService) applyMemoryContext(params *methods.RunExecuteParams) error {
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

func (r RunService) applyTodoContext(params *methods.RunExecuteParams) error {
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

// applyMCPServers attaches enabled MCP server configs (with secrets) so Runtime
// can discover tools and execute tools/call (docs/36 D2). Gateway never starts
// MCP processes itself.
func (r RunService) applyMCPServers(params *methods.RunExecuteParams) error {
	if params == nil {
		return nil
	}
	rows, err := r.repos.MCPServers.List(100)
	if err != nil {
		return err
	}
	out := make([]protocolmcp.MCPServerConfig, 0, len(rows))
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		out = append(out, mcpServerProtocolConfig(row, false))
	}
	if len(out) == 0 {
		return nil
	}
	params.Options.MCPServers = out
	return nil
}

// applyWorkerProfiles attaches the enabled Gateway profile snapshot. Runtime
// must not consult the legacy agent_definitions table for v0.2 executions.
func (r RunService) applyWorkerProfiles(params *methods.RunExecuteParams) error {
	if params == nil {
		return nil
	}
	profiles, err := NewWorkerProfileService(r.repos).ListEnabled()
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		return nil
	}
	out := make([]methods.WorkerProfileRef, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, methods.WorkerProfileRef{
			Key:             profile.Key,
			Name:            profile.Name,
			NameZH:          profile.NameZH,
			Description:     profile.Description,
			Phase:           profile.Phase,
			SystemPrompt:    profile.SystemPrompt,
			ProviderName:    profile.Provider,
			Model:           profile.Model,
			ToolPolicy:      "risk_based",
			ToolAllowlist:   append([]string(nil), profile.ToolAllowlist...),
			ToolDenylist:    append([]string(nil), profile.ToolDenylist...),
			DefaultMaxTurns: profile.DefaultMaxTurns,
			Enabled:         profile.Enabled,
		})
	}
	params.Options.WorkerProfiles = out
	return nil
}

func (r RunService) applyProviderProfile(params *methods.RunExecuteParams) error {
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
	_, err := r.runtime.CancelRun(ctx, methods.RunCancelParams{RunID: runID, Reason: reason})
	return err
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
	criteria := criteriaText(goal.Criteria)
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
	b.WriteString("\n本 Goal 已由用户创建并绑定到本次 run。请作为目标控制器推进结果：\n")
	b.WriteString("1. 读取 Goal contract 和现有证据，识别当前结果与成功标准之间最大的差距\n")
	b.WriteString("2. 用 goal.plan 选择或修订最有价值的下一批 actions；计划可以随证据改变，不要求预先冻结\n")
	b.WriteString("3. 执行当前 action 后用 goal.observe 记录真实结果和证据\n")
	b.WriteString("4. 用 goal.assess 对每条 criterion 作证据化判断，并决定继续、调整、阻塞或已满足\n")
	b.WriteString("5. 只有 persisted assessment=satisfied 且所有 criteria=met 时才能 goal.finish succeeded\n")
	b.WriteString("不要创建第二个 Goal，也不要把 Session TODO 当作 Goal 完成条件。\n")

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
	if goal.LastAssessment != nil {
		b.WriteString("上次评估：")
		b.WriteString(goal.LastAssessment.Verdict)
		b.WriteString(" - ")
		b.WriteString(goal.LastAssessment.Summary)
		b.WriteString("\n")
	}
	if criteria := criteriaText(goal.Criteria); criteria != "" {
		b.WriteString("成功标准：")
		b.WriteString(criteria)
		b.WriteString("\n")
	}
	if goal.CurrentAction != "" {
		b.WriteString("当前行动：")
		b.WriteString(goal.CurrentAction)
		b.WriteString("\n")
	}
	b.WriteString("请从已持久化的 evidence、assessment 和 action queue 继续；先判断差距是否变化，再选择下一行动。\n")
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

func (r RunService) applyGoalBindingAndContext(params *methods.RunExecuteParams, options map[string]any) error {
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
		// Goal budgets are authoritative: client max_tool_turns may only tighten.
		params.Options.MaxToolTurns = clampClientMaxToolTurns(params.Options.MaxToolTurns, bound)
		return nil
	}
	// No default bind: only inject context if options already carried a goal_id from client.
	return nil
}

// clampClientMaxToolTurns forces the Runtime segment loop budget to the Goal
// segment limit (and remaining total turns), never allowing a client option to
// raise the effective ceiling above the Goal configuration.
func clampClientMaxToolTurns(clientMax int, bound methods.GoalDTO) int {
	limit := bound.MaxToolTurnsSeg
	if bound.MaxTotalToolTurns > 0 {
		remaining := bound.MaxTotalToolTurns - bound.UsedToolTurns
		if remaining < 0 {
			remaining = 0
		}
		if remaining == 0 {
			// Exhausted total budget: keep a 1-turn ceiling so Runtime can
			// emit a controlled finish rather than unbounded tool loops.
			remaining = 1
		}
		if limit <= 0 || remaining < limit {
			limit = remaining
		}
	}
	if limit <= 0 {
		return clientMax
	}
	if clientMax <= 0 || clientMax > limit {
		return limit
	}
	return clientMax
}

func (r RunService) Workers(ctx context.Context, params methods.WorkerListParams) (methods.WorkerListResult, error) {
	if r.runtime == nil {
		return methods.WorkerListResult{}, fmt.Errorf("runtime client not configured")
	}
	return r.runtime.Workers(ctx, params)
}

func (r RunService) CancelAssignment(ctx context.Context, params methods.WorkerAssignmentCancelParams) (methods.WorkerAssignmentCancelResult, error) {
	if r.runtime == nil {
		return methods.WorkerAssignmentCancelResult{}, fmt.Errorf("runtime client not configured")
	}
	return r.runtime.CancelAssignment(ctx, params)
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

func (r RunService) Subscribe(runID string) (<-chan events.EnvelopeV2, func()) {
	return r.hub.Subscribe(runID)
}

func (r RunService) Replay(runID string, afterSeq uint64) ([]events.EnvelopeV2, error) {
	return r.repos.RunEvents.ListAfter(runID, afterSeq, 500)
}

func (r RunService) HandleRuntimeEvent(event events.EnvelopeV2) {
	if event.EventID == "" || event.RunID == "" {
		return
	}
	_ = r.repos.Runs.ProjectEvent(event)
	if event.Type == events.EventPermissionRequest {
		_ = r.repos.Permissions.ProjectRequired(event)
	}
	if isToolEvent(event.Type) {
		_ = r.repos.ToolCalls.Project(event)
		_ = r.repos.Runs.RefreshToolCount(event.RunID)
	}
	if event.Type == events.EventFinish || event.Type == events.EventError {
		_ = r.repos.Permissions.ClosePendingByRun(event.RunID, "closed", "run finished")
		status := "completed"
		if event.Type == events.EventError {
			status = "failed"
		} else if s, ok := event.Payload["status"].(string); ok && s != "" {
			status = s
		}
		if reason, _ := event.Payload["loop_end_reason"].(string); reason == "budget_exhausted" {
			status = "budget_exhausted"
		}
		_ = NewGoalService(r.repos).OnRootRunTerminal(event.RunID, event.SessionID, status)
	}
	_ = r.repos.RunEvents.Save(event)
	if (event.Type == events.EventMessageDelta || event.Type == events.EventReasoningDelta) && payloadString(event.Payload, "visibility") != "worker_private" {
		if delta, ok := event.Payload["delta"].(string); ok && delta != "" {
			metadata, _ := json.Marshal(map[string]any{
				"assignment_id": event.AssignmentID,
				"worker_id":     event.Worker.ID,
				"profile_key":   event.Worker.ProfileKey,
				"visibility":    payloadString(event.Payload, "visibility"),
			})
			_, _ = r.repos.Messages.AddOrAppendWithMetadata(event.SessionID, "assistant", delta, event.RunID, string(metadata))
			_ = r.repos.Sessions.Touch(event.SessionID)
		}
	}
	r.hub.Publish(event)
}

// HandleRuntimeExit closes a run whose dedicated agent process disappeared
// without sending EventFinish/EventError. Without this, its persisted "running"
// record permanently consumes a global concurrency slot until gateway restart.
func (r RunService) HandleRuntimeExit(runID string, exitErr error) {
	row, err := r.repos.Runs.Get(runID)
	if err != nil || (row.Status != "running" && row.Status != "waiting_permission") {
		return
	}
	message := "dedicated runtime process exited before the run finished"
	if exitErr != nil {
		message = fmt.Sprintf("%s: %v", message, exitErr)
	}
	r.HandleRuntimeEvent(events.EnvelopeV2{
		ProtocolVersion: events.ProtocolVersionV2,
		EventID:         fmt.Sprintf("evt_runtime_exit_%d", time.Now().UnixNano()),
		RunID:           row.ID,
		SessionID:       row.SessionID,
		RunSeq:          row.LastRootSeq + 1,
		Type:            events.EventError,
		Payload: map[string]any{
			"status":  "failed",
			"message": message,
		},
		CreatedAt: time.Now().UTC(),
	})
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

func payloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
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
		LastRunSeq:    row.LastRootSeq,
		MessageCount:  row.MessageCount,
		ToolCount:     row.ToolCount,
		Error:         row.Error,
		StartedAt:     row.StartedAt,
		FinishedAt:    row.FinishedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func runEventDTO(event events.EnvelopeV2) RunEventDTO {
	streamKind := ""
	if event.Stream != nil {
		streamKind = string(event.Stream.Kind)
	}
	return RunEventDTO{
		ID:           event.EventID,
		Type:         string(event.Type),
		RunID:        event.RunID,
		SessionID:    event.SessionID,
		AssignmentID: event.AssignmentID,
		RunSeq:       event.RunSeq,
		WorkerSeq:    event.WorkerSeq,
		WorkerID:     event.Worker.ID,
		ProfileKey:   event.Worker.ProfileKey,
		StreamKind:   streamKind,
		Payload:      event.Payload,
		CreatedAt:    event.CreatedAt,
	}
}

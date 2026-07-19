package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"redpanda/gateway/internal/gateway/model"
	protocolmcp "redpanda/protocol/mcp"
	"redpanda/protocol/methods"
	protows "redpanda/protocol/ws"
)

// Run admission → prepare → dispatch helpers (docs/41 W5-4 / docs/38 W1-A).
func (r RunService) admitRun(payload protows.RunStartPayload) (runAdmission, error) {
	runID := fmt.Sprintf("run_%d", time.Now().UnixNano())
	sessionID := payload.SessionID
	if sessionID == "" {
		sessionID = "session_" + runID
	}
	workspaceRoot := stringOption(payload.Options, "working_dir")
	session, err := r.repos.Sessions.Ensure(sessionID, sessionID, workspaceRoot)
	if err != nil {
		return runAdmission{}, err
	}

	inputText := stringInput(payload.Input, "text")
	runtimeMode := normalizedRuntimeMode(stringOption(payload.Options, "runtime_mode"))

	// Atomic admission: session serial + global concurrent budget + reserve active slot.
	if r.startMu != nil {
		r.startMu.Lock()
		defer r.startMu.Unlock()
	}
	sessionActive, err := r.repos.Runs.CountActiveBySession(session.ID)
	if err != nil {
		return runAdmission{}, err
	}
	if sessionActive > 0 {
		return runAdmission{}, fmt.Errorf("会话已有任务在运行中，请等待结束后再发送")
	}

	maxConcurrent := resolveMaxConcurrentRuns(payload.Options)
	active, err := r.repos.Runs.CountActive()
	if err != nil {
		return runAdmission{}, err
	}
	if int(active) >= maxConcurrent {
		return runAdmission{}, fmt.Errorf(
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
		TriggerSource: stringOption(payload.Options, "trigger_source"),
		TriggerRef:    stringOption(payload.Options, "trigger_ref"),
		StartedAt:     time.Now().UTC(),
	}); err != nil {
		return runAdmission{}, err
	}
	return runAdmission{
		runID:       runID,
		session:     session,
		inputText:   inputText,
		runtimeMode: runtimeMode,
	}, nil
}

func (r RunService) prepareRun(admission runAdmission, payload protows.RunStartPayload) (params methods.RunExecuteParams, pauseGoalOnFailure bool, err error) {
	conversation, err := buildModelConversation(r.repos, admission.session.ID)
	if err != nil {
		return methods.RunExecuteParams{}, false, err
	}
	if _, err := r.repos.Messages.Add(admission.session.ID, "user", admission.inputText, admission.runID); err != nil {
		return methods.RunExecuteParams{}, false, err
	}

	params = methods.RunExecuteParams{
		RunID: admission.runID,
		Session: methods.ReplySession{
			ID:           admission.session.ID,
			Name:         admission.session.Name,
			WorkingDir:   admission.session.WorkspaceRoot,
			Conversation: conversation,
		},
		Input: methods.ReplyInput{
			Text: admission.inputText,
		},
		Options: methods.RunExecuteOptions{
			ProviderProfileID:   stringOption(payload.Options, "provider_profile_id"),
			Model:               stringOption(payload.Options, "model"),
			ReasoningEffort:     stringOption(payload.Options, "reasoning_effort"),
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
		return methods.RunExecuteParams{}, false, err
	}
	if err := r.applyMemoryContext(&params); err != nil {
		return methods.RunExecuteParams{}, false, err
	}
	if err := r.applyTodoContext(&params); err != nil {
		return methods.RunExecuteParams{}, false, err
	}
	if err := r.applyGoalBindingAndContext(&params, payload.Options); err != nil {
		return methods.RunExecuteParams{}, false, err
	}
	// From this point the Goal may be active and bound to the admitted run.
	if err := r.applyWorkerProfiles(&params); err != nil {
		return methods.RunExecuteParams{}, true, err
	}
	if err := r.applyMCPServers(&params); err != nil {
		return methods.RunExecuteParams{}, true, err
	}
	return params, true, nil
}

func (r RunService) dispatchRun(ctx context.Context, admission runAdmission, params methods.RunExecuteParams, subscribe bool) (StartRunResult, error) {
	accepted, err := r.runtime.ExecuteWithMode(ctx, admission.runtimeMode, params)
	if err != nil {
		return StartRunResult{}, err
	}
	_ = r.repos.Sessions.Touch(admission.session.ID)
	return StartRunResult{
		RunID:       firstNonEmpty(accepted.RunID, admission.runID),
		SessionID:   admission.session.ID,
		Accepted:    accepted.Accepted,
		Subscribed:  subscribe,
		RuntimeMode: admission.runtimeMode,
	}, nil
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
	params.Options.ProviderStream = &profile.Stream
	if params.Options.Model == "" {
		params.Options.Model = profile.Model
	}
	models := providerModelsForRead(profile)
	if len(models) > 0 {
		found := false
		for _, item := range models {
			if item.Model != params.Options.Model {
				continue
			}
			found = true
			if params.Options.ReasoningEffort == "" {
				params.Options.ReasoningEffort = item.ReasoningEffort
			}
			break
		}
		if !found {
			return fmt.Errorf("model %q is not configured for provider profile %s", params.Options.Model, profileID)
		}
	}
	effort, err := NormalizeReasoningEffort(params.Options.ReasoningEffort)
	if err != nil {
		return err
	}
	params.Options.ReasoningEffort = effort
	return nil
}

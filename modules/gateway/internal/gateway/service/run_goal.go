package service

import (
	"context"
	"fmt"
	"strings"

	"redpanda/protocol/methods"
	protows "redpanda/protocol/ws"
)

// Goal-bound run entrypoints and binding into RunExecute params.
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

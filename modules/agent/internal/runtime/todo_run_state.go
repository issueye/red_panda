package runtime

import (
	"fmt"
	"strings"

	"redpanda/protocol/methods"
)

func (r *Runtime) setRunTodos(runID string, items []methods.TodoItemDTO) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runTodos == nil {
		r.runTodos = map[string][]methods.TodoItemDTO{}
	}
	copied := append([]methods.TodoItemDTO(nil), items...)
	r.runTodos[runID] = copied
}

func (r *Runtime) getRunTodos(runID string) []methods.TodoItemDTO {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := r.runTodos[runID]
	if len(items) == 0 {
		return nil
	}
	return append([]methods.TodoItemDTO(nil), items...)
}

func (r *Runtime) clearRunTodos(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.runTodos, runID)
}

func (r *Runtime) todoContextForRun(runID string) *methods.TodoContext {
	items := r.getRunTodos(runID)
	if len(items) == 0 {
		return nil
	}
	return formatTodoContext(items)
}

const todoContextHeader = `Current session task list (update via todo.write when progress changes; keep one in_progress):
`

func formatTodoContext(items []methods.TodoItemDTO) *methods.TodoContext {
	const limit = 3000
	const maxLine = 200
	// 优先处理进行中的待办事项。
	ordered := append([]methods.TodoItemDTO(nil), items...)
	sortTodoItemsOpenFirst(ordered)
	lines := make([]string, 0, len(ordered))
	kept := make([]methods.TodoItemDTO, 0, len(ordered))
	header := todoContextHeader
	total := len(header)
	for _, item := range ordered {
		key := item.ClientKey
		if key == "" {
			key = item.ID
		}
		line := fmt.Sprintf("- [%s] %s (%s)", item.Status, item.Content, key)
		runes := []rune(line)
		if len(runes) > maxLine {
			line = string(runes[:maxLine-1]) + "…"
		}
		if total+len(line)+1 > limit {
			break
		}
		lines = append(lines, line)
		kept = append(kept, item)
		total += len(line) + 1
	}
	if len(lines) == 0 {
		return nil
	}
	return &methods.TodoContext{
		Items:   kept,
		Context: header + strings.Join(lines, "\n"),
	}
}

func sortTodoItemsOpenFirst(items []methods.TodoItemDTO) {
	rank := func(status string) int {
		switch status {
		case "in_progress":
			return 0
		case "pending":
			return 1
		case "completed":
			return 2
		case "cancelled":
			return 3
		default:
			return 9
		}
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if rank(items[j].Status) < rank(items[i].Status) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

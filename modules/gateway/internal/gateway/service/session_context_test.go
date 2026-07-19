package service

import (
	"encoding/json"
	"strings"
	"testing"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/methods"
)

func TestSelectModelContextMessagesKeepsNewestWithinBudget(t *testing.T) {
	t.Parallel()
	msgs := make([]model.Message, 0, 20)
	for i := 0; i < 20; i++ {
		text := strings.Repeat("w", 40) // ~10 tokens body + 4 overhead
		msgs = append(msgs, model.Message{
			ID:          string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Seq:         uint64(i + 1),
			Role:        "user",
			ContentJSON: mustContentJSON(t, text),
		})
	}
	selected := selectModelContextMessages(msgs, 0, 50)
	if len(selected) == 0 {
		t.Fatal("expected some messages")
	}
	if selected[len(selected)-1].Seq != 20 {
		t.Fatalf("newest seq = %d, want 20", selected[len(selected)-1].Seq)
	}
	for i := 1; i < len(selected); i++ {
		if selected[i].Seq <= selected[i-1].Seq {
			t.Fatalf("not chronological: %#v", selected)
		}
	}
	if len(selected) >= len(msgs) {
		t.Fatalf("expected trim, got all %d", len(selected))
	}
}

func TestSelectModelContextMessagesAlwaysKeepsNewest(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("汉", 5000)
	msgs := []model.Message{
		{Seq: 1, Role: "user", ContentJSON: mustContentJSON(t, "old")},
		{Seq: 2, Role: "user", ContentJSON: mustContentJSON(t, huge)},
	}
	selected := selectModelContextMessages(msgs, 0, 100)
	if len(selected) != 1 || selected[0].Seq != 2 {
		t.Fatalf("selected = %#v, want only newest huge message", selected)
	}
}

func TestSelectModelContextMessagesReservesSummaryBudget(t *testing.T) {
	t.Parallel()
	msgs := []model.Message{
		{Seq: 1, Role: "user", ContentJSON: mustContentJSON(t, strings.Repeat("a", 80))},
		{Seq: 2, Role: "user", ContentJSON: mustContentJSON(t, strings.Repeat("b", 80))},
		{Seq: 3, Role: "user", ContentJSON: mustContentJSON(t, strings.Repeat("c", 80))},
	}
	without := selectModelContextMessages(msgs, 0, 80)
	withReserve := selectModelContextMessages(msgs, 60, 80)
	if len(withReserve) > len(without) {
		t.Fatalf("reserve should not increase selection: without=%d with=%d", len(without), len(withReserve))
	}
	if len(withReserve) == 0 || withReserve[len(withReserve)-1].Seq != 3 {
		t.Fatalf("must keep newest under reserve: %#v", withReserve)
	}
}

func mustContentJSON(t *testing.T, text string) string {
	t.Helper()
	raw, err := json.Marshal([]methods.ContentBlock{{Type: "text", Text: text}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

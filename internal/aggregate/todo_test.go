package aggregate

import (
	"strings"
	"testing"
)

func TestTodoItemsReadBoxesPriorityAndStale(t *testing.T) {
	md := "# Plan\n- [ ] [P1] Fix the proxy\n- [x] already done\n## P0 urgent\n- [ ] Rotate the key\n- [ ] Old thing ✅ done\n## Later\n- [ ] Nice to have\n- [ ] Nice to have\n"
	items, note := todoItems(md, "home", "TODO.md")
	if len(items) != 4 {
		t.Fatalf("want 4 open items, got %d: %+v", len(items), items)
	}
	want := map[string]string{"Fix the proxy": "P1", "Rotate the key": "P0", "Nice to have": ""}
	ids := map[string]bool{}
	for _, it := range items {
		text := strings.TrimSpace(strings.TrimPrefix(it.Title, "[P1]"))
		if p, ok := want[text]; ok && p != it.Priority {
			t.Errorf("%q priority = %q, want %q", it.Title, it.Priority, p)
		}
		if it.Kind != KindTodo || it.Status != "open" || ids[it.ID] {
			t.Errorf("bad item %+v (kind, status or duplicate id)", it)
		}
		ids[it.ID] = true
	}
	if !strings.Contains(note, "1 open box(es) say they are done") {
		t.Errorf("note = %q, want the stale count", note)
	}
}

func TestTodoIDsSurviveMovedLines(t *testing.T) {
	a, _ := todoItems("- [ ] one\n- [ ] two\n", "r", "TODO.md")
	b, _ := todoItems("- [ ] intro\n- [ ] two\n- [ ] one\n", "r", "TODO.md")
	byTitle := map[string]string{}
	for _, it := range a {
		byTitle[it.Title] = it.ID
	}
	for _, it := range b {
		if id, ok := byTitle[it.Title]; ok && id != it.ID {
			t.Errorf("%q changed id when lines moved: %s -> %s", it.Title, id, it.ID)
		}
	}
}

func TestPrioLabel(t *testing.T) {
	two, nine := 2, 9
	if prioLabel(&two) != "P2" || prioLabel(nil) != "" || prioLabel(&nine) != "" {
		t.Error("prioLabel wrong")
	}
}

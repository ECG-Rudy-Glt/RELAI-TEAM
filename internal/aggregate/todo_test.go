package aggregate

import (
	"os"
	"path/filepath"
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

func TestPriorityNeedsAMarkNotAWordInASentence(t *testing.T) {
	md := "- [ ] see page P1 of the docs\n- [ ] [P2] bracketed\n- [ ] (P3) parenthesised\n- [ ] P0: leading\n- [ ] P4 - leading dash\n## Backlog P3\n- [ ] under a heading\n"
	items, _ := todoItems(md, "r", "TODO.md")
	got := map[string]string{}
	for _, it := range items {
		got[it.Title] = it.Priority
	}
	want := map[string]string{
		"see page P1 of the docs": "", "[P2] bracketed": "P2", "(P3) parenthesised": "P3",
		"P0: leading": "P0", "P4 - leading dash": "P4", "under a heading": "P3",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%q priority = %q, want %q", k, got[k], v)
		}
	}
}

func TestChecklistsInDocFoldersAreFoundNotLinks(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "Documentation"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "Documentation", "TODO.md"), []byte("- [ ] x\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "tasks.md"), []byte("- [ ] y\n"), 0o644))
	secret := filepath.Join(t.TempDir(), "secret.md")
	must(os.WriteFile(secret, []byte("- [ ] leaked\n"), 0o644))
	must(os.Symlink(secret, filepath.Join(dir, "TODO.md")))
	got := findTodoFiles(dir)
	if len(got) != 2 {
		t.Fatalf("want Documentation/TODO.md and tasks.md only (no symlink), got %v", got)
	}
	a, _ := todoItems("- [ ] same\n", "r", "TODO.md")
	b, _ := todoItems("- [ ] same\n", "r", "Documentation/TODO.md")
	if a[0].ID == b[0].ID {
		t.Error("the same sentence in two files must be two items")
	}
}

package aggregate

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// todoFiles are the plain checklists some projects keep next to (or instead of) a tracker. They are looked for
// at the root and in the folders where projects usually keep documents; the name is matched without case.
var todoFiles = []string{"todo.md", "tasks.md"}
var todoDirs = []string{"", "docs", "doc", "documentation"}

// findTodoFiles returns the paths (relative to dir, slash-separated) of the checklists of a repository. Only
// regular files count: a symbolic link is never followed, so a link to a file outside the repository is not read.
func findTodoFiles(dir string) []string {
	var out []string
	scan := func(rel string) {
		entries, err := os.ReadDir(filepath.Join(dir, rel))
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.Type().IsRegular() {
				continue
			}
			for _, n := range todoFiles {
				if strings.EqualFold(e.Name(), n) {
					out = append(out, path.Join(rel, e.Name()))
				}
			}
		}
	}
	scan("")
	root, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range root { // a real folder (not a link) named docs, doc or documentation, in any case
		if !e.IsDir() {
			continue
		}
		for _, d := range todoDirs[1:] {
			if strings.EqualFold(e.Name(), d) {
				scan(e.Name())
			}
		}
	}
	return out
}

// maxTodoItems bounds what one file can add to the board: a checklist of thousands of lines is a
// notebook, not a task list.
const maxTodoItems = 300

var (
	todoBox = regexp.MustCompile(`^[-*]\s+\[ \]\s+(.+)$`)
	// A priority on a line is "[P1]" or "(P1)" anywhere, or "P1:" / "P1 -" at the start. A bare "P1" inside a
	// sentence ("see page P1 of the docs") is not a priority.
	todoPrio = regexp.MustCompile(`\[P([0-4])\]|\(P([0-4])\)|^P([0-4])\s*[:\-–—]`)
	// A heading may carry a bare P-number: "## P0 urgent", "## Backlog P3".
	headPrio = regexp.MustCompile(`\bP([0-4])\b`)
	// a box that still says it is finished: the file is out of date, not the work
	todoDone = regexp.MustCompile(`(?i)(✅|✔|\bdone\b|\bfait\b|\bterminé\b|\bfini\b)`)
)

// prioLabel turns Beads' number (0 = most urgent) into "P0".."P4"; anything else is "unknown".
func prioLabel(p *int) string {
	if p == nil || *p < 0 || *p > 4 {
		return ""
	}
	return fmt.Sprintf("P%d", *p)
}

// todoItems reads the unchecked boxes of a checklist. The priority comes from a P0..P4 tag on the line, or
// from the nearest heading above that carries one. Boxes that still say "done" are not items: they are
// counted in the note, since a list that contradicts itself is worth knowing about. IDs are a hash of the
// text so they survive lines being added or moved.
func todoItems(md, repo, file string) ([]Item, string) {
	var items []Item
	stale, over := 0, 0
	heading := ""
	seen := map[string]int{}
	for _, l := range strings.Split(md, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			heading = ""
			if m := headPrio.FindStringSubmatch(t); m != nil {
				heading = "P" + m[1]
			}
			continue
		}
		m := todoBox.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		text := strings.TrimSpace(m[1])
		if todoDone.MatchString(text) {
			stale++
			continue
		}
		if len(items) >= maxTodoItems {
			over++
			continue
		}
		prio := heading
		if p := todoPrio.FindStringSubmatch(text); p != nil {
			for _, g := range p[1:] {
				if g != "" {
					prio = "P" + g
				}
			}
		}
		sum := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ToLower(text))))[:8]
		seen[sum]++
		if seen[sum] > 1 {
			sum = fmt.Sprintf("%s-%d", sum, seen[sum])
		}
		if len(text) > 160 {
			text = text[:160] + "…"
		}
		items = append(items, Item{ID: repo + "/todo/" + idPrefix(file) + sum, Repo: repo, Kind: KindTodo, Title: text, Status: "open", Priority: prio})
	}
	var notes []string
	if stale > 0 {
		notes = append(notes, fmt.Sprintf("%d open box(es) say they are done (stale)", stale))
	}
	if over > 0 {
		notes = append(notes, fmt.Sprintf("%d more open box(es) not shown (limit %d)", over, maxTodoItems))
	}
	if len(notes) == 0 {
		return items, ""
	}
	return items, file + ": " + strings.Join(notes, "; ")
}

// idPrefix keeps the ids of the root TODO.md as they were, and tells the other checklists apart so the same
// sentence in two files does not become one item.
func idPrefix(file string) string {
	f := strings.ToLower(file)
	if f == "todo.md" {
		return ""
	}
	f = strings.TrimSuffix(f, ".md")
	return strings.NewReplacer("/", "-", " ", "-").Replace(f) + "-"
}

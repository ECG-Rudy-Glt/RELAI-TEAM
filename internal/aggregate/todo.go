package aggregate

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

// todoFiles are the plain checklists some projects keep next to (or instead of) a tracker.
var todoFiles = []string{"TODO.md", "TASKS.md"}

// maxTodoItems bounds what one file can add to the board: a checklist of thousands of lines is a
// notebook, not a task list.
const maxTodoItems = 300

var (
	todoBox  = regexp.MustCompile(`^[-*]\s+\[ \]\s+(.+)$`)
	todoPrio = regexp.MustCompile(`(?i)(?:\[|\(|\b)P([0-4])(?:\]|\)|\b)`)
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
			if m := todoPrio.FindStringSubmatch(t); m != nil {
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
			prio = "P" + p[1]
		}
		sum := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ToLower(text))))[:8]
		seen[sum]++
		if seen[sum] > 1 {
			sum = fmt.Sprintf("%s-%d", sum, seen[sum])
		}
		if len(text) > 160 {
			text = text[:160] + "…"
		}
		items = append(items, Item{ID: repo + "/todo/" + sum, Repo: repo, Kind: KindTodo, Title: text, Status: "open", Priority: prio})
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

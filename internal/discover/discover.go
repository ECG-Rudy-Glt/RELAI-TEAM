// Package discover finds the git repositories of a folder and the mentions between repositories, so a team
// board can be fed from "the folder where our projects live" or "the volume mounted at /repos" instead of a
// hand-written list. Read-only and deterministic. A mention is evidence, not a verdict: this package never
// decides that two repositories depend on each other, it reports what it saw.
package discover

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var skipDirs = map[string]bool{"node_modules": true, "vendor": true, "graphify-out": true, "dist": true, "build": true, "target": true, "__pycache__": true}

// Find returns the git repositories inside folder, looking depth levels down (1 = directly inside). It follows
// symbolic links to repositories (people link projects from elsewhere into one folder), lists a repository
// reached twice once, and never looks inside a repository or a hidden or generated folder.
func Find(folder string, depth int) ([]string, error) {
	if depth < 1 {
		depth = 1
	}
	abs, err := filepath.Abs(folder)
	if err != nil {
		return nil, err
	}
	if _, err := os.ReadDir(abs); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	mark := func(p string) bool {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			if seen[real] {
				return false
			}
			seen[real] = true
		}
		return true
	}
	var out []string
	var walk func(d string, level int)
	walk = func(d string, level int) {
		entries, err := os.ReadDir(d)
		if err != nil {
			return
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") || skipDirs[e.Name()] {
				continue
			}
			p := filepath.Join(d, e.Name())
			if st, err := os.Stat(p); err != nil || !st.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
				if mark(p) {
					out = append(out, p)
				}
				continue
			}
			if level < depth && mark(p) {
				walk(p, level+1)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
		return []string{abs}, nil
	}
	walk(abs, 1)
	out = mergeWorktrees(out)
	sort.Strings(out)
	return out, nil
}

// mergeWorktrees keeps one path per repository: working copies sharing a git directory (git worktrees)
// are one project. The main working copy (its .git is a folder) wins; with only linked copies, the first
// path in name order stays.
func mergeWorktrees(paths []string) []string {
	group := map[string][]string{}
	var order []string
	for _, p := range paths {
		key := gitOut(p, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if key == "" {
			key = p
		}
		if _, ok := group[key]; !ok {
			order = append(order, key)
		}
		group[key] = append(group[key], p)
	}
	var kept []string
	for _, k := range order {
		ps := group[k]
		sort.Strings(ps)
		main := ps[0]
		for _, p := range ps {
			if st, err := os.Stat(filepath.Join(p, ".git")); err == nil && st.IsDir() {
				main = p
				break
			}
		}
		kept = append(kept, main)
	}
	return kept
}

// Repo is a repository to look for mentions in and of.
type Repo struct {
	ID   string // the name other repositories are known by on the board
	Path string
}

// Link is what repository From says about repository To: strong evidence is To's remote slug or a relative
// path to it, weak is its name as a word. WeakConfig counts the weak files that are CI, deploy or config.
type Link struct {
	From       string   `json:"from"`
	To         string   `json:"to"`
	Strong     int      `json:"strong_files"`
	Weak       int      `json:"weak_files"`
	WeakConfig int      `json:"weak_config_files"`
	Kinds      []string `json:"kinds,omitempty"`
	Evidence   []string `json:"evidence,omitempty"` // up to three paths in From, never content
}

const minNameLength = 4

var excluded = []string{":!graphify-out", ":!node_modules", ":!vendor", ":!*.lock", ":!package-lock.json", ":!go.sum", ":!*.min.js", ":!*.svg", ":!*_test.go", ":!testdata", ":!*.test.*", ":!*.spec.*"}

// Mentions returns the mentions between the repositories, strongest first.
func Mentions(repos []Repo) []Link {
	slugs := map[string]string{}
	for _, r := range repos {
		slugs[r.ID] = slugOf(gitOut(r.Path, "remote", "get-url", "origin"))
	}
	var out []Link
	for _, a := range repos {
		for _, b := range repos {
			if a.Path == b.Path || a.ID == b.ID {
				continue
			}
			// Two clones of the same remote are one project, not a dependency.
			if slugs[a.ID] != "" && slugs[a.ID] == slugs[b.ID] {
				continue
			}
			name := filepath.Base(b.Path)
			l := Link{From: a.ID, To: b.ID}
			strong := map[string]bool{}
			if s := slugs[b.ID]; s != "" {
				for _, f := range grep(a.Path, s, false, false) {
					strong[f] = true
					l.addKind("remote URL")
				}
			}
			for _, f := range grep(a.Path, `\.\./`+regexp.QuoteMeta(name)+`(/|"|'|\s|$)`, false, true) {
				strong[f] = true
				l.addKind("relative path")
			}
			l.Strong = len(strong)
			weak := map[string]bool{}
			if len(name) >= minNameLength {
				for _, f := range grep(a.Path, name, true, false) {
					weak[f] = true
					if configLike(f) {
						l.WeakConfig++
					}
				}
			}
			l.Weak = len(weak)
			if l.Strong == 0 && l.Weak == 0 {
				continue
			}
			ev := keys(strong)
			if len(ev) == 0 {
				ev = keys(weak)
			}
			if len(ev) > 3 {
				ev = ev[:3]
			}
			l.Evidence = ev
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Strong != out[j].Strong {
			return out[i].Strong > out[j].Strong
		}
		if out[i].WeakConfig != out[j].WeakConfig {
			return out[i].WeakConfig > out[j].WeakConfig
		}
		if out[i].Weak != out[j].Weak {
			return out[i].Weak > out[j].Weak
		}
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (l *Link) addKind(k string) {
	for _, x := range l.Kinds {
		if x == k {
			return
		}
	}
	l.Kinds = append(l.Kinds, k)
}

var configRE = regexp.MustCompile(`(?i)(^|/)(\.github|\.forgejo|\.gitea|\.gitlab|\.circleci|k8s|kubernetes|deploy|ansible|terraform|helm|charts|infra)/|(^|/)(docker-compose[^/]*|dockerfile[^/]*|makefile|justfile|\.gitlab-ci\.yml)$|\.(tf|hcl|toml)$`)

func configLike(path string) bool {
	if strings.HasPrefix(path, ".beads/") || strings.HasPrefix(path, "openspec/") || strings.HasSuffix(strings.ToLower(path), ".md") {
		return false
	}
	return configRE.MatchString(path) || strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml")
}

var slugRE = regexp.MustCompile(`[:/]([A-Za-z0-9._-]+/[A-Za-z0-9._-]+?)(?:\.git)?/?$`)

func slugOf(url string) string {
	if m := slugRE.FindStringSubmatch(url); m != nil {
		return m[1]
	}
	return ""
}

func gitOut(dir string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", dir, "--no-optional-locks"}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func grep(dir, pattern string, word, regex bool) []string {
	args := []string{"-C", dir, "--no-optional-locks", "grep", "-l", "-I"}
	if regex {
		args = append(args, "-E")
	} else {
		args = append(args, "-i", "-F")
		if word {
			args = append(args, "-w")
		}
	}
	args = append(args, "-e", pattern, "--", ".")
	args = append(args, excluded...)
	cmd := exec.Command("git", args...)
	done := make(chan []byte, 1)
	go func() { out, _ := cmd.Output(); done <- out }()
	select {
	case out := <-done:
		var files []string
		for _, l := range strings.Split(string(out), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				files = append(files, l)
			}
		}
		return files
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		return nil
	}
}

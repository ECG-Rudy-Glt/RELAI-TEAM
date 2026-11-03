package aggregate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type cacheEntry struct {
	fp   string
	at   time.Time
	repo Repo
}

func (a *Aggregator) fullRefresh() time.Duration {
	if a.FullRefresh > 0 {
		return a.FullRefresh
	}
	return DefaultFullRefresh
}

func (a *Aggregator) cached(src, fp string) (Repo, bool) {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	e, ok := a.cache[src]
	if !ok || e.fp != fp || time.Since(e.at) >= a.fullRefresh() {
		return Repo{}, false
	}
	return e.repo, true
}

func (a *Aggregator) store(src, fp string, r Repo) {
	a.cmu.Lock()
	defer a.cmu.Unlock()
	if a.cache == nil {
		a.cache = map[string]cacheEntry{}
	}
	a.cache[src] = cacheEntry{fp: fp, at: time.Now(), repo: r}
}

// fingerprint summarises the inputs of a repo's items without running bd or openspec. ok is false
// when it cannot be computed (the repo is read every time, and an unreadable repo is reported as
// such, not cached).
func (a *Aggregator) fingerprint(src string) (string, bool) {
	if cloneURL.MatchString(src) {
		// ls-remote talks to the remote and writes nothing to disk, unlike a clone.
		out, err := a.Run("", "git", "ls-remote", src, "HEAD")
		if err != nil || strings.TrimSpace(string(out)) == "" {
			return "", false
		}
		return "remote:" + strings.TrimSpace(string(out)), true
	}
	return localFingerprint(src)
}

// localFingerprint hashes the size and modification time of what the items are read from:
// relai.yaml, Beads' .beads/last-touched, and everything under openspec/changes.
//
// .beads/last-touched is what makes this work: Beads rewrites it on every write (create, update,
// close, show) and never on a read, whereas the Dolt files next to it change on every single run,
// reads included, so they cannot be used as a signal. If a Beads version does not write it, the
// fingerprint stays constant and the repo is only re-read every FullRefresh.
func localFingerprint(dir string) (string, bool) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", false
	}
	h := sha256.New()
	stat := func(path string) {
		if fi, err := os.Stat(path); err == nil {
			fmt.Fprintf(h, "%s|%d|%d|%v\n", path, fi.Size(), fi.ModTime().UnixNano(), fi.IsDir())
		} else {
			fmt.Fprintf(h, "%s|absent\n", path)
		}
	}
	stat(filepath.Join(dir, "relai.yaml"))
	stat(filepath.Join(dir, ".beads"))
	stat(filepath.Join(dir, ".beads", "last-touched"))
	stat(filepath.Join(dir, "openspec"))
	changes := filepath.Join(dir, "openspec", "changes")
	var files []string
	_ = filepath.WalkDir(changes, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == "archive" {
			return filepath.SkipDir
		}
		files = append(files, path)
		return nil
	})
	sort.Strings(files)
	for _, f := range files {
		stat(f)
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

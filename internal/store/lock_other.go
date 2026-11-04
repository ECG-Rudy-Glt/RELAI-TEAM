//go:build !unix

package store

import "os"

// lockJournal is a no-op where flock is not available: the single-instance rule is then only
// documented, not enforced.
func lockJournal(path string) (*os.File, error) { return nil, nil }

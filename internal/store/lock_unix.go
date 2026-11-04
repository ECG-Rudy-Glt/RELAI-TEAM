//go:build unix

package store

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockJournal takes an exclusive, non-blocking lock on path+".lock" and returns the open lock
// file; closing it (or the process exiting, even by a crash) releases the lock. A second server
// on the same journal gets a clear error instead of silently running alongside the first.
func lockJournal(path string) (*os.File, error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%s is already in use by another relai-server (two servers on one journal would give the same item to two people); stop the other one or use a different data_dir", path)
		}
		return nil, err
	}
	return f, nil
}

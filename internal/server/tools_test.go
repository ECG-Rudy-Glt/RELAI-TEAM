package server

import (
	"errors"
	"strings"
	"testing"
)

func TestMissingTools(t *testing.T) {
	has := func(have ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, h := range have {
				if h == n {
					return "/bin/" + n, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	if got := MissingTools(has("git", "bd", "openspec")); len(got) != 0 {
		t.Errorf("everything present, got warnings: %v", got)
	}
	got := strings.Join(MissingTools(has("git")), "\n")
	for _, want := range []string{"bd is not on PATH: Beads tasks will not be shown", "openspec is not on PATH: OpenSpec changes will not be shown"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing warning %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "git is not") {
		t.Errorf("git is present but was reported: %s", got)
	}
	if got := MissingTools(has()); len(got) != 3 || !strings.Contains(got[0], "no repo can be read") {
		t.Errorf("nothing present: %v", got)
	}
}

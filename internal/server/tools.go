package server

import "os/exec"

// MissingTools lists the command-line tools the server reads repos through that are not on PATH,
// each with what the board loses without it. lookPath is exec.LookPath, injectable for tests.
//
// A missing tool does not stop the server: the repo is still listed, with a note. But the board then
// looks empty, which is confusing, so the server says so at startup and the web board shows the notes.
func MissingTools(lookPath func(string) (string, error)) []string {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	var out []string
	for _, t := range []struct{ name, loses string }{
		{"git", "no repo can be read"},
		{"bd", "Beads tasks will not be shown"},
		{"openspec", "OpenSpec changes will not be shown"},
	} {
		if _, err := lookPath(t.name); err != nil {
			out = append(out, t.name+" is not on PATH: "+t.loses)
		}
	}
	return out
}

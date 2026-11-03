package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/initcfg"
)

const initUsage = `relai-server init — create a ready-to-run configuration

  relai-server init --scan ~/work                  track every repo in ~/work that has Beads, OpenSpec or a relai.yaml
  relai-server init --repos /a/svc,/b/web          track these repos (paths or clone URLs)

Options:
  --members alice,agent-1,agent-2   who gets a token (default: you, agent-1, agent-2); the first is the administrator
  --dir <folder>                    where to write config.json and keep the event log (default: .)
  --listen <addr>                   default :8080
  --force                           overwrite an existing config.json (new tokens!)
`

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func runInit(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	scan := fs.String("scan", "", "")
	repos := fs.String("repos", "", "")
	members := fs.String("members", "", "")
	dir := fs.String("dir", ".", "")
	listen := fs.String("listen", ":8080", "")
	force := fs.Bool("force", false, "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%v\n\n%s", err, initUsage)
	}
	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(absDir, "config.json")
	if _, err := os.Stat(cfgPath); err == nil && !*force {
		return fmt.Errorf("%s already exists: it holds your members' token hashes, so it is not overwritten (use --force to replace it and issue new tokens)", cfgPath)
	}

	var rs []string
	if *scan != "" {
		found, err := initcfg.Scan(*scan)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			return fmt.Errorf("no git repo with Beads, OpenSpec or a relai.yaml found in %s", *scan)
		}
		rs = append(rs, found...)
	}
	for _, r := range strings.Split(*repos, ",") {
		if r = strings.TrimSpace(r); r != "" {
			if !strings.Contains(r, "://") && !strings.HasPrefix(r, "git@") {
				if r, err = filepath.Abs(r); err != nil {
					return err
				}
			}
			rs = append(rs, r)
		}
	}
	var ms []string
	if *members == "" {
		me := unsafeName.ReplaceAllString(os.Getenv("USER"), "")
		if me == "" {
			me = "me"
		}
		ms = []string{me, "agent-1", "agent-2"}
	} else {
		for _, m := range strings.Split(*members, ",") {
			ms = append(ms, strings.TrimSpace(m))
		}
	}

	dataDir := filepath.Join(absDir, "data")
	res, err := initcfg.Generate(initcfg.Options{Repos: rs, Members: ms, Listen: *listen, DataDir: dataDir})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(cfgPath, res.JSON, 0o600); err != nil {
		return err
	}

	fmt.Fprintf(out, "Wrote %s (%d repos, %d members)\n\n", cfgPath, len(rs), len(res.Members))
	for _, r := range rs {
		fmt.Fprintf(out, "  repo    %s\n", r)
	}
	fmt.Fprintln(out, "\nTokens. Shown once: the config keeps only their hashes. Give each person theirs.")
	for _, m := range res.Members {
		role := ""
		if m.Admin {
			role = " (administrator)"
		}
		fmt.Fprintf(out, "  %-16s %s%s\n", m.Identity, m.Token, role)
	}
	host := *listen
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	}
	fmt.Fprintf(out, "\nNext\n  1. relai-server -config %s\n  2. open http://%s/board and paste a token\n", cfgPath, host)
	fmt.Fprintf(out, "  3. for an agent:  claude mcp add relai-team -e RELAI_TEAM_URL=http://%s -e RELAI_TEAM_TOKEN=%s -- relai-mcp\n", host, res.Members[len(res.Members)-1].Token)
	var missing []string
	for _, tool := range []string{"git", "bd", "openspec"} {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(out, "\nNote: %s not found on PATH. The server reads repos through them (Beads tasks need bd, OpenSpec changes need openspec); the Docker image bundles all three.\n", strings.Join(missing, ", "))
	}
	if strings.HasPrefix(*listen, ":") || strings.HasPrefix(*listen, "0.0.0.0") {
		fmt.Fprintln(out, "\nNote: it listens on every network interface over plain HTTP. For anything beyond your own machine add tls_cert and tls_key (see README, \"HTTPS\").")
	}
	return nil
}

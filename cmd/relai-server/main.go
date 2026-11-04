// Command relai-server runs a team's live board: it aggregates the Relai contract of a declared
// set of repos and serves it over HTTP, with claim/release of work items.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/aggregate"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/board"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/initcfg"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/server"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/store"
)

var version = "dev" // set at build time: -ldflags "-X main.version=v0.1.0"

const usage = `relai-server — a live board for a team working across Relai repos

Usage:
  relai-server init [--scan <folder> | --repos a,b]   create config.json with member tokens (see "init -h")
  relai-server [-config <path>]     run the server (default config: $RELAI_TEAM_CONFIG or config.json)
  relai-server [-config <path>] -standby   wait for the active server to stop or die, then take over (same data_dir)
  relai-server token -config <path> -identity <name> [-drop-old]
                                    give a member a new token (shown once); the old one keeps working until
                                    you run it again with -drop-old; then reload with: kill -HUP <pid>
  relai-server hash-token <token>   print the SHA-256 to put in a member's "token_sha256"
  relai-server version
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Println("relai-server", version)
			return
		case "hash-token":
			if len(os.Args) != 3 {
				fmt.Fprint(os.Stderr, usage)
				os.Exit(2)
			}
			sum := sha256.Sum256([]byte(os.Args[2]))
			fmt.Println(hex.EncodeToString(sum[:]))
			return
		case "token":
			fs := flag.NewFlagSet("token", flag.ExitOnError)
			cfgPath := fs.String("config", os.Getenv("RELAI_TEAM_CONFIG"), "path to the config file")
			who := fs.String("identity", "", "the member to give a new token")
			drop := fs.Bool("drop-old", false, "remove the member's old tokens at once")
			_ = fs.Parse(os.Args[2:])
			if *cfgPath == "" {
				*cfgPath = "config.json"
			}
			if *who == "" {
				fmt.Fprintln(os.Stderr, "relai-server token: -identity is required")
				os.Exit(2)
			}
			tok, err := initcfg.Rotate(*cfgPath, *who, *drop, nil)
			if err != nil {
				fmt.Fprintln(os.Stderr, "relai-server token:", err)
				os.Exit(1)
			}
			fmt.Printf("New token for %s (shown once, not stored):\n\n  %s\n\n", *who, tok)
			if *drop {
				fmt.Println("The old tokens were removed from the config. Apply it: kill -HUP <relai-server pid> (or restart).")
			} else {
				fmt.Println("The old token still works. Give this one out, then run this command again with -drop-old,")
				fmt.Println("and apply each change with: kill -HUP <relai-server pid> (or restart). Claims are kept.")
			}
			return
		case "init":
			if len(os.Args) > 2 && (os.Args[2] == "-h" || os.Args[2] == "--help") {
				fmt.Print(initUsage)
				return
			}
			if err := runInit(os.Args[2:], os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "relai-server init:", err)
				os.Exit(1)
			}
			return
		case "help", "-h", "--help":
			fmt.Print(usage)
			return
		}
	}
	def := os.Getenv("RELAI_TEAM_CONFIG")
	if def == "" {
		def = "config.json"
	}
	fs := flag.NewFlagSet("relai-server", flag.ExitOnError)
	cfgPath := fs.String("config", def, "path to the config file")
	standby := fs.Bool("standby", false, "wait for the server that holds the journal to stop or die, then take over")
	_ = fs.Parse(os.Args[1:])

	if err := run(*cfgPath, *standby); err != nil {
		log.Fatalf("relai-server: %v", err)
	}
}

func run(cfgPath string, standby bool) error {
	cfg, err := server.LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	interval, _ := cfg.Interval()
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(cfg.DataDir, "events.jsonl")
	// Open first: it repairs a damaged journal, so the replay below sees a clean one.
	var events *store.Log
	if standby {
		log.Printf("standby: waiting for the server that holds %s to stop or die; this process serves nothing until then", logPath)
		events, err = store.OpenWait(logPath)
		if err == nil {
			log.Printf("standby: the journal is free, taking over")
		}
	} else {
		events, err = store.Open(logPath)
	}
	if err != nil {
		return err
	}
	defer events.Close()
	past, err := store.Replay(logPath)
	if err != nil {
		return err
	}

	for _, w := range server.MissingTools(nil) {
		log.Printf("WARNING: %s", w)
	}
	agg := aggregate.New()
	if agg.Git, err = cfg.GitAuth(); err != nil {
		return err
	}
	agg.Workers = cfg.RefreshConcurrency
	agg.FullRefresh = cfg.FullRefresh()
	agg.NoCache = cfg.FullRefresh() == 0
	if f := cfg.FullRefresh(); f < time.Minute {
		log.Printf("WARNING: full_refresh_interval is %s: every repo is re-read at least that often, and each read of a Beads repo rewrites about 3 MiB on disk. Leave it at the default (%s) unless you are debugging.", f, aggregate.DefaultFullRefresh)
	}
	srv, err := server.New(cfg, board.New(events, past), agg)
	if err != nil {
		return err
	}
	// Listen first, read the repos after: with hundreds of repos the first read takes tens of
	// seconds, during which /healthz must answer (a liveness probe would kill the process).
	srv.SetLoading()
	go func() {
		if err := srv.Refresh(); err != nil {
			log.Printf("initial refresh: %v", err)
		}
		log.Printf("initial read done")
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				before, safetyBefore := agg.Reads(), agg.SafetyReads()
				if err := srv.Refresh(); err != nil {
					log.Printf("refresh: %v", err)
				}
				n, safety := agg.Reads()-before, agg.SafetyReads()-safetyBefore
				if safety > n { // a failed read counts as safety but not as a read
					safety = n
				}
				if changed := n - safety; changed > 0 { // quiet when nothing changed
					log.Printf("refresh: re-read %d repo(s) whose files changed; the rest were unchanged", changed)
				}
				if safety > 0 {
					log.Printf("refresh: safety re-read of %d repo(s) whose files had not changed (full_refresh_interval %s)", safety, cfg.FullRefresh())
				}
			}
		}
	}()

	hs := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second, // lifted per request for the /events stream
	}
	if cfg.UsesTLS() {
		hs.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else if server.ListensBeyondLoopback(cfg.Listen) {
		log.Printf("WARNING: listening on %s over plain HTTP: tokens travel in clear. Set tls_cert and tls_key, or put a TLS-terminating proxy in front.", cfg.Listen)
	}
	hs.RegisterOnShutdown(srv.CloseStreams)
	// SIGHUP re-reads the config and applies what can change while running: members, tokens and request
	// limits. A token can be rotated or revoked without a restart, and claims are kept.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			newCfg, err := server.LoadConfig(cfgPath)
			if err == nil {
				err = srv.Reload(newCfg)
			}
			if err != nil {
				log.Printf("reload refused, the running configuration is unchanged: %v", err)
				continue
			}
			log.Printf("reloaded members, tokens and request limits (repos, listen address, TLS and the rest need a restart)")
		}
	}()
	errc := make(chan error, 1)
	go func() {
		scheme := "http"
		if cfg.UsesTLS() {
			scheme = "https"
		}
		log.Printf("relai-server %s listening on %s://%s, %d listed repos + %d discover folder(s), refresh every %s, web board at /board", version, scheme, cfg.Listen, len(cfg.Repos), len(cfg.Discover), interval)
		if cfg.UsesTLS() {
			errc <- hs.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
			return
		}
		errc <- hs.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Print("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return hs.Shutdown(sctx)
	}
	return nil
}

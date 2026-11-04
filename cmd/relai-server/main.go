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
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/server"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/store"
)

var version = "dev" // set at build time: -ldflags "-X main.version=v0.1.0"

const usage = `relai-server — a live board for a team working across Relai repos

Usage:
  relai-server init [--scan <folder> | --repos a,b]   create config.json with member tokens (see "init -h")
  relai-server [-config <path>]     run the server (default config: $RELAI_TEAM_CONFIG or config.json)
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
	_ = fs.Parse(os.Args[1:])

	if err := run(*cfgPath); err != nil {
		log.Fatalf("relai-server: %v", err)
	}
}

func run(cfgPath string) error {
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
	events, err := store.Open(logPath)
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
	srv, err := server.New(cfg, board.New(events, past), agg)
	if err != nil {
		return err
	}
	if err := srv.Refresh(); err != nil {
		log.Printf("initial refresh: %v", err)
	}

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
				before := agg.Reads()
				if err := srv.Refresh(); err != nil {
					log.Printf("refresh: %v", err)
				}
				if n := agg.Reads() - before; n > 0 { // quiet when nothing changed
					log.Printf("refresh: re-read %d repo(s) whose files changed; the rest were unchanged", n)
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
	errc := make(chan error, 1)
	go func() {
		scheme := "http"
		if cfg.UsesTLS() {
			scheme = "https"
		}
		log.Printf("relai-server %s listening on %s://%s, %d repos, refresh every %s, web board at /board", version, scheme, cfg.Listen, len(cfg.Repos), interval)
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

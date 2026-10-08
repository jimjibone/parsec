// Command parsec serves the parsec project planner.
//
//	parsec [--config FILE] [--data DIR] [--addr HOST:PORT]
//	parsec --config FILE passwd USERNAME [ROLE]
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"parsec/internal/api"
	"parsec/internal/auth"
	"parsec/internal/config"
	"parsec/internal/gitrepo"
	"parsec/internal/history"
	"parsec/internal/store"
	"parsec/internal/webui"
)

func main() {
	configPath := flag.String("config", "", "path to a config file (see deploy/parsec.example.yaml)")
	dataDir := flag.String("data", "", "path to the data git repository (created if missing); overrides the config file")
	addr := flag.String("addr", "", "listen address (default 127.0.0.1:7343); overrides the config file")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage:\n  parsec [flags]\n  parsec --config FILE passwd USERNAME [ROLE]   set a local password (auth mode local)\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if *dataDir != "" {
		cfg.Data = *dataDir
	}
	if *addr != "" {
		cfg.Addr = *addr
	}
	if err := cfg.Finish(); err != nil {
		log.Fatalf("config: %v", err)
	}

	if flag.Arg(0) == "passwd" {
		if err := passwd(cfg, flag.Args()[1:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}

	repo, err := gitrepo.Open(cfg.Data)
	if err != nil {
		log.Fatalf("open data repo: %v", err)
	}
	st, err := store.Open(cfg.Data)
	if err != nil {
		log.Fatalf("load data: %v", err)
	}
	a, err := auth.New(cfg)
	if err != nil {
		log.Fatalf("auth: %v", err)
	}

	srv := api.New(cfg, a, st, repo, history.New(cfg.Data, 500), webui.FS())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	committerDone := make(chan struct{})
	go func() {
		srv.RunCommitter(ctx)
		close(committerDone)
	}()

	hs := &http.Server{Addr: cfg.Addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(sctx) // open event streams are cut off at the deadline
	}()

	log.Printf("parsec: data %s, auth %s, listening on http://%s", cfg.Data, cfg.Auth.Mode, cfg.Addr)
	if cfg.Shared() && !*cfg.Git.AutoCommit {
		log.Printf("parsec: automatic commits are off; an admin commits from the git panel")
	}
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-committerDone // pending changes are committed before exit
}

// passwd creates or updates a local account's password, reading it from the
// terminal (or one line of stdin when piped).
func passwd(cfg *config.Config, args []string) error {
	if cfg.Auth.Mode != config.ModeLocal {
		return errors.New("passwd needs auth.mode: local in the config file")
	}
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: parsec --config FILE passwd USERNAME [ROLE]")
	}
	a, err := auth.New(cfg)
	if err != nil {
		return err
	}
	pw, err := readPassword()
	if err != nil {
		return err
	}
	in := auth.Update{Username: args[0], Password: pw}
	if len(args) == 2 {
		in.Role = args[1]
	}
	if _, ok := a.Users.Get(in.Username); ok {
		_, err = a.Users.Update(in)
	} else {
		_, err = a.Users.Create(in)
	}
	if err == nil {
		fmt.Println("password set for", in.Username)
	}
	return err
}

func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, "Password: ")
	a, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Again: ")
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(a) != string(b) {
		return "", errors.New("passwords do not match")
	}
	return string(a), nil
}

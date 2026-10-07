// Command parsec serves the parsec project planner.
package main

import (
	"flag"
	"log"
	"net/http"
	"path/filepath"

	"parsec/internal/api"
	"parsec/internal/gitrepo"
	"parsec/internal/store"
	"parsec/internal/webui"
)

func main() {
	dataDir := flag.String("data", "parsec-data", "path to the data git repository (created if missing)")
	addr := flag.String("addr", "127.0.0.1:7343", "listen address")
	flag.Parse()

	dir, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	repo, err := gitrepo.Open(dir)
	if err != nil {
		log.Fatalf("open data repo: %v", err)
	}
	st, err := store.Open(dir)
	if err != nil {
		log.Fatalf("load data: %v", err)
	}

	srv := api.New(st, repo, webui.FS())
	log.Printf("parsec: data %s, listening on http://%s", dir, *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}

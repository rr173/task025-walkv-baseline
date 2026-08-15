// Command walkv runs a WAL-backed key-value HTTP service, or a self-check
// when invoked with --smoke-test.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"task025-walkv/internal/httpapi"
	"task025-walkv/internal/selfcheck"
	"task025-walkv/internal/walstore"
)

func main() {
	args := os.Args[1:]

	// --smoke-test short-circuits to the self-check, which exits on its own.
	for _, a := range args {
		if a == "--smoke-test" {
			if err := selfcheck.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "smoke-test failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("smoke-test passed")
			return
		}
	}

	// Server mode. An optional leading "server" subcommand is accepted but
	// not required, so both `walkv server --addr :8080` and `walkv --addr
	// :8080` work identically.
	rest := args
	if len(rest) > 0 && rest[0] == "server" {
		rest = rest[1:]
	}
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address")
	walPath := fs.String("wal", "data.wal", "WAL file path")
	if err := fs.Parse(rest); err != nil {
		os.Exit(2)
	}

	store, err := walstore.Open(*walPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	srv := httpapi.NewServer(store)
	log.Printf("task025-walkv listening on %s (wal=%s)", *addr, *walPath)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatalf("server: %v", err)
	}
}

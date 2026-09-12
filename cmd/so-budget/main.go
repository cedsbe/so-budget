package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cedsbe/so-budget/internal/config"
	"github.com/cedsbe/so-budget/internal/service"
	"github.com/cedsbe/so-budget/internal/simplefin"
	"github.com/cedsbe/so-budget/internal/store"
	"github.com/cedsbe/so-budget/internal/web"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o750); err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st, bankClient(cfg), service.Options{Pepper: []byte(cfg.Pepper), Location: cfg.Location})

	switch os.Args[1] {
	case "serve":
		srv, err := web.New(svc, web.Config{BaseURL: cfg.BaseURL, Dev: cfg.Dev, IdleTimeout: cfg.IdleTimeout, AbsoluteTimeout: cfg.AbsoluteTimeout})
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("listening on %s", cfg.Listen)
		log.Fatal(http.ListenAndServe(cfg.Listen, srv.Handler()))
	case "invite":
		if len(os.Args) != 3 {
			usage()
			os.Exit(2)
		}
		tok, err := svc.Invite(context.Background(), os.Args[2])
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s/invite/%s\n", cfg.BaseURL, tok)
	case "backup":
		if len(os.Args) != 3 {
			usage()
			os.Exit(2)
		}
		if err := backup(st, os.Args[2]); err != nil {
			log.Fatal(err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

// bankClient returns the real SimpleFIN client, or the local fake when SB_SIMPLEFIN_FAKE=1.
func bankClient(cfg config.Config) simplefin.Client {
	if os.Getenv("SB_SIMPLEFIN_FAKE") == "1" {
		f := simplefin.NewFake()
		f.Set(simplefin.SampleSet(time.Now().Unix()))
		log.Printf("using fake SimpleFIN at %s; setup token: %s", f.URL(), f.SetupToken())
		return simplefin.NewHTTPClient()
	}
	return simplefin.NewHTTPClient()
}

// backup is implemented in Task 17.
func backup(st *store.Store, dest string) error { _ = time.Now; return fmt.Errorf("backup not implemented") }

func usage() {
	fmt.Fprintln(os.Stderr, "usage: so-budget <serve|invite NAME|backup DEST>")
}

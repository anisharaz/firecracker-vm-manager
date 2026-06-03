package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/api"
	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/config"
	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/manager"
	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/store"
	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/store/jsonstore"
	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/store/mongostore"
)

func main() {
	cfgPath := flag.String("config", "", "path to YAML config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		log.Fatalf("mkdir state_dir: %v", err)
	}

	s, err := openStore(cfg)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer s.Close()

	mgr, err := manager.New(cfg, s)
	if err != nil {
		log.Fatalf("manager: %v", err)
	}

	go func() {
		if errs := mgr.Reconcile(context.Background()); len(errs) > 0 {
			for _, e := range errs {
				log.Printf("reconcile: %v", e)
			}
		}
	}()

	app := api.NewApp(api.NewServer(mgr))

	go func() {
		log.Printf("listening on %s", cfg.ListenAddr)
		if err := app.Listen(cfg.ListenAddr); err != nil {
			log.Fatalf("listen: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("shutting down …")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	mgr.ShutdownAll(shutdownCtx)
	log.Println("bye")
}

func openStore(cfg config.Config) (store.Store, error) {
	switch cfg.Store.Type {
	case "json":
		return jsonstore.Open(cfg.Store.JSON.Path)
	case "mongo":
		return mongostore.Open(context.Background(), cfg.Store.Mongo)
	default:
		return nil, &configError{cfg.Store.Type}
	}
}

type configError struct{ t string }

func (e *configError) Error() string { return "unknown store.type: " + e.t }

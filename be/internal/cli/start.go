package cli

import (
	"context"
	"fmt"
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

	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the manager daemon (HTTP API + reconcile loop)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			return runStart(cfgPath)
		},
	}
	return cmd
}

func runStart(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return fmt.Errorf("mkdir state_dir: %w", err)
	}

	s, err := openStore(cfg)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer s.Close()

	mgr, err := manager.New(cfg, s)
	if err != nil {
		return fmt.Errorf("manager: %w", err)
	}

	go func() {
		if errs := mgr.Reconcile(context.Background()); len(errs) > 0 {
			for _, e := range errs {
				log.Printf("reconcile: %v", e)
			}
		}
	}()

	app := api.NewApp(api.NewServer(mgr))

	listenErr := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", cfg.ListenAddr)
		listenErr <- app.Listen(cfg.ListenAddr)
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sig:
	case err := <-listenErr:
		return fmt.Errorf("listen: %w", err)
	}

	log.Println("shutting down …")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	mgr.ShutdownAll(shutdownCtx)
	log.Println("bye")
	return nil
}

func openStore(cfg config.Config) (store.Store, error) {
	switch cfg.Store.Type {
	case "json":
		return jsonstore.Open(cfg.Store.JSON.Path)
	case "mongo":
		return mongostore.Open(context.Background(), cfg.Store.Mongo)
	default:
		return nil, fmt.Errorf("unknown store.type: %s", cfg.Store.Type)
	}
}

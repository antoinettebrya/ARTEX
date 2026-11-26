// seoos is the isolated SEO service. It never starts the legacy ARTEX manager,
// tool registry, agent runtime, traffic proxy, or background workers.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/api"
	"github.com/Autumn-27/artex/seoos/internal/config"
	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/store"
	"github.com/Autumn-27/artex/seoos/internal/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Error("SEO OS stopped", "reason", err.Error())
		os.Exit(1)
	}
}

func run() error {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command == "help" || command == "-h" || command == "--help" {
		fmt.Println("Usage: seoos [serve|migrate|provision-org NAME OWNER_EMAIL]\nConfiguration: SEOOS_DATABASE_URL, SEOOS_ORGANIZATION_ID, SEOOS_API_TOKEN; optional SEOOS_ADDR, SEOOS_ORGANIZATION_NAME.")
		return nil
	}
	switch command {
	case "serve", "migrate":
		if len(os.Args) > 2 {
			return errors.New("usage: seoos [serve|migrate|provision-org NAME OWNER_EMAIL]")
		}
	case "provision-org":
		if len(os.Args) != 4 {
			return errors.New("usage: seoos provision-org NAME OWNER_EMAIL")
		}
	default:
		return errors.New("usage: seoos [serve|migrate|provision-org NAME OWNER_EMAIL]")
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	repo, err := store.Open(startup, cfg.DatabaseURL)
	if err != nil {
		return errors.New("cannot connect to SEO PostgreSQL; check SEOOS_DATABASE_URL and database availability")
	}
	defer repo.Close()
	if err := repo.SetProviderKey(cfg.ProviderKey); err != nil {
		return errors.New("cannot initialize provider credential encryption")
	}
	if err := repo.Migrate(startup); err != nil {
		return errors.New("SEO database migration failed; check the migration version, checksum and database permissions")
	}
	if command == "provision-org" {
		orgID, credential, err := repo.ProvisionOrganization(startup, os.Args[2], os.Args[3])
		if err != nil {
			var validation *domain.ValidationError
			if errors.As(err, &validation) {
				return validation
			}
			return errors.New("cannot provision the SEO organization")
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"organization_id": orgID, "credential": credential})
	}
	if err := repo.EnsureOrganization(startup, cfg.OrganizationID, cfg.OrganizationName); err != nil {
		return errors.New("cannot initialize the configured SEO organization")
	}
	if command == "migrate" {
		slog.Info("SEO database migrations applied")
		return nil
	}
	researchProvider, err := newResearchProvider()
	if err != nil {
		return err
	}
	content, err := newContentServices()
	if err != nil {
		return err
	}
	execution, err := newExecutionServices()
	if err != nil {
		return err
	}
	handler, err := api.NewHandler(repo, cfg.OrganizationID, cfg.APIToken, execution.Fixture)
	if err != nil {
		return errors.New("invalid SEO HTTP configuration")
	}
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	cancel()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Run(ctx, repo, researchProvider, content, execution)
	}()
	defer func() {
		stop()
		<-workerDone
	}()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	slog.Info("SEO API starting", "address", cfg.Address, "organization_id", cfg.OrganizationID)
	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("SEO HTTP server failed to listen or serve")
		}
		return nil
	case <-ctx.Done():
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return errors.New("SEO HTTP shutdown exceeded its deadline")
		}
		return nil
	}
}

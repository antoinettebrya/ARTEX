// seoos is the isolated SEO service. It never starts the legacy ARTEX manager,
// tool registry, agent runtime, traffic proxy, or background workers.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
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

const usage = "Usage: seoos [serve|api|worker|migrate|health|provision-org NAME OWNER_EMAIL]"

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
		fmt.Println(usage)
		fmt.Println("API and migration use SEOOS_DATABASE_URL, SEOOS_ORGANIZATION_ID and SEOOS_API_TOKEN. Workers use SEOOS_DATABASE_URL and SEOOS_PROVIDER_KEY.")
		return nil
	}
	switch command {
	case "serve", "api", "worker", "migrate", "health":
		if len(os.Args) > 2 {
			return errors.New(usage)
		}
	case "provision-org":
		if len(os.Args) != 4 {
			return errors.New(usage)
		}
	default:
		return errors.New(usage)
	}
	if command == "health" {
		return readinessProbe(os.Getenv("SEOOS_ADDR"))
	}
	load := config.Load
	if command == "worker" {
		load = config.LoadStorage
	}
	cfg, err := load(os.Getenv)
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
	if command == "api" || command == "worker" {
		if err := repo.CheckMigrations(startup); err != nil {
			return errors.New("SEO database schema is not ready; apply and verify migrations before starting runtime services")
		}
	} else {
		if err := repo.Migrate(startup); err != nil {
			return errors.New("SEO database migration failed; check the migration version, checksum and database permissions")
		}
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
	if command == "migrate" || command == "serve" {
		if err := repo.EnsureOrganization(startup, cfg.OrganizationID, cfg.OrganizationName); err != nil {
			return errors.New("cannot initialize the configured SEO organization")
		}
	}
	if command == "migrate" {
		slog.Info("SEO database migrations applied")
		return nil
	}
	execution, err := newExecutionServices()
	if err != nil {
		return err
	}
	cancel()
	if command == "worker" || command == "serve" {
		researchProvider, err := newResearchProvider()
		if err != nil {
			return err
		}
		content, err := newContentServices()
		if err != nil {
			return err
		}
		if command == "worker" {
			slog.Info("SEO worker starting")
			worker.Run(ctx, repo, researchProvider, content, execution)
			return nil
		}
		workerDone := make(chan struct{})
		go func() {
			defer close(workerDone)
			worker.Run(ctx, repo, researchProvider, content, execution)
		}()
		defer func() {
			stop()
			<-workerDone
		}()
	}
	return serveHTTP(ctx, repo, cfg, execution.Fixture)
}

func readinessProbe(address string) error {
	if address == "" {
		address = "127.0.0.1:8789"
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("invalid health check address")
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{}}
	response, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/readyz")
	if err != nil {
		return errors.New("SEO API readiness probe failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("SEO API is not ready")
	}
	return nil
}

func serveHTTP(ctx context.Context, repo *store.Store, cfg config.Config, executionFixture *http.Client) error {
	handler, err := api.NewHandler(repo, cfg.OrganizationID, cfg.APIToken, executionFixture)
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

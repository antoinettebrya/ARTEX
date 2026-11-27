// seoos is the isolated SEO service. It never starts the legacy ARTEX manager,
// tool registry, agent runtime, traffic proxy, or background workers.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/Autumn-27/artex/seoos/internal/api"
	"github.com/Autumn-27/artex/seoos/internal/config"
	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/store"
	"github.com/Autumn-27/artex/seoos/internal/worker"
)

const usage = "Usage: seoos [serve|api|worker|migrate|health|worker-health|provision-org NAME OWNER_EMAIL|integrations migrate-legacy [--dry-run|--show-duplicates|--apply-approved]]"

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
	case "serve", "api", "worker", "migrate", "health", "worker-health":
		if len(os.Args) > 2 {
			return errors.New(usage)
		}
	case "integrations":
		if len(os.Args) < 3 || os.Args[2] != "migrate-legacy" || len(os.Args) > 4 ||
			len(os.Args) == 4 && os.Args[3] != "--dry-run" && os.Args[3] != "--show-duplicates" && os.Args[3] != "--apply-approved" {
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
	if command == "worker-health" {
		return workerHealth(workerHeartbeatPath(), time.Now())
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
	if command == "api" || command == "worker" || command == "integrations" {
		if err := repo.CheckMigrations(startup); err != nil {
			return errors.New("SEO database schema is not ready; apply and verify migrations before starting runtime services")
		}
	} else {
		if err := repo.Migrate(startup); err != nil {
			return errors.New("SEO database migration failed; check the migration version, checksum and database permissions")
		}
	}
	if command == "integrations" {
		actor := domain.Principal{OrganizationID: cfg.OrganizationID, Role: domain.RoleOwner, Kind: "bootstrap"}
		var options store.LegacyIntegrationOptions
		if len(os.Args) == 4 {
			options.DryRun = os.Args[3] == "--dry-run"
			options.DuplicatesOnly = os.Args[3] == "--show-duplicates"
			if os.Args[3] == "--apply-approved" {
				if info, statErr := os.Stdin.Stat(); statErr != nil || info.Mode()&os.ModeCharDevice != 0 {
					return errors.New("pass the reviewed connection and target account pairs as JSON on stdin")
				}
				options.Approvals, err = readLegacyApprovals(os.Stdin)
				if err != nil {
					return err
				}
			}
		}
		results, err := repo.ReviewLegacyIntegrations(domain.WithPrincipal(ctx, actor), cfg.OrganizationID, options)
		if err != nil {
			var invalid *domain.ValidationError
			if errors.As(err, &invalid) {
				return invalid
			}
			return errors.New("cannot migrate the legacy search integrations; no changes were committed")
		}
		return json.NewEncoder(os.Stdout).Encode(results)
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
			path := workerHeartbeatPath()
			defer os.Remove(path)
			worker.RunWithHeartbeat(ctx, repo, researchProvider, content, execution, heartbeatWriter(path))
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

func readLegacyApprovals(source io.Reader) ([]store.LegacyIntegrationApproval, error) {
	const maxApprovalBytes = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(source, maxApprovalBytes+1))
	if err != nil || len(raw) > maxApprovalBytes {
		return nil, errors.New("cannot read the approved migration pairs within the 1 MiB limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var approvals []store.LegacyIntegrationApproval
	if err := decoder.Decode(&approvals); err != nil || len(approvals) == 0 {
		return nil, errors.New("provide a nonempty JSON array of connection_id and target_account_id pairs")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("provide exactly one JSON approval array")
	}
	return approvals, nil
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
	providerFixture, err := newProviderTestFixture()
	if err != nil {
		return err
	}
	handler, err := api.NewHandler(repo, cfg.OrganizationID, cfg.APIToken,
		api.Fixtures{Execution: executionFixture, Providers: providerFixture})
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

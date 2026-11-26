package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// This lock is distinct from the legacy application's migration lock. A
// transaction-scoped lock cannot remain held if initialization fails.
const migrationLockID int64 = 0x53454f4f530001

type migration struct {
	version  int64
	name     string
	sql      string
	checksum string
}

func migrations() ([]migration, error) {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	result := make([]migration, 0, len(names))
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		prefix, _, ok := strings.Cut(base, "_")
		version, err := strconv.ParseInt(prefix, 10, 64)
		if !ok || err != nil || version < 1 {
			return nil, fmt.Errorf("invalid migration filename %s", name)
		}
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		result = append(result, migration{version, name, string(body), hex.EncodeToString(sum[:])})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	for i, migration := range result {
		if migration.version != int64(i+1) {
			return nil, fmt.Errorf("migration sequence must be contiguous at %s", migration.name)
		}
	}
	return result, nil
}

// Migrate applies embedded, append-only migrations atomically. Concurrent service
// starts serialize on the same PostgreSQL advisory lock. Existing migration
// checksums must match, and a binary older than the database fails closed.
func (s *Store) Migrate(ctx context.Context) error {
	all, err := migrations()
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("acquire SEO migration lock: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS public.seo_schema_migrations (
		version BIGINT PRIMARY KEY CHECK (version > 0),
		checksum TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
	)`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT version, checksum
		FROM public.seo_schema_migrations ORDER BY version`)
	if err != nil {
		return err
	}
	applied := make(map[int64]string)
	for rows.Next() {
		var version int64
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			_ = rows.Close()
			return err
		}
		applied[version] = checksum
	}
	rowErr := rows.Err()
	if err := rows.Close(); err != nil {
		return err
	}
	if rowErr != nil {
		return rowErr
	}
	for version, checksum := range applied {
		if version > int64(len(all)) || version < 1 {
			return fmt.Errorf("database contains unknown SEO migration version %d", version)
		}
		if checksum != all[version-1].checksum {
			return fmt.Errorf("SEO migration %d checksum mismatch; restore the original migration file", version)
		}
	}
	for i := 1; i <= len(applied); i++ {
		if _, ok := applied[int64(i)]; !ok {
			return fmt.Errorf("database SEO migration history has a gap at version %d", i)
		}
	}
	for _, migration := range all {
		if _, exists := applied[migration.version]; exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, migration.sql); err != nil {
			return fmt.Errorf("apply SEO migration %d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO public.seo_schema_migrations
			(version, checksum) VALUES ($1, $2)`, migration.version, migration.checksum); err != nil {
			return err
		}
	}
	return tx.Commit()
}

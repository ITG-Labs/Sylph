package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DirtyMigrationError struct {
	Version uint
}

func (e *DirtyMigrationError) Error() string {
	return fmt.Sprintf("migration %d is dirty: it was interrupted or failed midway; check whether its file was applied, then run `migrate force %d` if it was or `migrate force %d` if it was not (see `migrate status`)",
		e.Version, e.Version, e.Version-1)
}

type MigrationStatus struct {
	Version uint
	Dirty   bool
}

func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	return applyMigrations(ctx, pool, migrationsFS, "migrations")
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, dir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m, closeDB, err := newMigratorFrom(pool, fsys, dir)
	if err != nil {
		return err
	}
	defer closeDB()
	finished := make(chan struct{})
	done := make(chan struct{})
	sent := false
	go func() {
		defer close(finished)
		select {
		case <-ctx.Done():
			m.GracefulStop <- true
			sent = true
		case <-done:
		}
	}()
	upErr := m.Up()
	close(done)
	<-finished
	if upErr != nil && !errors.Is(upErr, migrate.ErrNoChange) {
		var dirty migrate.ErrDirty
		if errors.As(upErr, &dirty) {
			return &DirtyMigrationError{Version: uint(dirty.Version)}
		}
		return fmt.Errorf("migrate up: %w", upErr)
	}
	if sent {
		select {
		case <-m.GracefulStop:
		default:
			return fmt.Errorf("migrate up: stopped before the last migration: %w", ctx.Err())
		}
	}
	return nil
}

func MigrationState(pool *pgxpool.Pool) (MigrationStatus, error) {
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		return MigrationStatus{}, err
	}
	defer closeDB()
	v, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return MigrationStatus{}, fmt.Errorf("migration version: %w", err)
	}
	return MigrationStatus{Version: v, Dirty: dirty}, nil
}

func ForceMigrationVersion(pool *pgxpool.Pool, version int) error {
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		return err
	}
	defer closeDB()
	if version == 0 {
		version = -1
	}
	if err := m.Force(version); err != nil {
		return fmt.Errorf("migrate force: %w", err)
	}
	return nil
}

func newMigrator(pool *pgxpool.Pool) (*migrate.Migrate, func(), error) {
	return newMigratorFrom(pool, migrationsFS, "migrations")
}

func newMigratorFrom(pool *pgxpool.Pool, fsys fs.FS, dir string) (*migrate.Migrate, func(), error) {
	src, err := iofs.New(fsys, dir)
	if err != nil {
		return nil, nil, fmt.Errorf("load migrations: %w", err)
	}
	connString := pool.Config().ConnString()
	db, err := sql.Open("pgx/v5", connString)
	if err != nil {
		return nil, nil, fmt.Errorf("open sql for migrations: %w", err)
	}
	var schemaName string
	if err := db.QueryRow("SHOW search_path").Scan(&schemaName); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("resolve schema: %w", err)
	}
	schemaName = strings.TrimSpace(schemaName)
	if schemaName != "public" && schemaName != `"$user", public` {
		if _, err := db.Exec(fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, schemaName)); err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("ensure schema: %w", err)
		}
	} else {
		schemaName = "public"
	}
	driver, err := postgres.WithInstance(db, &postgres.Config{SchemaName: schemaName})
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrate driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrate init: %w", err)
	}
	return m, func() { db.Close() }, nil
}
